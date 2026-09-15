// 手动锁定卡(设计方案 §6.5 ③):锁定模式下拉(占位=当前 cellLockStatus);
// LTE = 小区数量(1-10)+ EARFCN/PCI 动态行组可增删(提交校验数字/完整组数),
// lock_lte_manual pairs 格式 "earfcn,pci;earfcn,pci"(对齐旧版);
// NR5G = EARFCN/PCI + SCS 分段控件(自动|15|30|60|120|240,auto=不提交由后端按频段
// 推断+守护探测)+ 频段,lock_nr_manual;失败自动解锁提示由后端 error 文案携带。
// 所有锁定/解锁操作经 confirm();成功后复位表单(对齐旧版 resetManualLockForm)。
import { LoaderCircleIcon, LockIcon, PlusIcon, Trash2Icon, UnlockIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import { isActionFailure, useLockLteManual, useLockNrManual, useUnlockCell } from "../hooks";
import {
  MAX_LTE_CELLS,
  SCS_OPTIONS,
  buildLtePairsParam,
  collectLtePairs,
  normalizeLteCellCount,
} from "../lib";
import type { LtePair } from "../lib";

export interface ManualLockCardProps {
  /** settings.cellLockStatus 原始值(下拉占位"当前：X") */
  cellLockStatus: string;
}

const emptyRow = (): LtePair => ({ earfcn: "", pci: "" });

export function ManualLockCard({ cellLockStatus }: ManualLockCardProps) {
  const { t } = useT("celllock");
  const confirm = useConfirmStore((state) => state.confirm);
  const lteLock = useLockLteManual();
  const nrLock = useLockNrManual();
  const unlock = useUnlockCell();

  const [mode, setMode] = useState<string>("");
  const [rows, setRows] = useState<LtePair[]>([]);
  const [nrEarfcn, setNrEarfcn] = useState("");
  const [nrPci, setNrPci] = useState("");
  const [nrScs, setNrScs] = useState("auto");
  const [nrBand, setNrBand] = useState("");

  const busy = lteLock.isPending || nrLock.isPending || unlock.isPending;

  function resetForm(): void {
    setMode("");
    setRows([]);
    setNrEarfcn("");
    setNrPci("");
    setNrScs("auto");
    setNrBand("");
  }

  function resizeRows(count: number): void {
    setRows((current) => {
      const next = current.slice(0, count);
      while (next.length < count) next.push(emptyRow());
      return next;
    });
  }

  async function handleLockLte(): Promise<void> {
    const count = rows.length;
    if (count < 1) {
      toast.error(t("enterTheNumberOfCellsToLock"));
      return;
    }
    const collected = collectLtePairs(rows);
    if (collected.status === "non-digit") {
      toast.error(t("parametersMustBeDigitsOnly"));
      return;
    }
    if (collected.status === "incomplete") {
      toast.error(t("common:earfcnGroupsIncomplete", { groups: count }));
      return;
    }
    const ok = await confirm({
      title: t("lockLteCell"),
      message: t("thisWillLockLteCellsContinue"),
      danger: true,
    });
    if (!ok) return;
    lteLock.mutate(
      { cellNum: count, pairs: buildLtePairsParam(collected.pairs) },
      {
        onSuccess: (result) => {
          if (!isActionFailure(result)) resetForm();
        },
      },
    );
  }

  async function handleLockNr(): Promise<void> {
    const earfcn = nrEarfcn.trim();
    const pci = nrPci.trim();
    const band = nrBand.trim();
    if (!earfcn || !pci || !band) {
      toast.error(t("pleaseFillInAllRequiredFields"));
      return;
    }
    if (![earfcn, pci, band].every((value) => /^\d+$/.test(value))) {
      toast.error(t("parametersMustBeDigitsOnly"));
      return;
    }
    const ok = await confirm({
      title: t("lockCell"),
      message: t("nr5gCellLockProbesScsTheNetworkMayDropBrieflyUpTo25sContinue"),
      danger: true,
    });
    if (!ok) return;
    nrLock.mutate(
      { pci, earfcn, scs: nrScs, band },
      {
        onSuccess: (result) => {
          if (!isActionFailure(result)) resetForm();
        },
      },
    );
  }

  async function handleUnlock(action: "unlock_lte" | "unlock_nr"): Promise<void> {
    const isLte = action === "unlock_lte";
    const ok = await confirm({
      title: isLte ? t("unlockLteCell") : t("unlockNr5gSaCell"),
      message: isLte ? t("thisWillUnlockLteCellsContinue") : t("thisWillUnlockNr5gSaCellsContinue"),
      danger: true,
    });
    if (!ok) return;
    unlock.mutate(action, {
      onSuccess: (result) => {
        if (!isActionFailure(result)) resetForm();
      },
    });
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2 sm:max-w-64">
        <Label htmlFor="celllock-mode">{t("lockMode")}</Label>
        <Select
          value={mode || undefined}
          onValueChange={(value) => {
            setMode(value);
            // 对齐旧版 onNetworkModeCellChange:切到 LTE 时清空小区数量与行组
            if (value === "LTE") setRows([]);
          }}
          disabled={busy}
        >
          <SelectTrigger id="celllock-mode" className="w-full">
            <SelectValue placeholder={t("common:currentValue", { value: cellLockStatus })} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="LTE">LTE</SelectItem>
            <SelectItem value="NR5G-SA">NR5G-SA</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {mode === "LTE" && (
        <div className="flex flex-col gap-3" data-slot="lte-manual-lock">
          <div className="flex flex-col gap-2 sm:max-w-48">
            <Label htmlFor="celllock-lte-count">{t("numberOfCells")}</Label>
            <Input
              id="celllock-lte-count"
              type="number"
              min={1}
              max={MAX_LTE_CELLS}
              placeholder="1-10"
              value={rows.length === 0 ? "" : String(rows.length)}
              onChange={(event) => resizeRows(normalizeLteCellCount(event.target.value))}
            />
          </div>
          {rows.map((row, index) => (
            <div key={index} className="flex items-center gap-2">
              <Input
                type="text"
                inputMode="numeric"
                autoComplete="off"
                className="max-w-40"
                aria-label={`EARFCN ${index + 1}`}
                placeholder={`EARFCN ${index + 1}`}
                value={row.earfcn}
                onChange={(event) =>
                  setRows((current) =>
                    current.map((item, i) =>
                      i === index ? { ...item, earfcn: event.target.value } : item,
                    ),
                  )
                }
              />
              <Input
                type="text"
                inputMode="numeric"
                autoComplete="off"
                className="max-w-40"
                aria-label={`PCI ${index + 1}`}
                placeholder={`PCI ${index + 1}`}
                value={row.pci}
                onChange={(event) =>
                  setRows((current) =>
                    current.map((item, i) =>
                      i === index ? { ...item, pci: event.target.value } : item,
                    ),
                  )
                }
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                disabled={busy}
                aria-label={`${t("removePair")} ${index + 1}`}
                onClick={() => setRows((current) => current.filter((_, i) => i !== index))}
              >
                <Trash2Icon className="text-danger" />
              </Button>
            </div>
          ))}
          <div>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              disabled={busy || rows.length >= MAX_LTE_CELLS}
              onClick={() => setRows((current) => [...current, emptyRow()])}
            >
              <PlusIcon />
              {t("common:add")}
            </Button>
          </div>
        </div>
      )}

      {mode === "NR5G-SA" && (
        <div className="flex flex-col gap-3" data-slot="nr-manual-lock">
          <div className="flex flex-wrap items-center gap-2">
            <Input
              type="text"
              inputMode="numeric"
              autoComplete="off"
              className="max-w-40"
              aria-label="EARFCN"
              placeholder="EARFCN"
              value={nrEarfcn}
              onChange={(event) => setNrEarfcn(event.target.value)}
            />
            <Input
              type="text"
              inputMode="numeric"
              autoComplete="off"
              className="max-w-40"
              aria-label="PCI"
              placeholder="PCI"
              value={nrPci}
              onChange={(event) => setNrPci(event.target.value)}
            />
            <Input
              type="text"
              inputMode="numeric"
              autoComplete="off"
              className="max-w-28"
              aria-label={t("colBand")}
              placeholder="Band"
              value={nrBand}
              onChange={(event) => setNrBand(event.target.value)}
            />
          </div>
          <Tabs value={nrScs} onValueChange={setNrScs}>
            <TabsList aria-label="SCS">
              {SCS_OPTIONS.map((option) => (
                <TabsTrigger key={option.value} value={option.value}>
                  {t(option.labelKey)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2 border-t border-line pt-3">
        {mode === "LTE" && (
          <Button type="button" disabled={busy} onClick={() => void handleLockLte()}>
            {lteLock.isPending ? <LoaderCircleIcon className="animate-spin" /> : <LockIcon />}
            {t("lockLteCell")}
          </Button>
        )}
        {mode === "NR5G-SA" && (
          <Button type="button" disabled={busy} onClick={() => void handleLockNr()}>
            {nrLock.isPending ? <LoaderCircleIcon className="animate-spin" /> : <LockIcon />}
            {t("lockNr5gSaCell")}
          </Button>
        )}
        <Button
          type="button"
          variant="danger"
          disabled={busy}
          onClick={() => void handleUnlock("unlock_lte")}
        >
          <UnlockIcon />
          {t("unlockLteCell")}
        </Button>
        <Button
          type="button"
          variant="danger"
          disabled={busy}
          onClick={() => void handleUnlock("unlock_nr")}
        >
          <UnlockIcon />
          {t("unlockNr5gSaCell")}
        </Button>
      </div>
    </div>
  );
}
