// Panel(设计方案 §3.3 域色卡片,全站单一实现):头部 = 域色渐变图标 chip + 16px/600 标题 + 右对齐 tools,
// 头部底边 2px 域色渐变条;danger 变体头部 danger-soft 底;interactive 时 hover 上浮 + 域色 glow(§5.1 卡片 hover);
// body p-4;footer bg-surface-2 13px muted。域色 → 类/渐变映射表集中在本文件,壳层与 feature 页共用。
// 根为 flex-col + h-full:stretch 网格中填满单元格,同行卡片等高、footer 贴底;auto 高度语境 h-full 解析为 auto 无副作用。
import type { LucideIcon } from "lucide-react";
import type { CSSProperties, ReactNode } from "react";

import { cn } from "@/lib/utils";

export type PanelDomain = "monitor" | "network" | "security" | "comm" | "tools" | "system";

/** 域色 → 渐变背景(tokens.css 的 var(--sa-grad-*),随明暗主题切换)。 */
export const DOMAIN_GRADIENT: Record<PanelDomain, string> = {
  monitor: "var(--sa-grad-monitor)",
  network: "var(--sa-grad-network)",
  security: "var(--sa-grad-security)",
  comm: "var(--sa-grad-comm)",
  tools: "var(--sa-grad-tools)",
  system: "var(--sa-grad-system)",
};

/** 域色 → soft 底类(激活导航项等大面积弱化底)。 */
export const DOMAIN_SOFT_CLASS: Record<PanelDomain, string> = {
  monitor: "bg-monitor-soft",
  network: "bg-network-soft",
  security: "bg-security-soft",
  comm: "bg-comm-soft",
  tools: "bg-tools-soft",
  system: "bg-system-soft",
};

/** 域色 → 实色文字/图标类。 */
export const DOMAIN_TEXT_CLASS: Record<PanelDomain, string> = {
  monitor: "text-monitor",
  network: "text-network",
  security: "text-security",
  comm: "text-comm",
  tools: "text-tools",
  system: "text-system",
};

/** 域色 → hover glow 阴影(interactive Panel 用,注入 --panel-glow)。 */
export const DOMAIN_GLOW: Record<PanelDomain, string> = {
  monitor: "0 8px 22px -6px var(--sa-domain-monitor)",
  network: "0 8px 22px -6px var(--sa-domain-network)",
  security: "0 8px 22px -6px var(--sa-domain-security)",
  comm: "0 8px 22px -6px var(--sa-domain-comm)",
  tools: "0 8px 22px -6px var(--sa-domain-tools)",
  system: "0 8px 22px -6px var(--sa-domain-system)",
};

export interface PanelProps {
  /** 域色(缺省 monitor);决定图标 chip/渐变条/glow 颜色 */
  domain?: PanelDomain;
  title?: ReactNode;
  icon?: LucideIcon;
  /** 头部右对齐操作区 */
  tools?: ReactNode;
  /** 底部说明条(bg-surface-2,13px muted) */
  footer?: ReactNode;
  /** danger 变体:头部 danger-soft 底(危险操作面板) */
  danger?: boolean;
  /** hover:-translate-y-0.5 + 域色 glow(§5.1 卡片 hover) */
  interactive?: boolean;
  className?: string;
  children?: ReactNode;
}

export function Panel({
  domain = "monitor",
  title,
  icon: Icon,
  tools,
  footer,
  danger = false,
  interactive = false,
  className,
  children,
}: PanelProps) {
  const style = {
    "--panel-grad": DOMAIN_GRADIENT[domain],
    ...(interactive ? { "--panel-glow": DOMAIN_GLOW[domain] } : {}),
  } as CSSProperties;
  return (
    <section
      data-slot="panel"
      data-domain={domain}
      style={style}
      className={cn(
        "flex h-full flex-col overflow-hidden rounded-md border border-line bg-surface shadow-sm",
        interactive &&
          "transition-[transform,box-shadow] duration-[var(--sa-dur-fast)] ease-sa hover:-translate-y-0.5 hover:shadow-[var(--panel-glow)]",
        className,
      )}
    >
      {(title || Icon || tools) && (
        <header
          data-slot="panel-header"
          className={cn("relative flex shrink-0 items-center gap-3 px-4 py-3", danger && "bg-danger-soft")}
        >
          {Icon && (
            <span
              aria-hidden="true"
              className="grid size-8 shrink-0 place-items-center rounded-sm bg-[image:var(--panel-grad)] text-white shadow-sm"
            >
              <Icon className="size-4" />
            </span>
          )}
          {title && <h3 className="text-lg font-semibold text-ink">{title}</h3>}
          {tools && <div className="ml-auto flex shrink-0 items-center gap-2">{tools}</div>}
          <span
            aria-hidden="true"
            data-slot="panel-header-bar"
            className="absolute inset-x-0 bottom-0 h-0.5 bg-[image:var(--panel-grad)]"
          />
        </header>
      )}
      <div className="flex flex-1 flex-col p-4">{children}</div>
      {footer && (
        <footer className="shrink-0 border-t border-line bg-surface-2 px-4 py-2.5 text-sm text-muted">
          {footer}
        </footer>
      )}
    </section>
  );
}
