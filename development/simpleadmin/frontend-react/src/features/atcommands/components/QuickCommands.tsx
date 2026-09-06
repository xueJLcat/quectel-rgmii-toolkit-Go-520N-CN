// 快捷命令 chip 网格(设计方案 §6.10:7 预设 ATI/CSQ/QENG/QRSRP/QGMR/CIMI/QMAP LANIP)。
// chip 视觉:描边卡 + tools 域色 hover glow(§5.1 卡片 hover),label 走 i18n,命令 mono 原文。
import { ZapIcon } from "lucide-react";

import { Panel } from "@/components/layout/panel";
import { useT } from "@/lib/i18n";

import { QUICK_COMMANDS } from "../lib";

export interface QuickCommandsProps {
  onRun: (cmd: string) => void;
  disabled: boolean;
}

export function QuickCommands({ onRun, disabled }: QuickCommandsProps) {
  const { t } = useT("atcommands");
  return (
    <Panel domain="tools" icon={ZapIcon} title={t("quickCommands")}>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        {QUICK_COMMANDS.map((item) => (
          <button
            key={item.cmd}
            type="button"
            disabled={disabled}
            onClick={() => onRun(item.cmd)}
            className="flex flex-col items-start gap-0.5 rounded-sm border border-line bg-surface px-3 py-2 text-left shadow-sm transition-[transform,box-shadow] duration-[var(--sa-dur-fast)] ease-sa hover:-translate-y-0.5 hover:border-tools/40 hover:shadow-[0_8px_22px_-6px_var(--sa-domain-tools)] focus-visible:ring-[3px] focus-visible:ring-accent/40 disabled:pointer-events-none disabled:opacity-50 active:scale-[0.98]"
          >
            <span className="text-xs text-muted">{t(item.labelKey)}</span>
            <span className="font-mono text-sm text-ink">{item.cmd}</span>
          </button>
        ))}
      </div>
    </Panel>
  );
}
