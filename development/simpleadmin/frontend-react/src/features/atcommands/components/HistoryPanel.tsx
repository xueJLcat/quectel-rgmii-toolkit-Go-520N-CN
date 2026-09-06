// 命令历史侧卡(设计方案 §6.10):localStorage "simpleadmin.atHistory" 兼容旧格式,
// ≤50 去重;点击复用(回填输入行),单条删除,整表清除。
import { HistoryIcon, XIcon } from "lucide-react";

import { EmptyState } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";

export interface HistoryPanelProps {
  history: string[];
  onReuse: (cmd: string) => void;
  onRemove: (cmd: string) => void;
  onClear: () => void;
}

export function HistoryPanel({ history, onReuse, onRemove, onClear }: HistoryPanelProps) {
  const { t } = useT("atcommands");
  return (
    <Panel
      domain="tools"
      icon={HistoryIcon}
      title={t("commandHistory")}
      tools={
        history.length > 0 ? (
          <Button variant="ghost" size="sm" onClick={onClear}>
            {t("clearHistory")}
          </Button>
        ) : undefined
      }
    >
      {history.length === 0 ? (
        <EmptyState title={t("noHistoryYet")} className="px-4 py-6" />
      ) : (
        <ul className="flex max-h-64 flex-col gap-1 overflow-y-auto">
          {history.map((cmd) => (
            <li key={cmd} className="flex items-center gap-1">
              <button
                type="button"
                onClick={() => onReuse(cmd)}
                title={t("reuseCommandHint")}
                className="min-w-0 flex-1 truncate rounded-sm px-2 py-1.5 text-left font-mono text-sm text-ink transition-colors hover:bg-tools-soft focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent/40"
              >
                {cmd}
              </button>
              <Button
                variant="ghost"
                size="icon"
                aria-label={`${t("deleteCommandAria")} ${cmd}`}
                className="shrink-0 text-muted hover:text-danger"
                onClick={() => onRemove(cmd)}
              >
                <XIcon />
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
