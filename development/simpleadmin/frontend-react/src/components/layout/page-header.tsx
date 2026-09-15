// PageHeader(设计方案 §4.1):页面标题 + muted 描述 + 右侧页面专属操作区;
// 标题左侧 4px 域色渐变竖条装饰(圆角),渐变值复用 panel.tsx 的域色映射表。
import type { ReactNode } from "react";

import { cn } from "@/lib/utils";
import { DOMAIN_GRADIENT } from "./panel";
import type { PanelDomain } from "./panel";

export interface PageHeaderProps {
  title: string;
  description?: string;
  /** 右侧操作区(按钮等) */
  actions?: ReactNode;
  domain?: PanelDomain;
  className?: string;
}

export function PageHeader({
  title,
  description,
  actions,
  domain = "monitor",
  className,
}: PageHeaderProps) {
  return (
    <header data-slot="page-header" className={cn("mb-4 flex items-start gap-3", className)}>
      <span
        aria-hidden="true"
        style={{ background: DOMAIN_GRADIENT[domain] }}
        className="mt-1 h-8 w-1 shrink-0 rounded-full"
      />
      <div className="min-w-0 flex-1">
        <h1 className="text-ink">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </header>
  );
}
