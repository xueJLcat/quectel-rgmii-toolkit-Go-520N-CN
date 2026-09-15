// 累计流量 count-up(设计方案 §5.1 数字滚动):rAF + easeOutCubic 600ms,与 MetricCard 同源,
// 但目标值为字节数、每帧经 humanBytes 格式化(旧版 nr_rx_human/nr_tx_human 的动效化)。
import { useEffect, useRef, useState } from "react";

import { cn } from "@/lib/utils";
import { humanBytes } from "../lib";

const COUNT_UP_MS = 600;

export interface CountUpBytesProps {
  /** 累计字节数;非有限值/负数时退化为 fallback 文本,不动画 */
  bytes: number | null;
  /** 服务端人性化文本(nr_rx_human 等),bytes 无效时显示 */
  fallback?: string;
  className?: string;
}

export function CountUpBytes({ bytes, fallback, className }: CountUpBytesProps) {
  const numeric = typeof bytes === "number" && Number.isFinite(bytes) && bytes >= 0;
  const target = numeric ? (bytes as number) : 0;
  const [display, setDisplay] = useState(target);
  const currentRef = useRef(target);
  const frameRef = useRef<number | null>(null);

  useEffect(() => {
    if (!numeric) {
      currentRef.current = 0;
      setDisplay(0);
      return;
    }
    const from = currentRef.current;
    if (from === target) {
      setDisplay(target);
      return;
    }
    const start = performance.now();
    const step = (now: number) => {
      const progress = Math.min(1, Math.max(0, (now - start) / COUNT_UP_MS));
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
  }, [target, numeric]);

  if (!numeric) {
    return <span className={cn("tabular-nums", className)}>{fallback || "-"}</span>;
  }
  return <span className={cn("tabular-nums", className)}>{humanBytes(display)}</span>;
}
