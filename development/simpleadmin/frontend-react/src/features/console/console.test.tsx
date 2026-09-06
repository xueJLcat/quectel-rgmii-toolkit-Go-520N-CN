// console 页测试:纯函数(WS 地址/resize 帧 clamp 序列化/连接状态机/帧解码/亮暗配色)+
// 组件测试(注入 fake WS 工厂与 fake xterm:懒连接、二进制帧读写、resize 防抖帧、
// 断开重连、重载清屏、卸载 dispose、主题联动)。jsdom 无真 WS/xterm 渲染能力,
// 真机验证留给集成阶段(安全红线:本机验证只连 127.0.0.1 mock)。
import { act, fireEvent, render, screen } from "@testing-library/react";

import "@/lib/i18n";
import { useUiStore } from "@/stores/ui";

import { ConsoleTerminal } from "./components/ConsoleTerminal";
import {
  buildResizeFrame,
  clampConsoleDim,
  consoleWsUrl,
  decodeConsoleFrame,
  reduceConsoleConnection,
  RESIZE_DEBOUNCE_MS,
  terminalThemeFor,
  WS_OPEN,
} from "./lib";
import type { ConsoleConnectionState } from "./lib";
import type {
  ConsoleSocketFactory,
  ConsoleSocketLike,
  FitFactory,
  FitLike,
  TerminalFactory,
  TerminalLike,
} from "./types";

// ---------------------------------------------------------------------------
// 纯函数
// ---------------------------------------------------------------------------

describe("consoleWsUrl", () => {
  test.each([
    ["http:", "192.168.1.1:8080", "ws://192.168.1.1:8080/api/console/ws"],
    ["https:", "example.com", "wss://example.com/api/console/ws"],
  ])("%s → %s", (protocol, host, expected) => {
    expect(consoleWsUrl(protocol, host)).toBe(expected);
  });
});

describe("clampConsoleDim / buildResizeFrame", () => {
  test.each([
    [80, 80],
    [0, 1],
    [-5, 1],
    [1500, 1000],
    [1000, 1000],
    [80.9, 80],
    [Number.NaN, 1],
    [Number.POSITIVE_INFINITY, 1],
  ])("clamp(%i) → %i(1–1000 契约)", (input, expected) => {
    expect(clampConsoleDim(input)).toBe(expected);
  });

  test("resize 帧序列化:文本帧 JSON,越界值 clamp", () => {
    expect(buildResizeFrame(120, 32)).toBe('{"type":"resize","cols":120,"rows":32}');
    expect(buildResizeFrame(0, 5000)).toBe('{"type":"resize","cols":1,"rows":1000}');
    const frame = JSON.parse(buildResizeFrame(80, 24)) as {
      type: string;
      cols: number;
      rows: number;
    };
    expect(frame).toEqual({ type: "resize", cols: 80, rows: 24 });
  });
});

describe("reduceConsoleConnection", () => {
  test.each([
    ["idle", "connect", "connecting"],
    ["connecting", "connect", "connecting"],
    ["connected", "connect", "connected"],
    ["disconnected", "connect", "connecting"],
    ["connecting", "open", "connected"],
    ["connected", "close", "disconnected"],
    ["connecting", "close", "disconnected"],
    ["idle", "close", "idle"],
    ["disconnected", "close", "disconnected"],
  ] as [ConsoleConnectionState, "connect" | "open" | "close", ConsoleConnectionState][])(
    "%s + %s → %s",
    (state, event, expected) => {
      expect(reduceConsoleConnection(state, event)).toBe(expected);
    },
  );
});

describe("decodeConsoleFrame", () => {
  test("二进制帧(ArrayBuffer/视图)→ Uint8Array;文本帧 → 字符串;其余忽略", () => {
    const buffer = Uint8Array.from([72, 73]).buffer;
    expect(decodeConsoleFrame(buffer)).toEqual(new Uint8Array([72, 73]));
    const view = new Uint8Array([1, 2, 3, 4]).subarray(1, 3);
    expect(decodeConsoleFrame(view)).toEqual(new Uint8Array([2, 3]));
    expect(decodeConsoleFrame("plain")).toBe("plain");
    expect(decodeConsoleFrame(undefined)).toBeNull();
    expect(decodeConsoleFrame(42)).toBeNull();
  });
});

