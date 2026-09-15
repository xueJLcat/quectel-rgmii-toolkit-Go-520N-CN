// 脏状态 sticky 提示条(设计方案 §6.8):"N 项更改未应用" + 应用更改/放弃更改;
// IPv4 端口规则与端口转发两个暂存编辑器 Tab 共用,warning 语义 + 底部悬浮跟随滚动。
import { AnimatePresence, motion } from "motion/react";
import { LoaderCircleIcon, TriangleAlertIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";

export interface DirtyBarProps {
  /** 未应用更改数(0 时隐藏) */
  changes: number;
  applying: boolean;
  onApply: () => void;
  onDiscard: () => void;
}

export function DirtyBar({ changes, applying, onApply, onDiscard }: DirtyBarProps) {
  const { t } = useT("firewall");
  return (
    <AnimatePresence>
      {changes > 0 && (
        <motion.div
          data-slot="dirty-bar"
          role="status"
          initial={{ opacity: 0, y: 16 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 16 }}
          transition={{ duration: 0.22, ease: [0.22, 0.7, 0.28, 1] }}
          className="sticky bottom-4 z-20 mt-4 flex flex-wrap items-center gap-3 rounded-md border border-warning/40 bg-warning-soft px-4 py-3 shadow-lg"
        >
          <TriangleAlertIcon aria-hidden="true" className="size-4 shrink-0 text-warning" />
          <span className="text-sm font-medium text-ink">
            {t("unappliedChanges", { n: changes })}
          </span>
          <div className="ml-auto flex items-center gap-2">
            <Button variant="secondary" size="sm" onClick={onDiscard} disabled={applying}>
              {t("discardChanges")}
            </Button>
            <Button size="sm" onClick={onApply} disabled={applying}>
              {applying && <LoaderCircleIcon className="animate-spin" />}
              {t("applyChanges")}
            </Button>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
