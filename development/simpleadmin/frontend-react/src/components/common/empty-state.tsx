// 空态(设计方案 §3.3 骨架屏/空态/错误重试三件套):居中 muted 虚线框卡,
// 可选图标(surface-3 底圆角方块)与 action 插槽;文案一律由调用方经 i18n 传入。
// 注意 §5.2 纪律:pending(开机保护期)绝不显示假空态,由调用方门控。
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

export interface EmptyStateProps {
  icon?: LucideIcon;
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
}

export function EmptyState({ icon: Icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-2 rounded-md border border-dashed border-line px-6 py-12 text-center",
        className,
      )}
    >
      {Icon && (
        <span
          aria-hidden="true"
          className="mb-1 grid size-12 place-items-center rounded-sm bg-surface-3 text-muted"
        >
          <Icon className="size-6" />
        </span>
      )}
      <div className="text-sm font-semibold text-soft">{title}</div>
      {description && <p className="max-w-sm text-sm text-muted">{description}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}
