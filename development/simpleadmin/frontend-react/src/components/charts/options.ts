// 图表 option 纯函数集合:全部无 DOM 依赖、可独立单测。主题相关文本/轴线色/tooltip 底色一律来自入参 theme,
// 由 EChart.tsx 在运行时读取 CSS 变量注入(见 defaultChartTheme 的明暗两套基线值,取自 tokens.css)。
import type { EChartsOption } from "./echarts-core";

export type ChartThemeMode = "light" | "dark";

export interface ChartTheme {
  mode: ChartThemeMode;
  /** 主文本(中心大数字),对应 --sa-text */
  text: string;
  /** 次级文本(标题/轴标签/图例),对应 --sa-muted */
  muted: string;
  /** 轴线与网格线,对应 --sa-border */
  axis: string;
  /** 仪表底环 / 进度条轨道底色 */
  track: string;
  /** tooltip 卡片底色,对应 --sa-surface */
  surface: string;
  /** 图表背景,恒为透明以便融入面板 */
  background: string;
}

// 明暗两套基线,值取自 styles/tokens.css(--sa-*);运行时若 CSS 变量可读则以变量为准。
export function defaultChartTheme(mode: ChartThemeMode): ChartTheme {
  return mode === "dark"
    ? {
        mode,
        text: "#f4f7fb",
        muted: "#aeb8c8",
        axis: "#2b3650",
        track: "#243049",
        surface: "#151c2c",
        background: "transparent",
      }
    : {
        mode,
        text: "#111827",
        muted: "#6b7280",
        axis: "#d8dee8",
        track: "#eef1f5",
        surface: "#ffffff",
        background: "transparent",
      };
}

// 松类型视图:仅用于 applyChartTheme 就地改写轴线/文本色,展开复制保留 series 内的 formatter 函数。
interface LooseAxis {
  axisLine?: { lineStyle?: Record<string, unknown> };
  axisTick?: { lineStyle?: Record<string, unknown> };
  splitLine?: { lineStyle?: Record<string, unknown> };
  axisLabel?: Record<string, unknown>;
  nameTextStyle?: Record<string, unknown>;
}

function themeAxis(axis: LooseAxis | undefined, theme: ChartTheme): LooseAxis | undefined {
  if (!axis) return axis;
  const next: LooseAxis = { ...axis };
  if (axis.axisLine) {
    next.axisLine = {
      ...axis.axisLine,
      lineStyle: { ...axis.axisLine.lineStyle, color: theme.axis },
    };
  }
  if (axis.axisTick) {
    next.axisTick = {
      ...axis.axisTick,
      lineStyle: { ...axis.axisTick.lineStyle, color: theme.axis },
    };
  }
  if (axis.splitLine) {
    next.splitLine = {
      ...axis.splitLine,
      lineStyle: { ...axis.splitLine.lineStyle, color: theme.axis },
    };
  }
  if (axis.axisLabel) next.axisLabel = { ...axis.axisLabel, color: theme.muted };
  if (axis.nameTextStyle) next.nameTextStyle = { ...axis.nameTextStyle, color: theme.muted };
  return next;
}

function themeAxes<T>(axes: T, theme: ChartTheme): T {
  if (Array.isArray(axes)) {
    return axes.map((a) => themeAxis(a as LooseAxis, theme)) as unknown as T;
  }
  return themeAxis(axes as LooseAxis, theme) as unknown as T;
}

/**
 * 运行时主题覆盖:EChart 读到 CSS 变量后,把文本色(--sa-muted)、轴线/网格线色(--sa-border)、
 * tooltip 底色(--sa-surface)、透明背景盖到 option 上;series(域色/质量色渐变、formatter 函数)保持不变由构建方决定。
 */
export function applyChartTheme(option: EChartsOption, theme: ChartTheme): EChartsOption {
  const src = option as unknown as Record<string, unknown>;
  const next: Record<string, unknown> = {
    ...src,
    backgroundColor: theme.background,
    textStyle: {
      ...(src.textStyle as Record<string, unknown> | undefined),
      color: theme.muted,
    },
  };
  if (src.xAxis !== undefined) next.xAxis = themeAxes(src.xAxis, theme);
  if (src.yAxis !== undefined) next.yAxis = themeAxes(src.yAxis, theme);
  const legend = src.legend as Record<string, unknown> | undefined;
  if (legend) {
    next.legend = {
      ...legend,
      textStyle: {
        ...(legend.textStyle as Record<string, unknown> | undefined),
        color: theme.muted,
      },
      inactiveColor: theme.axis,
    };
  }
  // tooltip 卡片主题化:ECharts 默认白底,暗色下浅字不可读;统一注入底色/边框/文字色。
  if (src.tooltip !== undefined) {
    const tooltip = src.tooltip as Record<string, unknown>;
    next.tooltip = {
      ...tooltip,
      backgroundColor: theme.surface,
      borderColor: theme.axis,
      textStyle: {
        ...(tooltip.textStyle as Record<string, unknown> | undefined),
        color: theme.text,
      },
    };
  }
  return next as unknown as EChartsOption;
}

