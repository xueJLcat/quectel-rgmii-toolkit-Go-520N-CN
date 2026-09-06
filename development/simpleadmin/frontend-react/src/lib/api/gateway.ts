export type GatewayMethod = "GET" | "POST";

export interface GatewayResponse {
  status: number;
  headers: Record<string, string>;
  body: string;
}

export interface ServerEventMessage {
  type: "event";
  event: string;
  data: Record<string, string>;
}

export type ServerEventHandler = (message: ServerEventMessage) => void;

export type GatewayState = "disconnected" | "connecting" | "connected";

export type GatewayStateHandler = (state: GatewayState) => void;

export interface WebSocketLike {
  readonly readyState: number;
  send(data: string): void;
  close(code?: number, reason?: string): void;
  onopen: ((event?: unknown) => void) | null;
  onclose: ((event?: unknown) => void) | null;
  onerror: ((event?: unknown) => void) | null;
  onmessage: ((event: { data?: unknown }) => void) | null;
}

export type WebSocketFactory = (url: string) => WebSocketLike;

export interface GatewayRequestOptions {
  headers?: Record<string, string>;
  timeoutMs?: number;
}

export interface GatewayOptions {
  webSocketFactory?: WebSocketFactory;
  fetchImpl?: typeof fetch;
  redirector?: () => void;
  requestTimeoutMs?: number;
}

interface PendingRequest {
  resolve: (response: GatewayResponse) => void;
  reject: (error: Error) => void;
}

interface ResponseFrame {
  id?: unknown;
  status?: unknown;
  headers?: unknown;
  body?: unknown;
  error?: unknown;
  type?: unknown;
  event?: unknown;
  data?: unknown;
}

// 小区扫描后端最长约 182 秒(命令超时 180s+等待余量),前端请求超时必须大于它,
// 否则扫描临近完成时前端会先超时(口径与 www/js/simpleadmin-api.js 一致)。
export const DEFAULT_REQUEST_TIMEOUT_MS = 240_000;
export const POST_LOGIN_HASH_KEY = "simpleadmin.postLoginHash";
export const FORM_CONTENT_TYPE = "application/x-www-form-urlencoded; charset=UTF-8";

const WS_PATH = "/api/ws";
const LOGIN_PATH = "/login.html";
// 会话探测/保活端点:需要登录态,401 即会话失效。
const SESSION_PROBE_PATH = "/api/get_uptime";
const RECONNECT_BASE_DELAY_MS = 500;
const RECONNECT_MAX_DELAY_MS = 15_000;
const KEEPALIVE_INTERVAL_MS = 15 * 60 * 1000;
const WS_OPEN = 1;

const defaultWebSocketFactory: WebSocketFactory = (url) =>
  new WebSocket(url) as unknown as WebSocketLike;

const defaultFetchImpl: typeof fetch = (input, init) => fetch(input, init);

const defaultRedirector: () => void = () => {
  try {
    location.replace(LOGIN_PATH);
  } catch {
    /* jsdom 等环境可能未实现 location.replace,静默忽略 */
  }
};

function defaultWsUrl(): string {
  return `${location.origin.replace(/^http/, "ws")}${WS_PATH}`;
}

function normalizeResponseHeaders(raw: unknown): Record<string, string> {
  const normalized: Record<string, string> = {};
  if (!raw || typeof raw !== "object") return normalized;
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    // Go 端 headers 是 map[string][]string,多值按 HTTP 惯例逗号拼接。
    if (Array.isArray(value)) {
      normalized[key] = value.join(", ");
    } else if (value !== undefined && value !== null) {
      normalized[key] = String(value);
    }
  }
  return normalized;
}

function normalizeEventData(raw: unknown): Record<string, string> {
  const data: Record<string, string> = {};
  if (!raw || typeof raw !== "object") return data;
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    if (value !== undefined && value !== null) data[key] = String(value);
  }
  return data;
}

export class Gateway {
  private readonly wsFactory: WebSocketFactory;
  private readonly fetchImpl: typeof fetch;
  private readonly redirector: () => void;
  private readonly timeoutMs: number;

  private ws: WebSocketLike | null = null;
  private connectPromise: Promise<void> | null = null;
  private connectResolve: (() => void) | null = null;
  private connectReject: ((error: Error) => void) | null = null;
  private nextId = 1;
  private readonly pending = new Map<string, PendingRequest>();
  private readonly eventHandlers = new Map<string, Set<ServerEventHandler>>();
  private readonly stateHandlers = new Set<GatewayStateHandler>();
  private state: GatewayState = "disconnected";
  private sessionRedirecting = false;
  private closedByUser = false;
  private reconnectAttempts = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private keepaliveTimer: ReturnType<typeof setInterval> | null = null;

