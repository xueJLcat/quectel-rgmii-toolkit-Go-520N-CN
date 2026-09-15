// 重启倒计时全屏遮罩(全站单一实现,设计方案 §5.1):z-50 全屏 bg-[var(--sa-overlay)] + backdrop-blur,
// 中央卡片 = CountdownGauge(charts 复用,seconds=剩余/total)+ 文案(默认含"请勿关闭页面")。
// 剩余秒数按 store.endsAt 真实时间戳 Math.ceil 计算(interval 1s tick 驱动,语义对齐 Vue 版
// simpleadmin-reboot.js)——后台标签页被节流,回来时按当前时间重算,不漂移。
// 到 0 转完成态:成功 chip + 关闭按钮;motion fade 进出。
import { AnimatePresence, motion } from "motion/react";
import type { Transition } from "motion/react";
import { useEffect, useState } from "react";

import { CountdownGauge } from "@/components/charts";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { useRebootStore } from "@/stores/reboot";
import { StatusChip } from "./status-chip";

const FADE: Transition = { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] };

function remainingSeconds(endsAt: number): number {
  return Math.max(0, Math.ceil((endsAt - Date.now()) / 1000));
}

export interface RebootOverlayProps {
  className?: string;
}

export function RebootOverlay({ className }: RebootOverlayProps) {
  const active = useRebootStore((state) => state.countdownActive);
  const endsAt = useRebootStore((state) => state.endsAt);
  const total = useRebootStore((state) => state.total);
  const label = useRebootStore((state) => state.label);
  const closeCountdown = useRebootStore((state) => state.closeCountdown);
  const { t } = useT();
  const [remaining, setRemaining] = useState(() => remainingSeconds(endsAt));

  // tick 由组件内 interval 驱动(而非 store):遮罩卸载即停,store 只存真实时间戳基准。
  useEffect(() => {
    if (!active) return;
    const tick = () => setRemaining(remainingSeconds(endsAt));
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [active, endsAt]);

  const message = label ?? t("settings:rebootingPleaseWaitDoNotCloseThisPage");

  return (
    <AnimatePresence>
      {active && (
        <motion.div
          key="reboot-overlay"
          role="alertdialog"
          aria-modal="true"
          aria-label={message}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={FADE}
          className={cn(
            "fixed inset-0 z-50 flex items-center justify-center bg-[var(--sa-overlay)] backdrop-blur",
            className,
          )}
        >
          <div className="mx-4 flex w-full max-w-sm flex-col items-center gap-4 rounded-lg border border-line bg-surface p-6 text-center shadow-xl">
            <CountdownGauge seconds={remaining} total={total} className="size-40" />
            {/* 秒数在 canvas 中心渲染;此 live region 供读屏播报,亦是 jsdom 降级下的文本锚点 */}
            <span data-slot="reboot-remaining" aria-live="polite" className="sr-only">
              {remaining}
            </span>
            {remaining > 0 ? (
              <p className="text-sm font-medium text-soft">{message}</p>
            ) : (
              <>
                <StatusChip tone="success">{t("settings:rebootCountdownFinished")}</StatusChip>
                <Button variant="secondary" size="sm" onClick={closeCountdown}>
                  {t("close")}
                </Button>
              </>
            )}
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
