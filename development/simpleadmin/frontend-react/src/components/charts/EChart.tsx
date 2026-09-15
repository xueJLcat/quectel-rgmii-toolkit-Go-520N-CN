// ECharts 基础 React 封装(未装 echarts-for-react,自写轻量版):
// init → setOption(notMerge:false 平滑过渡)→ ResizeObserver 自适应 → dispose 清理;
// 主题感知:模块级单例 MutationObserver 监听 <html data-bs-theme>,变化时读 CSS 变量重设 option;
// 无 canvas 环境(jsdom)确定性降级为 <div data-chart-fallback>,不抛错。
import { useCallback, useEffect, useRef, useState } from "react";
import type { CSSProperties } from "react";
import { echarts } from "./echarts-core";
import type { EChartsOption, EChartsType } from "./echarts-core";
import { applyChartTheme, defaultChartTheme } from "./options";
import type { ChartTheme, ChartThemeMode } from "./options";

function readThemeMode(): ChartThemeMode {
  if (typeof document === "undefined") return "light";
  return document.documentElement.getAttribute("data-bs-theme") === "dark" ? "dark" : "light";
}

function cssVar(name: string): string {
  if (typeof window === "undefined") return "";
  return window.getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/** 读取当前主题的图表色板:优先 CSS 变量,读不到(jsdom)则回退 tokens 基线值。 */
export function readChartTheme(): ChartTheme {
  const mode = readThemeMode();
  const base = defaultChartTheme(mode);
  return {
    mode,
    text: cssVar("--sa-text") || base.text,
    muted: cssVar("--sa-muted") || base.muted,
    axis: cssVar("--sa-border") || base.axis,
    track: cssVar("--sa-surface-3") || base.track,
    surface: cssVar("--sa-surface") || base.surface,
    background: "transparent",
  };
}

// 单例订阅:无论页面有多少图表,只创建一个 MutationObserver 监听 data-bs-theme。
const themeListeners = new Set<(theme: ChartTheme) => void>();
let themeObserver: MutationObserver | null = null;

function ensureThemeObserver(): void {
  if (themeObserver || typeof MutationObserver === "undefined" || typeof document === "undefined") {
    return;
  }
  themeObserver = new MutationObserver(() => {
    const theme = readChartTheme();
    themeListeners.forEach((listener) => listener(theme));
  });
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-bs-theme"],
  });
}

function subscribeChartTheme(listener: (theme: ChartTheme) => void): () => void {
  themeListeners.add(listener);
  ensureThemeObserver();
  return () => {
    themeListeners.delete(listener);
  };
}

/** 反应式主题 Hook:供各图表 HOC 构建 option(含明暗两套 series 色)并在切换时自动重渲染。 */
export function useChartTheme(): ChartTheme {
  const [theme, setTheme] = useState<ChartTheme>(readChartTheme);
  useEffect(() => subscribeChartTheme(setTheme), []);
  return theme;
}

// jsdom 等无 canvas 2d 上下文的环境:setOption 会抛错,这里提前探测以便确定性降级。
function canRenderCanvas(): boolean {
  if (typeof document === "undefined") return false;
  try {
    return document.createElement("canvas").getContext("2d") !== null;
  } catch {
    return false;
  }
}

export interface EChartProps {
  option: EChartsOption;
  className?: string;
  style?: CSSProperties;
  /** 事件名 → 处理函数,init 后经 chart.on 绑定 */
  onEvents?: Record<string, (params: unknown) => void>;
  /** echarts group,用于多图联动 */
  group?: string;
}

export function EChart({ option, className, style, onEvents, group }: EChartProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const chartRef = useRef<EChartsType | null>(null);
  const lastJsonRef = useRef("");
  const themeRef = useRef<ChartTheme>(readChartTheme());
  const optionRef = useRef<EChartsOption>(option);
  const onEventsRef = useRef(onEvents);
  const groupRef = useRef(group);
  const [failed, setFailed] = useState(false);

  optionRef.current = option;
  onEventsRef.current = onEvents;
  groupRef.current = group;

  const draw = useCallback(() => {
    const chart = chartRef.current;
    if (!chart) return;
    const themed = applyChartTheme(optionRef.current, themeRef.current);
    const json = JSON.stringify(themed);
    if (json === lastJsonRef.current) return; // 深比较去重,避免无谓重绘
    lastJsonRef.current = json;
    try {
      chart.setOption(themed, { notMerge: false });
    } catch {
      setFailed(true);
    }
  }, []);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    if (!canRenderCanvas()) {
      setFailed(true);
      return;
    }

    let chart: EChartsType;
    try {
      chart = echarts.init(el);
    } catch {
      setFailed(true);
      return;
    }
    chartRef.current = chart;

    const groupName = groupRef.current;
    if (groupName) chart.group = groupName;
    const events = onEventsRef.current;
    if (events) {
      for (const [name, handler] of Object.entries(events)) {
        chart.on(name, (params: unknown) => handler(params));
      }
    }

    draw();

    let resizeObserver: ResizeObserver | undefined;
    if (typeof ResizeObserver !== "undefined") {
      resizeObserver = new ResizeObserver(() => {
        try {
          chart.resize();
        } catch {
          /* resize 失败忽略,不影响已渲染内容 */
        }
      });
      resizeObserver.observe(el);
    }

    const unsubscribe = subscribeChartTheme((theme) => {
      themeRef.current = theme;
      draw();
    });

    return () => {
      unsubscribe();
      resizeObserver?.disconnect();
      chart.dispose();
      chartRef.current = null;
      lastJsonRef.current = "";
    };
  }, [draw]);

  useEffect(() => {
    draw();
  }, [option, draw]);

  if (failed) {
    return <div data-chart-fallback className={className} style={style} />;
  }
  return <div ref={containerRef} className={className} style={style} />;
}

export default EChart;
