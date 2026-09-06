// xterm.js 原生终端组件(设计方案 §6.11):
// - 懒连接:路由首次挂载(本组件 mount)才建 WS,卸载 dispose 终端 + close WS;
// - 帧契约(native_console.go):服务端二进制帧 = PTY 输出 → term.write(Uint8Array);
//   term.onData 键入 → WS 二进制帧;FitAddon 尺寸变化 → 文本帧 {"type":"resize",cols,rows}
//   (cols/rows 1–1000 clamp,200ms 防抖,对齐旧版 console_page.go);
// - 连接状态 StatusChip(连接中/已连接/断开)+ 重连按钮 + "重载"(断开重连 + 清屏);
// - 主题联动:亮暗两套配色读 CSS 变量,data-bs-theme 变化(经 stores/ui)→ term.options.theme
//   赋值刷新(xterm v5 无 v4 setOption,options 赋值即官方等价 API);
// - WS 工厂与 Terminal/Fit 构造全部可注入(props 缺省走 xterm-adapter 真实实现),
//   单测注入 fake 只测状态机/帧序列化/重连,不做真实终端渲染(jsdom 无能力)。
import { PlugZapIcon, RotateCwIcon, SquareTerminalIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";

import { useTerminalTheme } from "../hooks";
import {
  buildResizeFrame,
  consoleWsUrl,
  decodeConsoleFrame,
  reduceConsoleConnection,
  RESIZE_DEBOUNCE_MS,
  WS_CONNECTING,
  WS_OPEN,
} from "../lib";
import type { ConsoleConnectionState } from "../lib";
import { defaultFitFactory, defaultSocketFactory, defaultTerminalFactory } from "../xterm-adapter";
import type {
  ConsoleSocketFactory,
  ConsoleSocketLike,
  FitFactory,
  FitLike,
  TerminalFactory,
  TerminalLike,
} from "../types";

const TEXT_ENCODER = new TextEncoder();

export interface ConsoleTerminalProps {
  socketFactory?: ConsoleSocketFactory;
  terminalFactory?: TerminalFactory;
  fitFactory?: FitFactory;
}

export function ConsoleTerminal({
  socketFactory = defaultSocketFactory,
  terminalFactory = defaultTerminalFactory,
  fitFactory = defaultFitFactory,
}: ConsoleTerminalProps) {
  const { t } = useT("console");
  const theme = useTerminalTheme();
  const [connection, setConnection] = useState<ConsoleConnectionState>("idle");

  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<TerminalLike | null>(null);
  const fitRef = useRef<FitLike | null>(null);
  const socketRef = useRef<ConsoleSocketLike | null>(null);
  const resizeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // 工厂经 ref 读取:保持挂载 effect 依赖稳定(终端/WS 只创建一次)
  const factoriesRef = useRef({ socketFactory, terminalFactory, fitFactory });
  factoriesRef.current = { socketFactory, terminalFactory, fitFactory };
  const themeRef = useRef(theme);
  themeRef.current = theme;

  const dispatch = useCallback((event: Parameters<typeof reduceConsoleConnection>[1]) => {
    setConnection((prev) => reduceConsoleConnection(prev, event));
  }, []);

  /** fit 后把最新行列经文本帧上报(clamp 1–1000 在 buildResizeFrame 内)。 */
  const sendResize = useCallback(() => {
    const socket = socketRef.current;
    const term = termRef.current;
    if (!socket || socket.readyState !== WS_OPEN || !term) return;
    try {
      fitRef.current?.fit();
    } catch {
      // 容器不可见等 fit 失败场景忽略,沿用当前行列
    }
    socket.send(buildResizeFrame(term.cols, term.rows));
  }, []);

  const scheduleResize = useCallback(() => {
    if (resizeTimerRef.current !== null) clearTimeout(resizeTimerRef.current);
    resizeTimerRef.current = setTimeout(() => {
      resizeTimerRef.current = null;
      sendResize();
    }, RESIZE_DEBOUNCE_MS);
  }, [sendResize]);

  const connect = useCallback(() => {
    const existing = socketRef.current;
    if (existing && (existing.readyState === WS_CONNECTING || existing.readyState === WS_OPEN)) {
      return;
    }
    // 同源地址:ws|wss 按页面协议,会话 Cookie 随握手自动携带
    const socket = factoriesRef.current.socketFactory(
      consoleWsUrl(window.location.protocol, window.location.host),
    );
    socketRef.current = socket;
    dispatch("connect");
    socket.onopen = () => {
      if (socketRef.current !== socket) return;
      dispatch("open");
      termRef.current?.focus();
      sendResize();
    };
    socket.onclose = () => {
      if (socketRef.current !== socket) return;
      socketRef.current = null;
      dispatch("close");
    };
    socket.onerror = () => {
      // WS 语义 error 后必随 close,状态由 close 统一收尾
    };
    socket.onmessage = (event) => {
      if (socketRef.current !== socket) return;
      const term = termRef.current;
      if (!term) return;
      const payload = decodeConsoleFrame(event.data);
      if (payload !== null) term.write(payload);
    };
  }, [dispatch, sendResize]);

  /** 重载:断开重连 + 清屏(保留旧版语义)。 */
  const reload = useCallback(() => {
    const socket = socketRef.current;
    socketRef.current = null;
    if (socket) {
      try {
        socket.close();
      } catch {
        // 关闭失败不影响重建
      }
    }
    termRef.current?.reset();
    // 先按断开收尾:connect 的幂等守卫否则会把"已连接"状态原样吞掉
    dispatch("close");
    connect();
  }, [connect, dispatch]);

  // 挂载:创建终端(注入工厂)→ 懒连接 → 尺寸观察;卸载:dispose + close(§6.11)
  useEffect(() => {
    const element = containerRef.current;
    if (!element) return;
    const term = factoriesRef.current.terminalFactory(themeRef.current);
    const fit = factoriesRef.current.fitFactory();
    term.loadAddon(fit);
    term.open(element);
    termRef.current = term;
    fitRef.current = fit;
    try {
      fit.fit();
    } catch {
      // jsdom/隐藏容器 fit 失败忽略,连接后 resize 帧仍按 term 行列发送
    }
    const subscription = term.onData((data) => {
      const socket = socketRef.current;
      // 键入始终以二进制帧写入 PTY(文本帧保留给 JSON 控制消息)
      if (socket && socket.readyState === WS_OPEN) socket.send(TEXT_ENCODER.encode(data));
    });
    connect();

    const onWindowResize = () => scheduleResize();
    window.addEventListener("resize", onWindowResize);
    // 容器尺寸变化(侧栏折叠/窗口拖拽)同样触发 fit + resize 帧;jsdom 无 ResizeObserver 时守卫
    let observer: ResizeObserver | undefined;
    if (typeof ResizeObserver !== "undefined") {
      observer = new ResizeObserver(onWindowResize);
      observer.observe(element);
    }
    return () => {
      window.removeEventListener("resize", onWindowResize);
      observer?.disconnect();
      if (resizeTimerRef.current !== null) {
        clearTimeout(resizeTimerRef.current);
        resizeTimerRef.current = null;
      }
      subscription.dispose();
      const socket = socketRef.current;
      socketRef.current = null;
      if (socket) {
        try {
          socket.close();
        } catch {
          // 卸载关闭失败静默
        }
      }
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, [connect, scheduleResize]);

  // 主题联动:亮暗切换 → term.options.theme 赋值(xterm v5 即时生效)
  useEffect(() => {
    const term = termRef.current;
    if (term) term.options.theme = theme;
  }, [theme]);

  const chip =
    connection === "connected" ? (
      <StatusChip tone="success">{t("statusConnected")}</StatusChip>
    ) : connection === "connecting" ? (
      <StatusChip tone="warning" pulse>
        {t("statusConnecting")}
      </StatusChip>
    ) : connection === "disconnected" ? (
      <StatusChip tone="danger">{t("statusDisconnected")}</StatusChip>
    ) : (
      <StatusChip tone="muted">{t("statusConnecting")}</StatusChip>
    );

  return (
    <Panel
      domain="tools"
      icon={SquareTerminalIcon}
      title={t("terminalTitle")}
      tools={
        <>
          {chip}
          {connection === "disconnected" && (
            <Button variant="secondary" size="sm" onClick={connect}>
              <PlugZapIcon />
              {t("reconnect")}
            </Button>
          )}
          <Button variant="ghost" size="sm" onClick={reload}>
            <RotateCwIcon />
            {t("reload")}
          </Button>
        </>
      }
      footer={t("consoleFooterHint")}
    >
      <div
        ref={containerRef}
        role="application"
        aria-label={t("terminalAria")}
        className="h-[60svh] min-h-72 w-full overflow-hidden rounded-sm"
      />
    </Panel>
  );
}
