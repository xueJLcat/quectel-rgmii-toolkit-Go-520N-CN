// console 终端的可注入结构类型(组件只依赖这些最小接口,真实 xterm/WebSocket
// 在 xterm-adapter.ts 适配,单测注入 fake 即可覆盖连接状态机/resize/重连逻辑)。
import type { TerminalThemeColors } from "./lib";

/** 终端 socket 最小接口(与原生 WebSocket 二进制帧语义一致)。 */
export interface ConsoleSocketLike {
  readonly readyState: number;
  binaryType: string;
  send(data: string | Uint8Array): void;
  close(code?: number, reason?: string): void;
  onopen: ((event?: unknown) => void) | null;
  onclose: ((event?: unknown) => void) | null;
  onerror: ((event?: unknown) => void) | null;
  onmessage: ((event: { data?: unknown }) => void) | null;
}

export type ConsoleSocketFactory = (url: string) => ConsoleSocketLike;

/** xterm Terminal 最小接口(v5:options.theme 赋值即生效,无 v4 setOption)。 */
export interface TerminalLike {
  readonly cols: number;
  readonly rows: number;
  options: { theme?: unknown };
  open(element: HTMLElement): void;
  loadAddon(addon: unknown): void;
  write(data: string | Uint8Array): void;
  onData(handler: (data: string) => void): { dispose(): void };
  resize(cols: number, rows: number): void;
  reset(): void;
  focus(): void;
  dispose(): void;
}

/** FitAddon 最小接口。 */
export interface FitLike {
  fit(): void;
  proposeDimensions(): { cols?: number; rows?: number } | undefined;
}

export type TerminalFactory = (theme: TerminalThemeColors) => TerminalLike;
export type FitFactory = () => FitLike;
