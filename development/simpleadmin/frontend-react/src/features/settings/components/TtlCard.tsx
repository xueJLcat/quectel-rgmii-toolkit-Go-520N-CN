// TTL 卡:状态徽章(启用值/禁用)+ 0-255 严格校验输入(对齐旧 setTTL:/^\d{1,3}$/ 整体拒绝,
// 防 parseInt 把 "0x10" 静默解析成 0=禁用)+ setTTL;ok:false 如实提示,成败都回读。
// 0=禁用说明:改写 iptables TTL/HL 用于防止运营商热点检测。
import { GaugeIcon, LoaderCircleIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";

import { useSetTtl, useTtlStatus } from "../hooks";
import { validateTtlInput } from "../lib";

export function TtlCard() {
  const { t } = useT("settings");
  const query = useTtlStatus();
  const setTtl = useSetTtl();
  const [value, setValue] = useState("");
  const data = query.data;

  function handleSave(): void {
    const parsed = validateTtlInput(value);
    if (parsed === null) {
      toast.error(t("ttlMustBeAnIntegerBetween0And255"));
      return;
    }
    setTtl.mutate(parsed, {
      onSuccess: () => {
        toast.success(t("common:saved"));
        setValue("");
      },
      onError: () => {
        toast.error(t("common:dataUpdateFailed"));
      },
    });
  }

  return (
    <Panel domain="system" icon={GaugeIcon} title={t("ttlSettings")} footer={t("ttlHotspotNote")}>
      {query.isPending ? (
        <div className="flex flex-col gap-3" aria-busy="true">
          <Skeleton className="h-6 w-40" />
          <Skeleton className="h-9 w-full" />
        </div>
      ) : query.isError ? (
        <ErrorRetry
          message={t("failedToLoadTtlStatus")}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      ) : (
        // 卡片拉伸时状态/输入/说明三组均匀分布(与同行账户安全卡等高)
        <div className="flex flex-1 flex-col justify-between gap-4">
          <div data-slot="ttl-status" className="flex flex-wrap items-center gap-2">
            {data?.isEnabled ? (
              <StatusChip tone="success">{t("ttlEnabled")}</StatusChip>
            ) : (
              <StatusChip tone="danger">{t("ttlDisabled")}</StatusChip>
            )}
            <StatusChip tone="info">{`${t("ttlValue")}: ${data?.ttl ?? 0}`}</StatusChip>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Input
              className="w-32"
              type="text"
              inputMode="numeric"
              aria-label={t("ttlValue")}
              placeholder={t("ttlValue")}
              value={value}
              disabled={setTtl.isPending}
              onChange={(event) => setValue(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  handleSave();
                }
              }}
            />
            <Button onClick={handleSave} disabled={setTtl.isPending}>
              {setTtl.isPending && <LoaderCircleIcon className="animate-spin" />}
              {t("update")}
            </Button>
          </div>
          <p className="text-sm text-muted">{t("setTtlValueTo0ToDisable")}</p>
        </div>
      )}
    </Panel>
  );
}
