// xterm 真实实现适配层(唯一 import @xterm 的模块):组件经工厂注入使用,
// 单测用 vi.mock 替换本模块即可完全隔离真实终端渲染(jsdom 无渲染能力)。
// xterm/fit 均被打进独立 "xterm" chunk(vite manualChunks),本页懒加载。
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

import type {
  ConsoleSocketFactory,
  ConsoleSocketLike,
  FitLike,
  TerminalFactory,
  TerminalLike,
} from "./types";
import type { TerminalThemeColors } from "./lib";

/** 同源 WS:握手自动携带会话 Cookie(浏览器行为,无需 credentials 选项)。 */
export const defaultSocketFactory: ConsoleSocketFactory = (url: string): ConsoleSocketLike => {
  const socket = new WebSocket(url);
  // PTY 输出按二进制帧处理:ArrayBuffer 直转 Uint8Array 写终端
  socket.binaryType = "arraybuffer";
  return socket as unknown as ConsoleSocketLike;
};

export const defaultTerminalFactory: TerminalFactory = (
  theme: TerminalThemeColors,
): TerminalLike => {
  const terminal = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    // tokens.css --sa-font-mono 同源等宽栈(xterm 需要具体 font-family 字符串)
    fontFamily:
      'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
    scrollback: 5000,
    theme,
  });
  return terminal as unknown as TerminalLike;
};

export const defaultFitFactory = (): FitLike => new FitAddon() as unknown as FitLike;
