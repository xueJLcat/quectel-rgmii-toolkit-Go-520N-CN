// charts 目录统一出口:页面只从这里 import(HOC / 纯函数 / 类型),不直接引 echarts。
// ECharts 图表 HOC 共享 useChartTheme(反应式明暗主题),非 ECharts 的 HeatBar/MetricBar 为纯 div。
// 本目录整体作为路由级 lazy chunk 被引用时,echarts 经 vite manualChunks 自然分离为独立 chunk。
import { useMemo } from "react";
import type { CSSProperties } from "react";
import { cn } from "@/lib/utils";
import { EChart, useChartTheme } from "./EChart";
import {
  DOMAIN_GRADIENTS,
  countdownGaugeOption,
  gaugeOption,
  signalQualityColor,
  sparklineOption,
  trendLineOption,
} from "./options";
import type { DomainName, HistoryPoint } from "./options";

export * from "./echarts-core";
export * from "./options";
export * from "./EChart";

function clampPercent(v: number): number {
  return Math.max(0, Math.min(100, Number.isFinite(v) ? v : 0));
}

/**
 * 温度/负载阈值色阶(设计方案 §6.14 温度热条):≤45 emerald / ≤60 amber / ≤75 orange / >75 rose。
 * HeatBar 与 MetricBar(未显式传 color 时)共用;非有限值回退 muted 灰。
 */
export function heatThresholdColor(value: number): string {
  if (!Number.isFinite(value)) return "#6b7280";
  if (value <= 45) return "#10b981";
  if (value <= 60) return "#f59e0b";
  if (value <= 75) return "#f97316";
  return "#f43f5e";
}

export interface GaugeChartProps {
  value: number;
  max?: number;
  /** 中心数字单位后缀,如 "%" / "°C" */
  label?: string;
  /** 仪表下方标题(指标名) */
  title?: string;
  /** 导航域 → 域色渐变描边;缺省则按 value/max 百分比取信号质量色 */
  domain?: DomainName;
  className?: string;
  style?: CSSProperties;
}

export function GaugeChart({
  value,
  max = 100,
  label,
  title,
  domain,
  className,
  style,
}: GaugeChartProps) {
  const theme = useChartTheme();
  const option = useMemo(() => {
    const safeMax = Number.isFinite(max) && max > 0 ? max : 100;
    const colorStops = domain
      ? DOMAIN_GRADIENTS[domain]
      : [signalQualityColor(clampPercent((value / safeMax) * 100), theme.mode)];
    return gaugeOption({ value, max: safeMax, label, title, colorStops, theme });
  }, [value, max, label, title, domain, theme]);
  return <EChart option={option} className={className} style={style} />;
}

export interface TrendChartProps {
  points: readonly HistoryPoint[];
  className?: string;
  style?: CSSProperties;
}

export function TrendChart({ points, className, style }: TrendChartProps) {
  const theme = useChartTheme();
  const option = useMemo(() => trendLineOption({ points, theme }), [points, theme]);
  return <EChart option={option} className={className} style={style} />;
}

export interface SparklineProps {
  data: readonly number[];
  color?: string;
  className?: string;
  style?: CSSProperties;
}

export function Sparkline({ data, color, className, style }: SparklineProps) {
  const option = useMemo(() => sparklineOption({ data, color }), [data, color]);
  return <EChart option={option} className={className} style={style} />;
}

export interface CountdownGaugeProps {
  seconds: number;
  total: number;
  className?: string;
  style?: CSSProperties;
}

export function CountdownGauge({ seconds, total, className, style }: CountdownGaugeProps) {
  const theme = useChartTheme();
  const option = useMemo(
    () => countdownGaugeOption({ seconds, total, theme }),
    [seconds, total, theme],
  );
  return <EChart option={option} className={className} style={style} />;
}

const FILL_TRANSITION =
  "width var(--sa-dur-slow) var(--sa-ease), background-color var(--sa-dur-slow) var(--sa-ease)";

export interface HeatBarProps {
  /** 温度值(°C);宽度按 0–100 量程取百分比 */
  value: number;
  /** 传感器名称,如 "PA" / "PMIC" */
  label?: string;
  unit?: string;
  className?: string;
}

/** 温度热条(非 ECharts):名称 + 阈值色阶值 + 宽度百分比过渡条,暗色经 bg-surface-3 轨道自适应。 */
export function HeatBar({ value, label, unit = "°C", className }: HeatBarProps) {
  const color = heatThresholdColor(value);
  const width = clampPercent(value);
  const display = Number.isFinite(value) ? `${value}${unit}` : "—";
  return (
    <div className={cn("flex flex-col gap-1", className)}>
      <div className="flex items-center justify-between gap-2 text-sm">
        <span className="truncate text-muted">{label}</span>
        <span className="font-semibold tabular-nums" style={{ color }}>
          {display}
        </span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-surface-3">
        <div
          className="h-full rounded-full"
          style={{ width: `${width}%`, backgroundColor: color, transition: FILL_TRANSITION }}
        />
      </div>
    </div>
  );
}

export interface MetricBarProps {
  /** 0–100 百分比 */
  percent: number;
  /** 显式颜色(如信号质量色);缺省按阈值色阶 */
  color?: string;
  className?: string;
}

/** 表格单元格内嵌微型进度条(非 ECharts):CPU%/内存%/RSRP 等,宽度百分比过渡。 */
export function MetricBar({ percent, color, className }: MetricBarProps) {
  const fill = color ?? heatThresholdColor(percent);
  const width = clampPercent(percent);
  return (
    <div className={cn("h-1.5 w-full overflow-hidden rounded-full bg-surface-3", className)}>
      <div
        className="h-full rounded-full"
        style={{ width: `${width}%`, backgroundColor: fill, transition: FILL_TRANSITION }}
      />
    </div>
  );
}
