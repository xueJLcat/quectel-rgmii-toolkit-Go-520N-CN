// xterm 真实实现适配层(唯一 import @xterm 的模块):组件经工厂注入使用,
// 单测用 vi.mock 替换本模块即可完全隔离真实终端渲染(jsdom 无渲染能力)。
// xterm/fit/webgl/search 均被打进独立 "xterm" chunk(vite manualChunks),本页懒加载。
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebglAddon } from "@xterm/addon-webgl";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

import type {
  ConsoleSocketFactory,
  ConsoleSocketLike,
  FitLike,
  SearchFactory,
  SearchLike,
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
  // WebGL 渲染器须在 open() 之后激活(需要 canvas 挂载):包装 open,激活失败
  // (无 WebGL 上下文的环境)或运行中上下文丢失时 dispose,xterm 自动回落
  // DOM 渲染器,终端功能不受影响——官方 README 的降级口径。
  const openRenderer = terminal.open.bind(terminal);
  const terminalLike = terminal as unknown as TerminalLike;
  terminalLike.open = (element: HTMLElement) => {
    openRenderer(element);
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => webgl.dispose());
      terminal.loadAddon(webgl);
    } catch {
      // 无 WebGL:保持缺省 DOM 渲染器
    }
  };
  return terminalLike;
};

export const defaultFitFactory = (): FitLike => new FitAddon() as unknown as FitLike;

export const defaultSearchFactory: SearchFactory = (term: TerminalLike): SearchLike | null => {
  try {
    const addon = new SearchAddon();
    term.loadAddon(addon);
    return {
      findNext: (query) => addon.findNext(query),
      findPrevious: (query) => addon.findPrevious(query),
      clearDecorations: () => addon.clearDecorations(),
      dispose: () => addon.dispose(),
    };
  } catch {
    // 终端尚未 open 等异常场景:搜索功能缺席,终端本体不受影响
    return null;
  }
};
