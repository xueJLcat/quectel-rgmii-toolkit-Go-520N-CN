import { Gateway, POST_LOGIN_HASH_KEY } from "./gateway";
import type { WebSocketLike } from "./gateway";

class MockWebSocket implements WebSocketLike {
  static instances: MockWebSocket[] = [];

  url: string;
  readyState = 0;
  sent: string[] = [];
  onopen: ((event?: unknown) => void) | null = null;
  onclose: ((event?: unknown) => void) | null = null;
  onerror: ((event?: unknown) => void) | null = null;
  onmessage: ((event: { data?: unknown }) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(data: string): void {
    if (this.readyState !== 1) throw new Error("socket not open");
    this.sent.push(data);
  }

  close(): void {
    this.readyState = 3;
    this.onclose?.();
  }

  open(): void {
    this.readyState = 1;
    this.onopen?.();
  }

  drop(): void {
    this.readyState = 3;
    this.onclose?.();
  }

  fail(): void {
    this.readyState = 3;
    this.onerror?.();
    this.onclose?.();
  }

  receive(message: unknown): void {
    this.onmessage?.({ data: JSON.stringify(message) });
  }

  receiveRaw(data: string): void {
    this.onmessage?.({ data });
  }

  lastFrame(): Record<string, unknown> {
    return JSON.parse(this.sent[this.sent.length - 1] ?? "{}") as Record<string, unknown>;
  }
}

interface TestContext {
  gw: Gateway;
  sockets: MockWebSocket[];
  redirector: ReturnType<typeof vi.fn>;
  fetchMock: ReturnType<typeof vi.fn>;
}

const created: Gateway[] = [];

function createGateway(options?: { requestTimeoutMs?: number }): TestContext {
  MockWebSocket.instances = [];
  const redirector = vi.fn();
  const fetchMock = vi.fn().mockResolvedValue({ status: 200 });
  const gw = new Gateway({
    webSocketFactory: (url) => new MockWebSocket(url),
    redirector,
    fetchImpl: fetchMock as unknown as typeof fetch,
    ...options,
  });
  created.push(gw);
  return { gw, sockets: MockWebSocket.instances, redirector, fetchMock };
}

async function connectGateway(ctx: TestContext): Promise<MockWebSocket> {
  const promise = ctx.gw.connect();
  const socket = ctx.sockets[ctx.sockets.length - 1];
  socket.open();
  await promise;
  return socket;
}

beforeEach(() => {
  sessionStorage.clear();
  location.hash = "";
});

afterEach(() => {
  vi.useRealTimers();
  while (created.length > 0) {
    const gw = created.pop();
    gw?.stopKeepalive();
    gw?.close();
  }
});

describe("gateway connect/state", () => {
  it("connect 建立 WS 到 /api/ws 并更新状态", async () => {
    const ctx = createGateway();
    const states: string[] = [];
    ctx.gw.onStateChange((state) => states.push(state));
    expect(ctx.gw.isConnected()).toBe(false);
    expect(ctx.gw.getState()).toBe("disconnected");

    const promise = ctx.gw.connect();
    expect(ctx.sockets).toHaveLength(1);
    expect(ctx.sockets[0].url).toBe(`${location.origin.replace(/^http/, "ws")}/api/ws`);
    ctx.sockets[0].open();
    await promise;

    expect(ctx.gw.isConnected()).toBe(true);
    expect(states).toEqual(["connecting", "connected"]);
  });

  it("未连接时 request 直接 reject(不排队)", async () => {
    const ctx = createGateway();
    await expect(ctx.gw.request("GET", "/api/get_uptime")).rejects.toThrow("gateway not connected");
  });

  it("close() 主动断开后不再重连", async () => {
    vi.useFakeTimers();
    const ctx = createGateway();
    await connectGateway(ctx);
    ctx.gw.close();
    expect(ctx.gw.isConnected()).toBe(false);
    expect(ctx.gw.getState()).toBe("disconnected");
    vi.advanceTimersByTime(60_000);
    expect(ctx.sockets).toHaveLength(1);
    await expect(ctx.gw.request("GET", "/api/get_uptime")).rejects.toThrow("gateway not connected");
  });
});

describe("gateway request/response", () => {
  it("请求帧含 id/method/path/headers/body,响应按 id 匹配且支持乱序", async () => {
    const ctx = createGateway();
    const socket = await connectGateway(ctx);

    const p1 = ctx.gw.request("GET", "/api/get_uptime");
    const p2 = ctx.gw.request("POST", "/api/dashboard_data", "action=get");
    const f1 = JSON.parse(socket.sent[0]) as Record<string, unknown>;
    const f2 = JSON.parse(socket.sent[1]) as Record<string, unknown>;
    expect(f1).toMatchObject({ method: "GET", path: "/api/get_uptime", headers: {}, body: "" });
    expect(f2).toMatchObject({ method: "POST", path: "/api/dashboard_data", body: "action=get" });
    expect(f1.id).not.toBe(f2.id);

    // 乱序:先回 id2 再回 id1
    socket.receive({
      id: f2.id,
      status: 200,
      headers: { "Content-Type": ["application/json"] },
      body: '{"pending":true}',
    });
    socket.receive({ id: f1.id, status: 200, body: "up 1 day, 0 hour, 2 min" });

    await expect(p2).resolves.toEqual({
      status: 200,
      headers: { "Content-Type": "application/json" },
      body: '{"pending":true}',
    });
    await expect(p1).resolves.toEqual({
      status: 200,
      headers: {},
      body: "up 1 day, 0 hour, 2 min",
    });
  });

  it("POST form body 原样入帧,无 Content-Type 时自动补默认值", async () => {
    const ctx = createGateway();
    const socket = await connectGateway(ctx);

    const promise = ctx.gw.request("POST", "/api/set_ttl", "ttlvalue=64");
    expect(socket.lastFrame()).toMatchObject({
      method: "POST",
      path: "/api/set_ttl",
      body: "ttlvalue=64",
      headers: { "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8" },
    });

    const frame = socket.lastFrame();
    socket.receive({ id: frame.id, status: 200, body: '{"ok":true}' });
    await expect(promise).resolves.toMatchObject({ status: 200, body: '{"ok":true}' });
  });

  it("error 帧(无 status)reject 对应请求", async () => {
    const ctx = createGateway();
    const socket = await connectGateway(ctx);
    const promise = ctx.gw.request("GET", "/api/bad_path");
    socket.receive({ id: socket.lastFrame().id, error: "unsupported websocket api endpoint" });
    await expect(promise).rejects.toThrow("unsupported websocket api endpoint");
  });

  it("非法 JSON 帧被忽略且不中断连接", async () => {
    const ctx = createGateway();
    const socket = await connectGateway(ctx);
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    socket.receiveRaw("not-json{{");
    expect(consoleError).toHaveBeenCalled();
    expect(ctx.gw.isConnected()).toBe(true);
    consoleError.mockRestore();
  });

  it("超时 reject:默认 240s,可 per-request 覆盖", async () => {
    vi.useFakeTimers();
    const ctx = createGateway();
    const socket = await connectGateway(ctx);

    const fast = ctx.gw.request("GET", "/api/a", undefined, { timeoutMs: 1000 });
    const slow = ctx.gw.request("GET", "/api/b");
    const fastResult = fast.then(
      () => "resolved",
      (error: Error) => error.message,
    );
    const slowResult = slow.then(
      () => "resolved",
      (error: Error) => error.message,
    );

    vi.advanceTimersByTime(999);
    vi.advanceTimersByTime(1);
    expect(await fastResult).toBe("request timeout");

    // 默认超时 240_000ms:再推进到累计 240s 才超时
    vi.advanceTimersByTime(238_000);
    vi.advanceTimersByTime(1000);
    expect(await slowResult).toBe("request timeout");

    // 超时后连接仍可用(pending 已清理,不影响后续请求)
    const next = ctx.gw.request("GET", "/api/c");
    socket.receive({ id: socket.lastFrame().id, status: 200, body: "ok" });
    await expect(next).resolves.toMatchObject({ body: "ok" });
  });

  it("401 响应写入 postLoginHash 并调用注入的 redirector(仅一次)", async () => {
    const ctx = createGateway();
    const socket = await connectGateway(ctx);
    location.hash = "#/settings";

    const p1 = ctx.gw.request("GET", "/api/dashboard_data");
    socket.receive({
      id: socket.lastFrame().id,
      status: 401,
      error: "login required",
      body: '{"ok":false,"error":"login required"}',
    });
    await expect(p1).resolves.toMatchObject({ status: 401 });

    const p2 = ctx.gw.request("GET", "/api/history_data");
    socket.receive({ id: socket.lastFrame().id, status: 401, body: "" });
    await expect(p2).resolves.toMatchObject({ status: 401 });

    expect(ctx.redirector).toHaveBeenCalledTimes(1);
    expect(sessionStorage.getItem(POST_LOGIN_HASH_KEY)).toBe("#/settings");
  });
});

describe("gateway server events", () => {
  it("事件帧派发给具名订阅与通配 *,取消订阅后不再收到", async () => {
    const ctx = createGateway();
    const socket = await connectGateway(ctx);
    const named = vi.fn();
    const wildcard = vi.fn();
    const off = ctx.gw.onServerEvent("at_cache_updated", named);
    ctx.gw.onServerEvent("*", wildcard);

    socket.receive({ type: "event", event: "at_cache_updated", data: { command: "ATI" } });
    expect(named).toHaveBeenCalledWith({
      type: "event",
      event: "at_cache_updated",
      data: { command: "ATI" },
    });
    expect(wildcard).toHaveBeenCalledTimes(1);

    off();
    socket.receive({ type: "event", event: "at_cache_updated", data: { command: "AT+CSQ" } });
    expect(named).toHaveBeenCalledTimes(1);
    expect(wildcard).toHaveBeenCalledTimes(2);

    socket.receive({ type: "event", event: "ip_passthrough_result", data: { ok: "true" } });
    expect(named).toHaveBeenCalledTimes(1);
    expect(wildcard).toHaveBeenCalledTimes(3);
    expect(wildcard).toHaveBeenLastCalledWith({
      type: "event",
      event: "ip_passthrough_result",
      data: { ok: "true" },
    });
  });
});

describe("gateway reconnect", () => {
  it("断线 reject 挂起请求,按退避自动重连,恢复后可再次请求", async () => {
    vi.useFakeTimers();
    const ctx = createGateway();
    const socket = await connectGateway(ctx);

    const inflight = ctx.gw.request("GET", "/api/get_uptime");
    const inflightError = inflight.then(
      () => null,
      (error: Error) => error.message,
    );
    socket.drop();
    expect(await inflightError).toBe("WebSocket connection closed");
    expect(ctx.gw.isConnected()).toBe(false);
    await expect(ctx.gw.request("GET", "/api/x")).rejects.toThrow("gateway not connected");

    // 第一次退避 500ms
    vi.advanceTimersByTime(499);
    expect(ctx.sockets).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(ctx.sockets).toHaveLength(2);

    // 再次握手失败 → fetch /api/get_uptime 探测会话(非 401 继续重连)
    ctx.sockets[1].fail();
    expect(ctx.fetchMock).toHaveBeenCalledWith(
      "/api/get_uptime",
      expect.objectContaining({ credentials: "same-origin" }),
    );

    // 第二次退避 1000ms,连接恢复后可再次请求
    vi.advanceTimersByTime(1000);
    expect(ctx.sockets).toHaveLength(3);
    ctx.sockets[2].open();
    expect(ctx.gw.isConnected()).toBe(true);

    const next = ctx.gw.request("GET", "/api/get_uptime");
    ctx.sockets[2].receive({ id: ctx.sockets[2].lastFrame().id, status: 200, body: "up 2 day" });
    await expect(next).resolves.toMatchObject({ status: 200, body: "up 2 day" });
  });

  it("退避从 500ms 起指数增长,上限 15s", async () => {
    vi.useFakeTimers();
    const ctx = createGateway();
    await connectGateway(ctx);
    ctx.sockets[0].drop();

    for (const delay of [500, 1000, 2000, 4000, 8000, 15000, 15000]) {
      const before = ctx.sockets.length;
      vi.advanceTimersByTime(delay - 1);
      expect(ctx.sockets).toHaveLength(before);
      vi.advanceTimersByTime(1);
      expect(ctx.sockets).toHaveLength(before + 1);
      ctx.sockets[ctx.sockets.length - 1].fail();
    }
  });

  it("握手失败探测到 401 → 跳登录并停止重连", async () => {
    vi.useFakeTimers();
    const ctx = createGateway();
    ctx.fetchMock.mockResolvedValue({ status: 401 });

    const connectError = ctx.gw.connect().then(
      () => null,
      (error: Error) => error.message,
    );
    ctx.sockets[0].fail();
    expect(await connectError).toBe("WebSocket connection failed");
    await Promise.resolve();
    await Promise.resolve();

    expect(ctx.redirector).toHaveBeenCalledTimes(1);
    expect(sessionStorage.getItem(POST_LOGIN_HASH_KEY)).toBe("");
    vi.advanceTimersByTime(60_000);
    expect(ctx.sockets).toHaveLength(1);
  });
});

describe("gateway keepalive", () => {
  it("startKeepalive 每 15min fetch get_uptime,幂等启动,stopKeepalive 停止", async () => {
    vi.useFakeTimers();
    const ctx = createGateway();
    ctx.gw.startKeepalive();
    ctx.gw.startKeepalive();

    vi.advanceTimersByTime(14 * 60 * 1000);
    expect(ctx.fetchMock).not.toHaveBeenCalled();
    vi.advanceTimersByTime(60 * 1000);
    expect(ctx.fetchMock).toHaveBeenCalledTimes(1);
    expect(ctx.fetchMock).toHaveBeenCalledWith(
      "/api/get_uptime",
      expect.objectContaining({ credentials: "same-origin" }),
    );

    ctx.gw.stopKeepalive();
    vi.advanceTimersByTime(60 * 60 * 1000);
    expect(ctx.fetchMock).toHaveBeenCalledTimes(1);
  });
});
