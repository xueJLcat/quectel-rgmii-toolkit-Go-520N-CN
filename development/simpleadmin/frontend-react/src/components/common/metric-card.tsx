// MetricCard(设计方案 §5.1 数字滚动):数值变化 count-up 600ms(rAF + easeOutCubic,
// tabular-nums 防抖动);icon 置于六域渐变圆角方块(§3.1 --sa-grad-{domain} 令牌);
// 卡片 hover 上浮复用 ui/card interactive。value 为字符串或非有限数时原样渲染、不动画;
// decimals 缺省取目标值自身的小数位数。
import type { LucideIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import type { DomainName } from "@/components/charts";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export interface MetricCardProps {
  label: string;
  value: number | string;
  unit?: string;
  icon?: LucideIcon;
  /** 导航六域 → icon 方块渐变底,缺省 monitor */
  domain?: DomainName;
  decimals?: number;
  /** 数字滚动动画,默认开;字符串值自动失效 */
  countUp?: boolean;
  className?: string;
}

const COUNT_UP_MS = 600;

function decimalPlacesOf(value: number): number {
  if (!Number.isFinite(value)) return 0;
  const text = String(value);
  const dot = text.indexOf(".");
  return dot === -1 ? 0 : text.length - dot - 1;
}

/** rAF 驱动的 count-up:返回趋近 target 的显示值;中途改目标从当前帧值平滑重定向。 */
function useCountUp(target: number, duration: number, enabled: boolean): number {
  const [display, setDisplay] = useState(target);
  const currentRef = useRef(target);
  const frameRef = useRef<number | null>(null);

  useEffect(() => {
    if (!enabled || !Number.isFinite(target)) {
      currentRef.current = target;
      setDisplay(target);
      return;
    }
    const from = currentRef.current;
    if (from === target) {
      setDisplay(target);
      return;
    }
    const start = performance.now();
    const step = (now: number) => {
      const progress = Math.min(1, Math.max(0, (now - start) / duration));
      const eased = 1 - Math.pow(1 - progress, 3);
      const next = from + (target - from) * eased;
      currentRef.current = next;
      setDisplay(next);
      frameRef.current = progress < 1 ? requestAnimationFrame(step) : null;
    };
    frameRef.current = requestAnimationFrame(step);
    return () => {
      if (frameRef.current !== null) {
        cancelAnimationFrame(frameRef.current);
        frameRef.current = null;
      }
    };
  }, [target, duration, enabled]);

  return display;
}

export function MetricCard({
  label,
  value,
  unit,
  icon: Icon,
  domain = "monitor",
  decimals,
  countUp = true,
  className,
}: MetricCardProps) {
  const numeric = typeof value === "number" && Number.isFinite(value);
  const places = decimals ?? (numeric ? decimalPlacesOf(value) : 0);
  const animated = useCountUp(numeric ? value : 0, COUNT_UP_MS, numeric && countUp);
  const text = numeric ? animated.toFixed(places) : String(value);
  return (
    <Card interactive className={cn("flex items-center gap-3 p-4", className)}>
      {Icon && (
        <span
          aria-hidden="true"
          className="grid size-10 shrink-0 place-items-center rounded-sm text-white shadow-sm"
          style={{ backgroundImage: `var(--sa-grad-${domain})` }}
        >
          <Icon className="size-5" />
        </span>
      )}
      <div className="min-w-0 flex-1">
        <div className="truncate text-xs text-muted">{label}</div>
        <div className="flex items-baseline gap-1 text-2xl font-semibold text-ink">
          <span data-slot="metric-value" className="tabular-nums">
            {text}
          </span>
          {unit && <span className="text-sm font-medium text-muted">{unit}</span>}
        </div>
      </div>
    </Card>
  );
}