/** 六大导航域 → 渐变描边色对(取自 tokens.css --sa-grad-*),GaugeChart 的 domain 映射用。 */
export type DomainName = "monitor" | "network" | "security" | "comm" | "tools" | "system";

export const DOMAIN_GRADIENTS: Record<DomainName, [string, string]> = {
  monitor: ["#3b82f6", "#06b6d4"],
  network: ["#10b981", "#14b8a6"],
  security: ["#f59e0b", "#f97316"],
  comm: ["#8b5cf6", "#d946ef"],
  tools: ["#06b6d4", "#0ea5e9"],
  system: ["#f43f5e", "#ec4899"],
};

/** 信号质量四档(全站统一,设计方案 §3.2):优秀 emerald / 良好 lime / 一般 amber / 差 rose。 */
export type SignalQuality = "excellent" | "good" | "fair" | "poor";

const QUALITY_COLORS: Record<ChartThemeMode, Record<SignalQuality, string>> = {
  light: { excellent: "#10b981", good: "#84cc16", fair: "#f59e0b", poor: "#f43f5e" },
  dark: { excellent: "#34d399", good: "#a3e635", fair: "#fbbf24", poor: "#fb7185" },
};

/** 百分比 → 质量档阈值(0–100):≥80 优秀 / ≥60 良好 / ≥40 一般 / 其余差;非有限值归差。 */
export function signalQualityFromPercent(percent: number): SignalQuality {
  if (!Number.isFinite(percent)) return "poor";
  if (percent >= 80) return "excellent";
  if (percent >= 60) return "good";
  if (percent >= 40) return "fair";
  return "poor";
}

/** 质量档或百分比 → 质量色阶 hex;明暗两套由 mode 决定。 */
export function signalQualityColor(
  quality: number | SignalQuality,
  mode: ChartThemeMode = "light",
): string {
  const key = typeof quality === "number" ? signalQualityFromPercent(quality) : quality;
  return QUALITY_COLORS[mode][key];
}

interface LinearGradient {
  type: "linear";
  x: number;
  y: number;
  x2: number;
  y2: number;
  colorStops: { offset: number; color: string }[];
  global: boolean;
}

/** hex(#rgb / #rrggbb)→ rgba();非 hex 原样返回,alpha 夹在 0–1。 */
function hexToRgba(hex: string, alpha: number): string {
  const a = Math.min(1, Math.max(0, alpha));
  const body = hex.replace("#", "").trim();
  const full =
    body.length === 3
      ? body
          .split("")
          .map((c) => c + c)
          .join("")
      : body;
  if (full.length !== 6) return hex;
  const r = parseInt(full.slice(0, 2), 16);
  const g = parseInt(full.slice(2, 4), 16);
  const b = parseInt(full.slice(4, 6), 16);
  if (Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)) return hex;
  return `rgba(${r}, ${g}, ${b}, ${a})`;
}

/** 纵向面积渐变:顶部 alpha 0.25 → 底部全透明(设计方案趋势图 area 规格)。 */
function areaGradient(hex: string): LinearGradient {
  return {
    type: "linear",
    x: 0,
    y: 0,
    x2: 0,
    y2: 1,
    colorStops: [
      { offset: 0, color: hexToRgba(hex, 0.25) },
      { offset: 1, color: hexToRgba(hex, 0) },
    ],
    global: false,
  };
}

/** 描边渐变:1 色→纯色,多色→沿对角均匀分布的线性渐变;空数组→透明兜底。 */
function strokeGradient(colorStops: readonly string[]): LinearGradient | string {
  const stops = colorStops.filter((c): c is string => typeof c === "string" && c.length > 0);
  if (stops.length === 0) return "transparent";
  if (stops.length === 1) return stops[0];
  return {
    type: "linear",
    x: 0,
    y: 0,
    x2: 1,
    y2: 1,
    colorStops: stops.map((color, i) => ({ offset: i / (stops.length - 1), color })),
    global: false,
  };
}

function finiteOrNull(v: unknown): number | null {
  return typeof v === "number" && Number.isFinite(v) ? v : null;
}

/** 字节/秒人性化(右轴刻度与 tooltip 用);< 1024 直接 B/s。 */
export function humanBytesPerSec(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B/s";
  const units = ["B/s", "KB/s", "MB/s", "GB/s"];
  let value = bytes;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i += 1;
  }
  return `${value >= 100 || Number.isInteger(value) ? Math.round(value) : value.toFixed(1)} ${
    units[i]
  }`;
}