describe("terminalThemeFor", () => {
  const vars = {
    bg: "#0d1320",
    surface: "#ffffff",
    text: "#111827",
    textSoft: "#d8dee9",
    accent: "#1f6feb",
  };

  test("暗色:深蓝黑底 + soft 前景;亮色:白底 + 正文前景", () => {
    expect(terminalThemeFor("dark", vars)).toMatchObject({
      background: "#0d1320",
      foreground: "#d8dee9",
      cursor: "#1f6feb",
    });
    expect(terminalThemeFor("light", vars)).toMatchObject({
      background: "#ffffff",
      foreground: "#111827",
      cursor: "#1f6feb",
    });
  });

  test("CSS 变量缺失(jsdom)回退令牌默认值", () => {
    const empty = { bg: "", surface: "", text: "", textSoft: "", accent: "" };
    expect(terminalThemeFor("dark", empty).background).toBe("#0d1320");
    expect(terminalThemeFor("light", empty).background).toBe("#ffffff");
    expect(terminalThemeFor("light", empty).foreground).toBe("#111827");
  });
});

// ---------------------------------------------------------------------------
// 组件测试(fake WS 工厂 + fake xterm 注入)
// ---------------------------------------------------------------------------

class FakeSocket implements ConsoleSocketLike {
  static instances: FakeSocket[] = [];
  readyState = 0; // CONNECTING
  binaryType = "blob";
  sent: (string | Uint8Array)[] = [];
  closeCalls = 0;
  onopen: ((event?: unknown) => void) | null = null;
  onclose: ((event?: unknown) => void) | null = null;
  onerror: ((event?: unknown) => void) | null = null;
  onmessage: ((event: { data?: unknown }) => void) | null = null;

  constructor(readonly url: string) {
    FakeSocket.instances.push(this);
  }

  send(data: string | Uint8Array): void {
    this.sent.push(data);
  }

  close(): void {
    this.closeCalls += 1;
    this.readyState = 3;
  }

  // 测试驱动
  openNow(): void {
    this.readyState = WS_OPEN;
    this.onopen?.();
  }

  closeNow(): void {
    this.readyState = 3;
    this.onclose?.();
  }

  emit(data: unknown): void {
    this.onmessage?.({ data });
  }
}

class FakeTerminal implements TerminalLike {
  cols = 120;
  rows = 32;
  options: { theme?: unknown } = {};
  written: (string | Uint8Array)[] = [];
  dataHandlers: ((data: string) => void)[] = [];
  loadAddonCalls = 0;
  opened = false;
  disposed = false;
  resetCalls = 0;
  focusCalls = 0;

  constructor(theme?: unknown) {
    this.options.theme = theme;
  }

  open(): void {
    this.opened = true;
  }

  loadAddon(): void {
    this.loadAddonCalls += 1;
  }

  write(data: string | Uint8Array): void {
    this.written.push(data);
  }

  onData(handler: (data: string) => void): { dispose(): void } {
    this.dataHandlers.push(handler);
    return { dispose: () => {} };
  }

  resize(): void {}

  reset(): void {
    this.resetCalls += 1;
  }

  focus(): void {
    this.focusCalls += 1;
  }

  dispose(): void {
    this.disposed = true;
  }
}

class FakeFit implements FitLike {
  fitCalls = 0;
  fit(): void {
    this.fitCalls += 1;
  }
  proposeDimensions(): { cols?: number; rows?: number } {
    return { cols: 120, rows: 32 };
  }
}

let lastTerminal: FakeTerminal | null = null;

// 模块级稳定工厂:避免每次 render 新身份触发挂载 effect 重建
const socketFactory: ConsoleSocketFactory = (url) => new FakeSocket(url);
const terminalFactory: TerminalFactory = (theme) => {
  const term = new FakeTerminal(theme);
  lastTerminal = term;
  return term;
};
const fitFactory: FitFactory = () => new FakeFit();

function renderTerminal() {
  return render(
    <ConsoleTerminal
      socketFactory={socketFactory}
      terminalFactory={terminalFactory}
      fitFactory={fitFactory}
    />,
  );
}

function latestSocket(): FakeSocket {
  return FakeSocket.instances[FakeSocket.instances.length - 1];
}

const textDecoder = new TextDecoder();

beforeEach(() => {
  FakeSocket.instances = [];
  lastTerminal = null;
});

afterEach(() => {
  // 主题 store 复位(全局单例,防测试间串状态)
  useUiStore.getState().setTheme("light");
});

