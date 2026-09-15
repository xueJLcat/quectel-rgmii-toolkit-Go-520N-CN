// console 页纯函数库(无 React/xterm 依赖,jsdom 可全量单测):
// - WS 地址:同源 ${ws|wss}://host/api/console/ws(Cookie 随同源握手自动携带);
// - resize 控制帧:文本帧 JSON {"type":"resize",cols,rows},cols/rows 1–1000 clamp
//   (契约见 go-build native_console.go applyConsoleResize:越界帧被服务端忽略);
// - 连接状态机:idle/connecting/connected/disconnected(所有迟到帧按 socket 身份守卫,
//   状态迁移集中在这里,组件只做事件转发);
// - 帧解码:二进制帧(ArrayBuffer/TypedArray)= PTY 输出,文本帧兜底按字符串写入;
// - 终端配色:亮暗两套,读 tokens.css CSS 变量,jsdom/变量缺失时回退令牌默认值。

export const CONSOLE_WS_PATH = "/api/console/ws";

/** WebSocket readyState 常量(避免组件里出现魔法数)。 */
export const WS_CONNECTING = 0;
export const WS_OPEN = 1;
export const WS_CLOSED = 3;

export function consoleWsUrl(protocol: string, host: string): string {
  const scheme = protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${host}${CONSOLE_WS_PATH}`;
}

export const CONSOLE_DIM_MIN = 1;
export const CONSOLE_DIM_MAX = 1000;

/** 尺寸 clamp:非法值回退下限,小数向下取整,范围 1–1000(对齐服务端校验)。 */
export function clampConsoleDim(value: number): number {
  if (!Number.isFinite(value)) return CONSOLE_DIM_MIN;
  return Math.min(CONSOLE_DIM_MAX, Math.max(CONSOLE_DIM_MIN, Math.floor(value)));
}

/** resize 控制帧序列化(文本帧承载 JSON,与二进制键入帧互不冲突)。 */
export function buildResizeFrame(cols: number, rows: number): string {
  return JSON.stringify({
    type: "resize",
    cols: clampConsoleDim(cols),
    rows: clampConsoleDim(rows),
  });
}

/** 容器尺寸变化 → resize 帧的防抖间隔(对齐旧版 console_page.go 200ms)。 */
export const RESIZE_DEBOUNCE_MS = 200;

// ---------------------------------------------------------------------------
// 连接状态机
// ---------------------------------------------------------------------------

export type ConsoleConnectionState = "idle" | "connecting" | "connected" | "disconnected";

export type ConsoleConnectionEvent = "connect" | "open" | "close";

/**
 * 状态迁移(纯函数):connect 幂等(连接中/已连接不重复发起);open → connected;
 * close → disconnected(idle 保持,从未连接过不算"断开")。
 * error 不设独立事件:WS 语义上 error 后必随 close,状态由 close 统一收尾。
 */
export function reduceConsoleConnection(
  state: ConsoleConnectionState,
  event: ConsoleConnectionEvent,
): ConsoleConnectionState {
  switch (event) {
    case "connect":
      return state === "connecting" || state === "connected" ? state : "connecting";
    case "open":
      return "connected";
    case "close":
      return state === "idle" ? "idle" : "disconnected";
  }
}

// ---------------------------------------------------------------------------
// 帧解码
// ---------------------------------------------------------------------------

/** 服务端帧 → xterm 可写数据:二进制帧 = PTY 输出;字符串帧兜底;其余忽略。 */
export function decodeConsoleFrame(data: unknown): string | Uint8Array | null {
  if (typeof data === "string") return data;
  if (data instanceof ArrayBuffer) return new Uint8Array(data);
  if (ArrayBuffer.isView(data)) {
    return new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
  }
  return null;
}

// ---------------------------------------------------------------------------
// 终端配色(亮暗两套,读 CSS 变量)
// ---------------------------------------------------------------------------

export type ConsoleThemeMode = "light" | "dark";

export interface CssVarBag {
  bg: string;
  surface: string;
  text: string;
  textSoft: string;
  accent: string;
}

export interface TerminalThemeColors {
  background: string;
  foreground: string;
  cursor: string;
  selectionBackground: string;
}

/**
 * CSS 变量缺失(jsdom/首帧)时的回退值 = tokens.css 两套主题的令牌原值;
 * selection 需要半透明,令牌无 alpha 变体,按品牌色/暗色 accent 固定透明度。
 */
const THEME_FALLBACK: Record<ConsoleThemeMode, TerminalThemeColors> = {
  light: {
    background: "#ffffff",
    foreground: "#111827",
    cursor: "#1f6feb",
    selectionBackground: "rgba(31, 111, 235, 0.22)",
  },
  dark: {
    background: "#0d1320",
    foreground: "#d8dee9",
    cursor: "#7db1ff",
    selectionBackground: "rgba(125, 177, 255, 0.3)",
  },
};

/** 由 CSS 变量组装 xterm 主题:暗色用 --sa-bg 深蓝黑,亮色用 --sa-surface 白底。 */
export function terminalThemeFor(mode: ConsoleThemeMode, vars: CssVarBag): TerminalThemeColors {
  const fallback = THEME_FALLBACK[mode];
  const background =
    mode === "dark" ? vars.bg || fallback.background : vars.surface || fallback.background;
  return {
    background,
    foreground: (mode === "dark" ? vars.textSoft : vars.text) || fallback.foreground,
    cursor: vars.accent || fallback.cursor,
    selectionBackground: fallback.selectionBackground,
  };
}