export interface GaugeParams {
  value: number;
  max?: number;
  /** 中心大数字的单位后缀,如 "%" / "°C" / " dBm" */
  label?: string;
  /** 仪表下方 13px muted 标题(指标名) */
  title?: string;
  /** 渐变描边色对(域色 / 质量色);缺省用主题文本色 */
  colorStops?: readonly string[];
  theme: ChartTheme;
}

/**
 * 半环仪表:startAngle 210 / endAngle -30(开口朝下的 3/4 环,单指标观感最稳,故不选全环)。
 * progress 动画 800ms cubic-out(§5.1);渐变描边;中心 24px/700 tabular 数字 + 13px muted 标题。
 */
export function gaugeOption({
  value,
  max = 100,
  label = "",
  title = "",
  colorStops,
  theme,
}: GaugeParams): EChartsOption {
  const safeValue = Number.isFinite(value) ? value : 0;
  const shown = Math.round(safeValue);
  return {
    backgroundColor: theme.background,
    series: [
      {
        type: "gauge",
        startAngle: 210,
        endAngle: -30,
        min: 0,
        max: Number.isFinite(max) && max > 0 ? max : 100,
        radius: "100%",
        center: ["50%", "58%"],
        progress: {
          show: true,
          width: 12,
          roundCap: true,
          itemStyle: { color: strokeGradient(colorStops ?? [theme.text]) },
        },
        axisLine: { lineStyle: { width: 12, color: [[1, theme.track]] } },
        pointer: { show: false },
        anchor: { show: false },
        axisTick: { show: false },
        splitLine: { show: false },
        axisLabel: { show: false },
        title: { show: true, offsetCenter: [0, "38%"], fontSize: 13, color: theme.muted },
        detail: {
          valueAnimation: true,
          offsetCenter: [0, 0],
          fontSize: 24,
          fontWeight: 700,
          color: theme.text,
          formatter: `{value}${label}`,
        },
        data: [{ value: shown, name: title }],
        animationDuration: 800,
        animationEasing: "cubicOut",
      },
    ],
  };
}

/** 后端 /api/history_data 单点结构(见 metrics_history.go);字段均 omitempty,前端必须容错。 */
export interface HistoryPoint {
  t?: number;
  cpu?: number;
  ram?: number;
  sig?: number;
  rsrp?: number;
  rx?: number;
  tx?: number;
  rxr?: number;
  txr?: number;
}

function formatHHmm(unixSeconds: number | undefined): string {
  if (typeof unixSeconds !== "number" || !Number.isFinite(unixSeconds)) return "";
  const d = new Date(unixSeconds * 1000);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  return `${hh}:${mm}`;
}

const TREND_SIGNAL = "#10b981";
const TREND_DL = "#3b82f6";
const TREND_UL = "#8b5cf6";

/**
 * 双 y 轴趋势图:左轴信号 % 0–100(线 + 渐变 area,opacity 0.25→0),右轴速率 bytes/s(DL/UL 两线)。
 * x 轴时间 HH:mm;tooltip axis 十字准星;legend 顶部;网格线用主题轴线色。空数组 / 缺字段容错为 null。
 */
export function trendLineOption({
  points,
  theme,
}: {
  points: readonly HistoryPoint[];
  theme: ChartTheme;
}): EChartsOption {
  const safe = Array.isArray(points) ? points : [];
  const xs = safe.map((p) => formatHHmm(p?.t));
  const sig = safe.map((p) => finiteOrNull(p?.sig));
  const dl = safe.map((p) => finiteOrNull(p?.rxr));
  const ul = safe.map((p) => finiteOrNull(p?.txr));
  return {
    backgroundColor: theme.background,
    grid: { left: 8, right: 8, top: 36, bottom: 8, containLabel: true },
    tooltip: {
      trigger: "axis",
      axisPointer: { type: "cross" },
      textStyle: { color: theme.text },
    },
    legend: {
      top: 0,
      data: ["信号", "下载", "上传"],
      textStyle: { color: theme.muted },
      inactiveColor: theme.axis,
    },
    xAxis: {
      type: "category",
      boundaryGap: false,
      data: xs,
      axisLine: { lineStyle: { color: theme.axis } },
      axisLabel: { color: theme.muted },
    },
    yAxis: [
      {
        type: "value",
        min: 0,
        max: 100,
        name: "%",
        nameTextStyle: { color: theme.muted },
        axisLabel: { color: theme.muted },
        splitLine: { lineStyle: { color: theme.axis } },
      },
      {
        type: "value",
        min: 0,
        name: "B/s",
        nameTextStyle: { color: theme.muted },
        axisLabel: { color: theme.muted, formatter: (v: number) => humanBytesPerSec(v) },
        splitLine: { show: false },
      },
    ],
    series: [
      {
        name: "信号",
        type: "line",
        yAxisIndex: 0,
        smooth: true,
        showSymbol: false,
        data: sig,
        lineStyle: { width: 2, color: TREND_SIGNAL },
        itemStyle: { color: TREND_SIGNAL },
        areaStyle: { color: areaGradient(TREND_SIGNAL) },
      },
      {
        name: "下载",
        type: "line",
        yAxisIndex: 1,
        smooth: true,
        showSymbol: false,
        data: dl,
        lineStyle: { width: 1.5, color: TREND_DL },
        itemStyle: { color: TREND_DL },
      },
      {
        name: "上传",
        type: "line",
        yAxisIndex: 1,
        smooth: true,
        showSymbol: false,
        data: ul,
        lineStyle: { width: 1.5, color: TREND_UL },
        itemStyle: { color: TREND_UL },
      },
    ],
  };
}

