// xterm.js 原生终端组件(设计方案 §6.11):
// - 懒连接:路由首次挂载(本组件 mount)才建 WS,卸载 dispose 终端 + close WS;
// - 帧契约(native_console.go):服务端二进制帧 = PTY 输出 → term.write(Uint8Array);
//   term.onData 键入 → WS 二进制帧;FitAddon 尺寸变化 → 文本帧 {"type":"resize",cols,rows}
//   (cols/rows 1–1000 clamp,200ms 防抖,对齐旧版 console_page.go);
// - 连接状态 StatusChip(连接中/已连接/断开)+ 重连按钮 + "重载"(断开重连 + 清屏);
// - 主题联动:亮暗两套配色读 CSS 变量,data-bs-theme 变化(经 stores/ui)→ term.options.theme
//   赋值刷新(xterm v5 无 v4 setOption,options 赋值即官方等价 API);
// - 渲染与搜索:WebGL 渲染器(adapter 内激活,上下文不可用/丢失自动回落 DOM);
//   SearchAddon 搜索回滚缓冲(工具栏按钮或 Ctrl/Cmd+F 唤起浮层,Enter 下一个、
//   Shift+Enter 上一个、Esc 关闭并清除高亮,无命中时输入框红框 + "无匹配");
// - WS 工厂与 Terminal/Fit/Search 构造全部可注入(props 缺省走 xterm-adapter 真实实现),
//   单测注入 fake 只测状态机/帧序列化/重连,不做真实终端渲染(jsdom 无能力)。
import {
  ChevronDownIcon,
  ChevronUpIcon,
  PlugZapIcon,
  RotateCwIcon,
  SearchIcon,
  SquareTerminalIcon,
  XIcon,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";

import { StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
import {
  defaultFitFactory,
  defaultSearchFactory,
  defaultSocketFactory,
  defaultTerminalFactory,
} from "../xterm-adapter";
import type {
  ConsoleSocketFactory,
  ConsoleSocketLike,
  FitFactory,
  FitLike,
  SearchFactory,
  SearchLike,
  TerminalFactory,
  TerminalLike,
} from "../types";

const TEXT_ENCODER = new TextEncoder();

export interface ConsoleTerminalProps {
  socketFactory?: ConsoleSocketFactory;
  terminalFactory?: TerminalFactory;
  fitFactory?: FitFactory;
  searchFactory?: SearchFactory;
}

export function ConsoleTerminal({
  socketFactory = defaultSocketFactory,
  terminalFactory = defaultTerminalFactory,
  fitFactory = defaultFitFactory,
  searchFactory = defaultSearchFactory,
}: ConsoleTerminalProps) {
  const { t } = useT("console");
  const theme = useTerminalTheme();
  const [connection, setConnection] = useState<ConsoleConnectionState>("idle");
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchNoMatch, setSearchNoMatch] = useState(false);
  // SearchAddon 挂载成功与否(effect 内创建;ref 不触发渲染,以 state 驱动按钮显隐)
  const [searchAvailable, setSearchAvailable] = useState(false);

  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<TerminalLike | null>(null);
  const fitRef = useRef<FitLike | null>(null);
  const searchRef = useRef<SearchLike | null>(null);
  const socketRef = useRef<ConsoleSocketLike | null>(null);
  const resizeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // 工厂经 ref 读取:保持挂载 effect 依赖稳定(终端/WS 只创建一次)
  const factoriesRef = useRef({ socketFactory, terminalFactory, fitFactory, searchFactory });
  factoriesRef.current = { socketFactory, terminalFactory, fitFactory, searchFactory };
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

  /** 执行搜索(Enter/按钮/输入变化);空查询清除高亮,未命中置红框状态。 */
  const runFind = useCallback((query: string, direction: "next" | "previous") => {
    const search = searchRef.current;
    if (!search) return;
    if (query === "") {
      search.clearDecorations();
      setSearchNoMatch(false);
      return;
    }
    const found =
      direction === "next" ? search.findNext(query) : search.findPrevious(query);
    setSearchNoMatch(!found);
  }, []);

  const closeSearch = useCallback(() => {
    searchRef.current?.clearDecorations();
    setSearchOpen(false);
    setSearchQuery("");
    setSearchNoMatch(false);
    termRef.current?.focus();
  }, []);

  /** Ctrl/Cmd+F 唤起搜索(拦截浏览器页内查找);键入事件从 xterm 内部冒泡至此。 */
  const handleContainerKeyDown = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
        event.preventDefault();
        setSearchOpen(true);
      }
    },
    [],
  );

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
    // SearchAddon 须挂到已 open 的终端;工厂失败返回 null,搜索按钮随之隐藏
    searchRef.current = factoriesRef.current.searchFactory(term);
    setSearchAvailable(searchRef.current !== null);
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
    // Ctrl/Cmd+F 官方拦截口:xterm 对已处理按键会 stopPropagation(Ctrl+F 本会
    // 作为 \x06 写入 PTY),容器 onKeyDown 收不到;返回 false 让 xterm 放弃处理。
    term.attachCustomKeyEventHandler((event) => {
      if (
        event.type === "keydown" &&
        (event.ctrlKey || event.metaKey) &&
        event.key.toLowerCase() === "f"
      ) {
        setSearchOpen(true);
        return false;
      }
      return true;
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
      searchRef.current?.dispose();
      searchRef.current = null;
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
          {searchAvailable && (
            <Button
              variant="ghost"
              size="sm"
              aria-label={t("searchToggle")}
              aria-pressed={searchOpen}
              title={t("searchToggle")}
              onClick={() => (searchOpen ? closeSearch() : setSearchOpen(true))}
            >
              <SearchIcon />
            </Button>
          )}
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
      <div className="relative">
        <div
          ref={containerRef}
          role="application"
          aria-label={t("terminalAria")}
          onKeyDown={handleContainerKeyDown}
          className="h-[60svh] min-h-72 w-full overflow-hidden rounded-sm"
        />
        {searchOpen && (
          <div
            role="search"
            className="absolute right-2 top-2 z-10 flex items-center gap-1 rounded-sm border border-line bg-surface p-1.5 shadow-md"
          >
            <Input
              autoFocus
              aria-label={t("searchPlaceholder")}
              placeholder={t("searchPlaceholder")}
              className="h-7 w-44 text-xs"
              value={searchQuery}
              invalid={searchNoMatch}
              onChange={(event) => {
                const value = event.target.value;
                setSearchQuery(value);
                runFind(value, "next");
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  runFind(searchQuery, event.shiftKey ? "previous" : "next");
                } else if (event.key === "Escape") {
                  event.preventDefault();
                  closeSearch();
                }
                // 搜索框键入不得落入终端
                event.stopPropagation();
              }}
            />
            {searchNoMatch && (
              <span className="text-xs text-danger" data-slot="search-no-match">
                {t("searchNoMatch")}
              </span>
            )}
            <Button
              variant="ghost"
              size="sm"
              aria-label={t("searchPrev")}
              title={t("searchPrev")}
              disabled={searchQuery === ""}
              onClick={() => runFind(searchQuery, "previous")}
            >
              <ChevronUpIcon />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              aria-label={t("searchNext")}
              title={t("searchNext")}
              disabled={searchQuery === ""}
              onClick={() => runFind(searchQuery, "next")}
            >
              <ChevronDownIcon />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              aria-label={t("searchClose")}
              title={t("searchClose")}
              onClick={closeSearch}
            >
              <XIcon />
            </Button>
          </div>
        )}
      </div>
    </Panel>
  );
}
