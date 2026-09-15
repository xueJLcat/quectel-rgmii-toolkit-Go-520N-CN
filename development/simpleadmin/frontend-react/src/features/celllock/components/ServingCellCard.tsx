// 一键锁定当前服务小区(设计方案 §6.5 ⑤):SCS 下拉(auto=后端读服务小区并守护探测
// 30/15)+ primary 大按钮 lock_serving_cell;守护锁定流程文案对齐旧版:确认"将锁定当前
// 驻留小区，期间可能短暂断网",失败默认提示"SCS 探测失败，锁定已自动解除…",
// 成功 toast 展示 locked / pci / freq / band 拼接详情。
import { CrosshairIcon, LoaderCircleIcon } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import { useLockServingCell } from "../hooks";
import { SCS_OPTIONS } from "../lib";

export function ServingCellCard() {
  const { t } = useT("celllock");
  const confirm = useConfirmStore((state) => state.confirm);
  const lockServing = useLockServingCell();
  const [scs, setScs] = useState("auto");

  async function handleLock(): Promise<void> {
    const ok = await confirm({
      title: t("lockServingCell"),
      message: t("lockTheCurrentServingCellTheNetworkMayDropBrieflyContinue"),
      danger: true,
    });
    if (!ok) return;
    lockServing.mutate(scs);
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted">
        {t("lockTheCurrentServingCellTheNetworkMayDropBrieflyContinue")}
      </p>
      <div className="flex flex-wrap items-center gap-3">
        <Select value={scs} onValueChange={setScs} disabled={lockServing.isPending}>
          <SelectTrigger className="w-40" aria-label="SCS">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SCS_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {t(option.labelKey)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          type="button"
          size="lg"
          className="flex-1 sm:flex-none sm:px-10"
          disabled={lockServing.isPending}
          onClick={() => void handleLock()}
        >
          {lockServing.isPending ? (
            <LoaderCircleIcon className="animate-spin" />
          ) : (
            <CrosshairIcon />
          )}
          {t("lockServingCell")}
        </Button>
      </div>
    </div>
  );
}