/** 微型线(接口速率 Sparkline):无轴无 tooltip,smooth,线宽 1.5,area 淡渐变;高度由容器决定。 */
export function sparklineOption({
  data,
  color = TREND_SIGNAL,
}: {
  data: readonly number[];
  color?: string;
}): EChartsOption {
  const safe = (Array.isArray(data) ? data : []).map(finiteOrNull);
  return {
    backgroundColor: "transparent",
    animation: false,
    grid: { left: 0, right: 0, top: 2, bottom: 2 },
    xAxis: {
      type: "category",
      show: false,
      boundaryGap: false,
      data: safe.map((_, i) => i),
    },
    yAxis: { type: "value", show: false },
    series: [
      {
        type: "line",
        smooth: true,
        showSymbol: false,
        data: safe,
        lineStyle: { width: 1.5, color },
        itemStyle: { color },
        areaStyle: { color: areaGradient(color) },
      },
    ],
  };
}

const COUNTDOWN_GRADIENT: [string, string] = ["#f43f5e", "#f97316"];

/**
 * 重启倒计时环:全环(90 / -270),danger 渐变描边,中心显示剩余秒数。
 * progress 表示 seconds/total 的剩余比例;theme 可选(默认亮色,全屏遮罩通常传暗色)。
 */
export function countdownGaugeOption({
  seconds,
  total,
  theme = defaultChartTheme("light"),
}: {
  seconds: number;
  total: number;
  theme?: ChartTheme;
}): EChartsOption {
  const safeTotal = Number.isFinite(total) && total > 0 ? total : 1;
  const safeSeconds = Math.max(0, Number.isFinite(seconds) ? seconds : 0);
  return {
    backgroundColor: theme.background,
    series: [
      {
        type: "gauge",
        startAngle: 90,
        endAngle: -270,
        min: 0,
        max: safeTotal,
        radius: "90%",
        progress: {
          show: true,
          width: 10,
          roundCap: true,
          itemStyle: { color: strokeGradient(COUNTDOWN_GRADIENT) },
        },
        axisLine: { lineStyle: { width: 10, color: [[1, theme.track]] } },
        pointer: { show: false },
        anchor: { show: false },
        axisTick: { show: false },
        splitLine: { show: false },
        axisLabel: { show: false },
        title: { show: false },
        detail: {
          valueAnimation: true,
          offsetCenter: [0, 0],
          fontSize: 28,
          fontWeight: 700,
          color: theme.text,
          formatter: "{value}",
        },
        data: [{ value: safeSeconds }],
        animationDuration: 800,
        animationEasing: "cubicOut",
      },
    ],
  };
}

export interface BarCompareItem {
  label: string;
  value: number;
  color?: string;
}

/** 诊断页延迟对比横向 bar:y 轴类目、x 轴数值,每条目可带独立颜色;空数组容错。 */
export function barCompareOption({
  items,
  theme,
}: {
  items: readonly BarCompareItem[];
  theme: ChartTheme;
}): EChartsOption {
  const safe = Array.isArray(items) ? items : [];
  return {
    backgroundColor: theme.background,
    grid: { left: 8, right: 24, top: 8, bottom: 8, containLabel: true },
    tooltip: { trigger: "item", textStyle: { color: theme.text } },
    xAxis: {
      type: "value",
      axisLabel: { color: theme.muted },
      splitLine: { lineStyle: { color: theme.axis } },
    },
    yAxis: {
      type: "category",
      data: safe.map((i) => i.label),
      axisLine: { lineStyle: { color: theme.axis } },
      axisLabel: { color: theme.muted },
    },
    series: [
      {
        type: "bar",
        barWidth: "60%",
        data: safe.map((i) => ({
          value: finiteOrNull(i.value) ?? 0,
          itemStyle: { color: i.color ?? DOMAIN_GRADIENTS.tools[0], borderRadius: [0, 4, 4, 0] },
        })),
        label: { show: true, position: "right", color: theme.muted, formatter: "{c}" },
      },
    ],
  };
}