  constructor(options: GatewayOptions = {}) {
    this.wsFactory = options.webSocketFactory ?? defaultWebSocketFactory;
    this.fetchImpl = options.fetchImpl ?? defaultFetchImpl;
    this.redirector = options.redirector ?? defaultRedirector;
    this.timeoutMs = options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
  }

  getState(): GatewayState {
    return this.state;
  }

  isConnected(): boolean {
    return this.ws !== null && this.ws.readyState === WS_OPEN;
  }

  onStateChange(handler: GatewayStateHandler): () => void {
    this.stateHandlers.add(handler);
    return () => {
      this.stateHandlers.delete(handler);
    };
  }

  onServerEvent(name: string, handler: ServerEventHandler): () => void {
    let handlers = this.eventHandlers.get(name);
    if (!handlers) {
      handlers = new Set();
      this.eventHandlers.set(name, handlers);
    }
    handlers.add(handler);
    return () => {
      const set = this.eventHandlers.get(name);
      if (!set) return;
      set.delete(handler);
      if (set.size === 0) this.eventHandlers.delete(name);
    };
  }

  connect(): Promise<void> {
    if (this.isConnected()) return Promise.resolve();
    if (this.connectPromise) return this.connectPromise;
    this.closedByUser = false;
    return this.attemptConnect();
  }

  close(): void {
    this.closedByUser = true;
    this.clearReconnectTimer();
    const socket = this.ws;
    this.ws = null;
    this.settleConnect(new Error("gateway closed"));
    this.rejectAll(new Error("gateway closed"));
    if (socket) {
      try {
        socket.close();
      } catch {
        /* 关闭失败不影响本地状态清理 */
      }
    }
    this.setState("disconnected");
  }

  request(
    method: GatewayMethod,
    path: string,
    body?: string,
    options?: GatewayRequestOptions,
  ): Promise<GatewayResponse> {
    const socket = this.ws;
    if (!socket || socket.readyState !== WS_OPEN) {
      // 重连期间不排队:调用方按失败处理(与断线语义一致)。
      return Promise.reject(new Error("gateway not connected"));
    }
    const headers: Record<string, string> = { ...options?.headers };
    const payload = body ?? "";
    if (payload && !Object.keys(headers).some((key) => key.toLowerCase() === "content-type")) {
      headers["Content-Type"] = FORM_CONTENT_TYPE;
    }
    const id = String(this.nextId++);
    const timeoutMs = options?.timeoutMs ?? this.timeoutMs;
    return new Promise<GatewayResponse>((resolve, reject) => {
      const timer = setTimeout(() => {
        if (!this.pending.has(id)) return;
        this.pending.delete(id);
        reject(new Error("request timeout"));
      }, timeoutMs);
      this.pending.set(id, {
        resolve: (response) => {
          clearTimeout(timer);
          if (response.status === 401) this.redirectToLogin();
          resolve(response);
        },
        reject: (error) => {
          clearTimeout(timer);
          reject(error);
        },
      });
      try {
        socket.send(JSON.stringify({ id, method, path, headers, body: payload }));
      } catch (error) {
        this.pending.delete(id);
        clearTimeout(timer);
        reject(error instanceof Error ? error : new Error(String(error)));
      }
    });
  }

  startKeepalive(): void {
    if (this.keepaliveTimer !== null) return;
    // 业务请求全部经 WS 网关,Cookie 不会随 WS 帧续期;定期发一个普通 HTTP
    // 请求穿过服务端会话中间件,触发滑动续期,避免活跃页面被整点登出。
    this.keepaliveTimer = setInterval(() => {
      if (typeof document !== "undefined" && document.hidden) return;
      try {
        this.fetchImpl(SESSION_PROBE_PATH, {
          cache: "no-store",
          credentials: "same-origin",
        }).catch(() => {
          /* keepalive best-effort */
        });
      } catch {
        /* fetch 不可用时静默跳过 */
      }
    }, KEEPALIVE_INTERVAL_MS);
  }

  stopKeepalive(): void {
    if (this.keepaliveTimer === null) return;
    clearInterval(this.keepaliveTimer);
    this.keepaliveTimer = null;
  }