describe("ConsoleTerminal 组件", () => {
  test("懒连接:挂载即建同源 WS,状态 连接中;open 后 已连接 + 首个 resize 文本帧", () => {
    renderTerminal();
    expect(FakeSocket.instances).toHaveLength(1);
    expect(latestSocket().url).toBe(consoleWsUrl(window.location.protocol, window.location.host));
    expect(screen.getByText("连接中")).toBeInTheDocument();
    expect(lastTerminal?.opened).toBe(true);
    expect(lastTerminal?.loadAddonCalls).toBe(1);

    act(() => latestSocket().openNow());
    expect(screen.getByText("已连接")).toBeInTheDocument();
    expect(latestSocket().sent[0]).toBe('{"type":"resize","cols":120,"rows":32}');
    expect(lastTerminal?.focusCalls).toBe(1);
  });

  test("二进制帧 → term.write(Uint8Array);文本帧 → 原样写入", () => {
    renderTerminal();
    const socket = latestSocket();
    act(() => socket.openNow());
    act(() => socket.emit(Uint8Array.from([72, 73]).buffer));
    act(() => socket.emit("plain text"));
    const written = lastTerminal?.written ?? [];
    expect(written[0]).toBeInstanceOf(Uint8Array);
    expect(textDecoder.decode(written[0] as Uint8Array)).toBe("HI");
    expect(written[1]).toBe("plain text");
  });

  test("term.onData 键入 → WS 二进制帧发送", () => {
    renderTerminal();
    const socket = latestSocket();
    act(() => socket.openNow());
    act(() => lastTerminal?.dataHandlers[0]("ls -l\n"));
    const payload = socket.sent[socket.sent.length - 1];
    // TextEncoder 产物可能来自 Node realm(jsdom 双 realm),按构造器名 + 解码断言
    expect(typeof payload).not.toBe("string");
    expect((payload as Uint8Array).constructor.name).toBe("Uint8Array");
    expect(textDecoder.decode(payload as Uint8Array)).toBe("ls -l\n");
  });

  test("未连接时键入不发送", () => {
    renderTerminal();
    const socket = latestSocket();
    act(() => lastTerminal?.dataHandlers[0]("x"));
    expect(socket.sent).toHaveLength(0);
  });

  test("窗口尺寸变化:200ms 防抖后发送 clamp 过的 resize 帧", () => {
    vi.useFakeTimers();
    try {
      renderTerminal();
      const socket = latestSocket();
      act(() => socket.openNow());
      const sentBefore = socket.sent.length;
      act(() => {
        fireEvent(window, new Event("resize"));
      });
      expect(socket.sent.length).toBe(sentBefore); // 防抖期内不发
      act(() => {
        vi.advanceTimersByTime(RESIZE_DEBOUNCE_MS);
      });
      expect(socket.sent[sentBefore]).toBe('{"type":"resize","cols":120,"rows":32}');
    } finally {
      vi.useRealTimers();
    }
  });

  test("断开:状态 断开 + 重连按钮 → 点击建新 WS", () => {
    renderTerminal();
    const first = latestSocket();
    act(() => first.openNow());
    act(() => first.closeNow());
    expect(screen.getByText("断开")).toBeInTheDocument();
    const reconnect = screen.getByRole("button", { name: /重新连接/ });
    act(() => {
      fireEvent.click(reconnect);
    });
    expect(FakeSocket.instances).toHaveLength(2);
    expect(screen.getByText("连接中")).toBeInTheDocument();
  });

  test("重载:断开旧 WS + 清屏 + 重连", () => {
    renderTerminal();
    const first = latestSocket();
    act(() => first.openNow());
    act(() => {
      fireEvent.click(screen.getByRole("button", { name: /重载/ }));
    });
    expect(first.closeCalls).toBe(1);
    expect(lastTerminal?.resetCalls).toBe(1);
    expect(FakeSocket.instances).toHaveLength(2);
    expect(screen.getByText("连接中")).toBeInTheDocument();
  });

  test("卸载:dispose 终端 + close WS", () => {
    const { unmount } = renderTerminal();
    const socket = latestSocket();
    act(() => socket.openNow());
    unmount();
    expect(lastTerminal?.disposed).toBe(true);
    expect(socket.closeCalls).toBe(1);
  });

  test("主题联动:data-bs-theme 切换(经 stores/ui)→ term.options.theme 更新", () => {
    renderTerminal();
    // jsdom 无 CSS 变量:回退令牌默认值(亮色白底)
    expect((lastTerminal?.options.theme as { background: string }).background).toBe("#ffffff");
    act(() => {
      useUiStore.getState().setTheme("dark");
    });
    expect(document.documentElement.getAttribute("data-bs-theme")).toBe("dark");
    expect((lastTerminal?.options.theme as { background: string }).background).toBe("#0d1320");
    act(() => {
      useUiStore.getState().setTheme("light");
    });
    expect((lastTerminal?.options.theme as { background: string }).background).toBe("#ffffff");
  });

  test("迟到帧守卫:旧 socket 的 onmessage/onclose 不污染新连接", () => {
    renderTerminal();
    const first = latestSocket();
    act(() => {
      fireEvent.click(screen.getByRole("button", { name: /重载/ }));
    });
    const second = latestSocket();
    act(() => second.openNow());
    // 旧连接迟到的 close/输出全部被忽略
    act(() => {
      first.onclose?.();
      first.emit("stale");
    });
    expect(screen.getByText("已连接")).toBeInTheDocument();
    expect(lastTerminal?.written).toHaveLength(0);
  });
});
