// 频段矩阵(设计方案 §6.4 ①):LTE/NSA/SA 三列卡,频段 chip 网格。
// 选中 = 域色渐变填充白字(var(--sa-grad-network)),未选 = 描边;当前服务端已锁频段
// 带角标圆点(data-locked)以区别于用户未提交的勾选;每列 锁定 + 全选/取消选中
// (对齐旧版 toggleBandCheckboxes:全选中→取消,否则→全选)。
import { LoaderCircleIcon, LockIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useT } from "@/lib/i18n";

import type { BandGroup, BandMode } from "../lib/bandMap";

export interface BandMatrixProps {
  groups: BandGroup[];
  /** 各模式当前生效勾选(用户选择 ?? 服务端已锁) */
  checkedByMode: Record<BandMode, string[]>;
  /** 各模式服务端已锁频段(高亮角标) */
  lockedByMode: Record<BandMode, string[]>;
  /** 已锁频段尚未读取(pending/加载中):chip 禁用 + 查询提示 */
  bandsLoading: boolean;
  lockingMode: BandMode | null;
  onToggleBand: (mode: BandMode, name: string) => void;
  onToggleAll: (mode: BandMode) => void;
  onLock: (mode: BandMode) => void;
}

export function BandMatrix({
  groups,
  checkedByMode,
  lockedByMode,
  bandsLoading,
  lockingMode,
  onToggleBand,
  onToggleAll,
  onLock,
}: BandMatrixProps) {
  const { t } = useT("network");
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
      {groups.map((group) => {
        const checked = checkedByMode[group.mode];
        const locked = lockedByMode[group.mode];
        const allChecked =
          group.bands.length > 0 && group.bands.every((b) => checked.includes(b.name));
        return (
          <div
            key={group.mode}
            data-slot="band-mode-panel"
            data-mode={group.mode}
            className="flex flex-col gap-3 rounded-md border border-line bg-surface-2/50 p-3"
          >
            <div className="flex items-center justify-between gap-2">
              <h4 className="text-sm font-semibold text-ink">{group.title}</h4>
              <span className="text-xs tabular-nums text-muted">
                {checked.length}/{group.bands.length}
              </span>
            </div>
            {group.bands.length === 0 ? (
              <p className="py-4 text-center text-sm text-muted">{t("noBandsAvailable")}</p>
            ) : (
              <div className="flex flex-wrap gap-1.5" role="group" aria-label={group.title}>
                {group.bands.map((band) => {
                  const selected = checked.includes(band.name);
                  const isLocked = locked.includes(band.name);
                  return (
                    <button
                      key={band.name}
                      type="button"
                      role="checkbox"
                      aria-checked={selected}
                      aria-label={`${group.prefix}${band.name}`}
                      title={isLocked ? t("currentlyLocked") : undefined}
                      disabled={bandsLoading}
                      data-slot="band-chip"
                      data-locked={isLocked || undefined}
                      onClick={() => onToggleBand(group.mode, band.name)}
                      className={cn(
                        "relative inline-flex h-8 min-w-11 items-center justify-center rounded-sm border px-2 text-sm font-medium transition-all duration-[var(--sa-dur-fast)] ease-sa focus-visible:ring-[3px] focus-visible:ring-accent/40 disabled:pointer-events-none disabled:opacity-50",
                        selected
                          ? "border-transparent bg-[image:var(--sa-grad-network)] text-white shadow-sm"
                          : "border-line-strong bg-surface text-ink hover:bg-surface-2",
                      )}
                    >
                      {group.prefix}
                      {band.name}
                      {isLocked && (
                        <span
                          aria-hidden="true"
                          data-slot="band-chip-locked-dot"
                          className="absolute right-0.5 top-0.5 size-1.5 rounded-full bg-current opacity-70"
                        />
                      )}
                    </button>
                  );
                })}
              </div>
            )}
            <div className="mt-auto flex items-center gap-2">
              <Button
                size="sm"
                disabled={bandsLoading || lockingMode !== null}
                onClick={() => onLock(group.mode)}
              >
                {lockingMode === group.mode ? (
                  <LoaderCircleIcon className="animate-spin" />
                ) : (
                  <LockIcon />
                )}
                {t("common:lock")}
              </Button>
              <Button
                size="sm"
                variant="secondary"
                disabled={bandsLoading || group.bands.length === 0}
                onClick={() => onToggleAll(group.mode)}
              >
                {allChecked ? t("deselect") : t("selectAll")}
              </Button>
            </div>
          </div>
        );
      })}
    </div>
  );
}
