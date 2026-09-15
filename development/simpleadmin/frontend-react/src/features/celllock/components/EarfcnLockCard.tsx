// 频点锁定卡(设计方案 §6.5 ④):NR5G(≤32)/LTE(≤2)逗号分隔输入 + 当前锁定 chips +
// set_earfcn_lock/clear_earfcn_lock;清除 = 危险确认;写入失败时后端 error 文案
// ("该固件可能不支持此频点锁")经 toast 透出;成功后立即回读频点锁状态(对齐旧版)。
import { LoaderCircleIcon, LockIcon, UnlockIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { ErrorRetry, StatusChip } from "@/components/common";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import { isActionFailure, useClearEarfcnLock, useSetEarfcnLock } from "../hooks";
import { EARFCN_LIMITS, parseEarfcnInput } from "../lib";
import type { EarfcnRat } from "../lib";

export interface EarfcnLockCardProps {
  nr5gArfcns: string[];
  lteArfcns: string[];
  pending: boolean;
  failed: boolean;
  onRetry: () => void;
  retrying: boolean;
}

interface RatRowProps {
  rat: EarfcnRat;
  arfcns: string[];
  value: string;
  onChange: (value: string) => void;
  onLock: () => void;
  onClear: () => void;
  busy: boolean;
  locking: boolean;
}

function RatRow({ rat, arfcns, value, onChange, onLock, onClear, busy, locking }: RatRowProps) {
  const { t } = useT("celllock");
  return (
    <div className="flex flex-col gap-2" data-slot={`earfcn-row-${rat}`}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="w-14 text-sm font-semibold text-ink">
          {rat === "NR5G" ? "NR5G" : "LTE"}
        </span>
        <StatusChip tone={arfcns.length > 0 ? "info" : "muted"}>
          {arfcns.length > 0 ? t("common:enabled") : t("common:notEnabled")}
        </StatusChip>
        {arfcns.map((arfcn) => (
          <Badge key={arfcn} variant="outline">
            {arfcn}
          </Badge>
        ))}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Input
          type="text"
          inputMode="numeric"
          autoComplete="off"
          className="min-w-52 flex-1"
          aria-label={
            rat === "NR5G" ? t("nr5gEarfcnsCommaSeparatedMax32") : t("lteEarfcnsCommaSeparatedMax2")
          }
          placeholder={
            rat === "NR5G" ? t("nr5gEarfcnsCommaSeparatedMax32") : t("lteEarfcnsCommaSeparatedMax2")
          }
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
        <Button type="button" size="sm" disabled={busy} onClick={onLock}>
          {locking ? <LoaderCircleIcon className="animate-spin" /> : <LockIcon />}
          {t("lockEarfcns")}
        </Button>
        <Button type="button" size="sm" variant="danger" disabled={busy} onClick={onClear}>
          <UnlockIcon />
          {t("unlockEarfcns")}
        </Button>
      </div>
    </div>
  );
}

export function EarfcnLockCard({
  nr5gArfcns,
  lteArfcns,
  pending,
  failed,
  onRetry,
  retrying,
}: EarfcnLockCardProps) {
  const { t } = useT("celllock");
  const confirm = useConfirmStore((state) => state.confirm);
  const setLock = useSetEarfcnLock();
  const clearLock = useClearEarfcnLock();

  const [inputs, setInputs] = useState<Record<EarfcnRat, string>>({ NR5G: "", LTE: "" });
  const busy = setLock.isPending || clearLock.isPending;

  async function handleSet(rat: EarfcnRat): Promise<void> {
    const parsed = parseEarfcnInput(inputs[rat], rat);
    if (parsed.status === "empty") {
      toast.error(t("pleaseEnterEarfcns"));
      return;
    }
    if (parsed.status === "non-digit") {
      toast.error(t("parametersMustBeDigitsOnly"));
      return;
    }
    if (parsed.status === "limit") {
      toast.error(t("earfcnCountExceedsTheLimit"));
      return;
    }
    const ok = await confirm({
      title: t("lockEarfcns"),
      message:
        rat === "NR5G"
          ? t("thisWillSetNr5gEarfcnLockContinue")
          : t("thisWillSetLteEarfcnLockContinue"),
      danger: true,
    });
    if (!ok) return;
    setLock.mutate(
      { rat, arfcns: parsed.arfcns },
      {
        onSuccess: (result) => {
          if (!isActionFailure(result)) setInputs((current) => ({ ...current, [rat]: "" }));
        },
      },
    );
  }

  async function handleClear(rat: EarfcnRat): Promise<void> {
    const ok = await confirm({
      title: t("unlockEarfcns"),
      message:
        rat === "NR5G"
          ? t("thisWillRemoveTheNr5gEarfcnLockContinue")
          : t("thisWillRemoveTheLteEarfcnLockContinue"),
      danger: true,
    });
    if (!ok) return;
    clearLock.mutate(rat);
  }

  if (failed) {
    return (
      <ErrorRetry message={t("common:failedToLoadStatus")} onRetry={onRetry} retrying={retrying} />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {pending && (
        <StatusChip tone="muted" pulse>
          {t("common:moduleWarmingUp")}
        </StatusChip>
      )}
      <RatRow
        rat="NR5G"
        arfcns={nr5gArfcns}
        value={inputs.NR5G}
        onChange={(value) => setInputs((current) => ({ ...current, NR5G: value }))}
        onLock={() => void handleSet("NR5G")}
        onClear={() => void handleClear("NR5G")}
        busy={busy}
        locking={setLock.isPending && setLock.variables?.rat === "NR5G"}
      />
      <RatRow
        rat="LTE"
        arfcns={lteArfcns}
        value={inputs.LTE}
        onChange={(value) => setInputs((current) => ({ ...current, LTE: value }))}
        onLock={() => void handleSet("LTE")}
        onClear={() => void handleClear("LTE")}
        busy={busy}
        locking={setLock.isPending && setLock.variables?.rat === "LTE"}
      />
      <p className="text-xs text-muted">
        NR5G ≤ {EARFCN_LIMITS.NR5G} · LTE ≤ {EARFCN_LIMITS.LTE}
      </p>
    </div>
  );
}
