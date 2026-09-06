// StatusChip(设计方案 §3.3):圆点 + 文本 pill,替换一切裸文本状态。
// 复用 ui/badge 组合(variant 即 tone 的 *-soft 底/语义文字色令牌),pulse 时圆点叠加 animate-ping 光晕。
import { Badge, BadgeDot } from "@/components/ui/badge";
import type { BadgeProps } from "@/components/ui/badge";
import type * as React from "react";

export type StatusTone = "success" | "warning" | "danger" | "info" | "muted" | "accent";

const TONE_VARIANT: Record<StatusTone, NonNullable<BadgeProps["variant"]>> = {
  success: "success",
  warning: "warning",
  danger: "danger",
  info: "info",
  muted: "muted",
  accent: "default",
};

export interface StatusChipProps extends React.ComponentProps<"span"> {
  tone?: StatusTone;
  /** 圆点 animate-ping 呼吸光晕(在线/进行中等"活着"的状态) */
  pulse?: boolean;
}

export function StatusChip({ tone = "muted", pulse = false, children, ...props }: StatusChipProps) {
  return (
    <Badge variant={TONE_VARIANT[tone]} {...props}>
      <span className="relative flex size-1.5 shrink-0">
        {pulse && (
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-current opacity-75" />
        )}
        <BadgeDot />
      </span>
      {children}
    </Badge>
  );
}
