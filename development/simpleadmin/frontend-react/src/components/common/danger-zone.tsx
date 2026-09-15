// 危险操作区(设计方案 §3.3 danger 面板:头部 danger-soft 底):rose 边框卡,
// 用于重启模块/设备、关机、改 IMEI、删除配置、恢复出厂等破坏性操作分组;
// 内部操作按钮由调用方以 children 传入(破坏性操作仍需经 stores/confirm 二次确认)。
import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

export interface DangerZoneProps {
  title: string;
  description?: string;
  children: ReactNode;
  className?: string;
}

export function DangerZone({ title, description, children, className }: DangerZoneProps) {
  return (
    <section
      className={cn("overflow-hidden rounded-md border border-danger/40 bg-surface", className)}
    >
      <header className="border-b border-danger/30 bg-danger-soft px-6 py-4">
        <h3 className="text-base font-semibold text-danger">{title}</h3>
        {description && <p className="mt-1 text-sm text-muted">{description}</p>}
      </header>
      <div className="p-6">{children}</div>
    </section>
  );
}