  private attemptConnect(): Promise<void> {
    this.clearReconnectTimer();
    const socket = this.wsFactory(defaultWsUrl());
    this.ws = socket;
    this.setState("connecting");
    let opened = false;
    const promise = new Promise<void>((resolve, reject) => {
      this.connectResolve = resolve;
      this.connectReject = reject;
    });
    this.connectPromise = promise;

    socket.onopen = () => {
      if (this.ws !== socket) return;
      opened = true;
      this.reconnectAttempts = 0;
      this.setState("connected");
      this.settleConnect();
    };
    socket.onerror = () => {
      if (this.ws !== socket || opened) return;
      this.settleConnect(new Error("WebSocket connection failed"));
    };
    socket.onclose = () => {
      if (this.ws !== socket) return;
      this.ws = null;
      this.setState("disconnected");
      this.rejectAll(new Error("WebSocket connection closed"));
      this.settleConnect(new Error("WebSocket connection closed"));
      // 握手从未成功:可能是会话失效被 302 拦截,fetch 探测区分网络故障与登出。
      if (!opened && !this.closedByUser) this.probeSession();
      if (!this.closedByUser && !this.sessionRedirecting) this.scheduleReconnect();
    };
    socket.onmessage = (event) => {
      if (this.ws !== socket) return;
      this.handleMessage(event);
    };
    return promise;
  }

  private settleConnect(error?: Error): void {
    const resolve = this.connectResolve;
    const reject = this.connectReject;
    this.connectPromise = null;
    this.connectResolve = null;
    this.connectReject = null;
    if (error) {
      reject?.(error);
    } else {
      resolve?.();
    }
  }

  private handleMessage(event: { data?: unknown }): void {
    let message: ResponseFrame;
    try {
      message = JSON.parse(String(event.data ?? "{}")) as ResponseFrame;
    } catch (error) {
      console.error("Invalid WebSocket API response:", error);
      return;
    }
    if (message.type === "event" && typeof message.event === "string" && message.event !== "") {
      this.dispatchServerEvent({
        type: "event",
        event: message.event,
        data: normalizeEventData(message.data),
      });
      return;
    }
    const id = message.id === undefined || message.id === null ? "" : String(message.id);
    const item = this.pending.get(id);
    if (!item) return;
    this.pending.delete(id);
    if (message.error && !message.status) {
      item.reject(new Error(String(message.error)));
      return;
    }
    item.resolve({
      status: Number(message.status) || 0,
      headers: normalizeResponseHeaders(message.headers),
      body: typeof message.body === "string" ? message.body : "",
    });
  }

  private dispatchServerEvent(message: ServerEventMessage): void {
    for (const name of [message.event, "*"]) {
      const handlers = this.eventHandlers.get(name);
      if (!handlers) continue;
      for (const handler of [...handlers]) {
        try {
          handler(message);
        } catch (error) {
          console.error(`server event handler failed (${name}):`, error);
        }
      }
    }
  }

  private rejectAll(error: Error): void {
    this.pending.forEach((item) => item.reject(error));
    this.pending.clear();
  }

  private setState(state: GatewayState): void {
    if (this.state === state) return;
    this.state = state;
    for (const handler of [...this.stateHandlers]) {
      try {
        handler(state);
      } catch (error) {
        console.error("gateway state handler failed:", error);
      }
    }
  }

  private scheduleReconnect(): void {
    if (this.closedByUser || this.sessionRedirecting || this.reconnectTimer !== null) return;
    const delay = Math.min(
      RECONNECT_MAX_DELAY_MS,
      RECONNECT_BASE_DELAY_MS * 2 ** this.reconnectAttempts,
    );
    this.reconnectAttempts += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (this.closedByUser || this.sessionRedirecting) return;
      this.attemptConnect().catch(() => {
        /* 失败后的下一次退避由 onclose 驱动 */
      });
    }, delay);
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer === null) return;
    clearTimeout(this.reconnectTimer);
    this.reconnectTimer = null;
  }

  private probeSession(): void {
    if (this.sessionRedirecting) return;
    try {
      this.fetchImpl(SESSION_PROBE_PATH, {
        cache: "no-store",
        credentials: "same-origin",
      })
        .then((response) => {
          if (response && response.status === 401) this.redirectToLogin();
        })
        .catch(() => {
          /* 探测失败按网络故障处理,维持原有退避重连 */
        });
    } catch {
      /* fetch 不可用按网络故障处理 */
    }
  }

  private redirectToLogin(): void {
    if (this.sessionRedirecting) return;
    this.sessionRedirecting = true;
    try {
      sessionStorage.setItem(POST_LOGIN_HASH_KEY, location.hash || "");
    } catch {
      /* 存储不可用时仍继续跳转 */
    }
    try {
      this.redirector();
    } catch {
      /* 跳转失败不阻塞后续逻辑 */
    }
  }
}

export const gateway = new Gateway();
