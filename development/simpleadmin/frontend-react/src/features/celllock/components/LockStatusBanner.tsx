// 锁定状态横幅(设计方案 §6.5 ①):settings.cellLockStatus(未锁/已锁4G/已锁5G/双锁)
// 与 earfcn_lock_status(NR5G/LTE 频点锁)合成一行渐变 StatusChip;
// 已锁定 = 域色渐变填充白字,未锁 = muted;pending = pulse(保留旧值,§5.2)。
import { StatusChip } from "@/components/common";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

import { CELL_LOCK_STATUS_KEY_NS, cellLockStatusKey } from "../lib";

export interface LockStatusBannerProps {
  /** settings.cellLockStatus 原始值(后端中文枚举);undefined=未加载 */
  cellLockStatus?: string;
  settingsPending: boolean;
  nr5gArfcns: string[];
  lteArfcns: string[];
  earfcnPending: boolean;
  className?: string;
}

const GRADIENT_CHIP_CLASS =
  "border-transparent bg-[image:var(--sa-grad-network)] text-white [&_span]:text-white";

export function LockStatusBanner({
  cellLockStatus,
  settingsPending,
  nr5gArfcns,
  lteArfcns,
  earfcnPending,
  className,
}: LockStatusBannerProps) {
  const { t } = useT("celllock");
  const statusKey = cellLockStatusKey(cellLockStatus);
  const statusLabel =
    statusKey === "unknown"
      ? (cellLockStatus ?? t("lockStatusUnknown"))
      : t(CELL_LOCK_STATUS_KEY_NS[statusKey]);
  const locked = statusKey !== "unlocked" && statusKey !== "unknown";

  return (
    <div
      data-slot="lock-status-banner"
      className={cn(
        "flex flex-wrap items-center gap-2 rounded-md border border-line bg-surface px-4 py-3 shadow-sm",
        className,
      )}
    >
      <span className="text-sm font-medium text-muted">{t("lockStatus")}</span>
      <StatusChip
        tone={locked ? "accent" : "muted"}
        pulse={settingsPending}
        className={locked ? GRADIENT_CHIP_CLASS : undefined}
      >
        {statusLabel}
      </StatusChip>
      <StatusChip tone={nr5gArfcns.length > 0 ? "info" : "muted"} pulse={earfcnPending}>
        NR5G{" "}
        {nr5gArfcns.length > 0
          ? `${t("common:enabled")}: ${nr5gArfcns.join(",")}`
          : t("common:notEnabled")}
      </StatusChip>
      <StatusChip tone={lteArfcns.length > 0 ? "info" : "muted"} pulse={earfcnPending}>
        LTE{" "}
        {lteArfcns.length > 0
          ? `${t("common:enabled")}: ${lteArfcns.join(",")}`
          : t("common:notEnabled")}
      </StatusChip>
    </div>
  );
}
