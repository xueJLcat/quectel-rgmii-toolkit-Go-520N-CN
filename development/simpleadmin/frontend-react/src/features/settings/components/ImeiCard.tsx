// IMEI 卡:当前 IMEI(mono + CopyButton)+ 15 位校验写入 set_imei。
// 重启语义对齐旧 updateIMEI:confirm → 乐观倒计时 40s → 写入;仅 ok:false(模块明确拒绝)
// 或传输失败(命令未送达)才取消倒计时;后端把"超时无应答"视为重启进行中(atResponseRebootOK),
// 因此提示文案明示超时不算失败;倒计时结束后自动回读(对齐旧 onDone → init)。
import { FingerprintIcon, LoaderCircleIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";

import { CopyButton, ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";
import { useRebootStore } from "@/stores/reboot";

import { SYSTEM_STATUS_KEY, useSystemAction, useSystemStatus } from "../hooks";
import { isDisplayableImei, isValidImeiInput } from "../lib";

/** 旧 updateIMEI:Reboot.countdown(40) */
const IMEI_REBOOT_SECONDS = 40;

export function ImeiCard() {
  const { t } = useT("settings");
  const confirm = useConfirmStore((state) => state.confirm);
  const startCountdown = useRebootStore((state) => state.startCountdown);
  const closeCountdown = useRebootStore((state) => state.closeCountdown);
  const statusQuery = useSystemStatus();
  const systemAction = useSystemAction();
  const queryClient = useQueryClient();
  const [newImei, setNewImei] = useState("");
  const refreshTimerRef = useRef<number | null>(null);

  const rawImei = String(statusQuery.data?.imei ?? "").trim();
  const currentImei = isDisplayableImei(rawImei) ? rawImei : "-";

  // 对齐旧 fetchSystemStatus:输入框为空时预填当前 IMEI
  useEffect(() => {
    if (isDisplayableImei(rawImei)) {
      setNewImei((current) => (current === "" ? rawImei : current));
    }
  }, [rawImei]);

  useEffect(
    () => () => {
      if (refreshTimerRef.current !== null) window.clearTimeout(refreshTimerRef.current);
    },
    [],
  );

  function scheduleRefresh(): void {
    if (refreshTimerRef.current !== null) window.clearTimeout(refreshTimerRef.current);
    refreshTimerRef.current = window.setTimeout(
      () => {
        refreshTimerRef.current = null;
        void queryClient.invalidateQueries({ queryKey: SYSTEM_STATUS_KEY });
      },
      (IMEI_REBOOT_SECONDS + 5) * 1000,
    );
  }

  function cancelRefresh(): void {
    if (refreshTimerRef.current !== null) {
      window.clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    }
  }

  async function handleUpdate(): Promise<void> {
    const value = newImei.trim();
    if (!value) {
      toast.error(t("noNewImeiWasProvided"));
      return;
    }
    if (!isValidImeiInput(value)) {
      toast.error(t("invalidImei"));
      return;
    }
    if (currentImei !== "-" && value === currentImei) {
      toast.error(t("theImeiIsTheSameAsTheCurrentImei"));
      return;
    }
    const ok = await confirm({
      title: t("thisWillChangeTheImeiAndRebootTheModem"),
      message: t("common:continue"),
      confirmText: t("confirmAndReboot"),
      danger: true,
    });
    if (!ok) return;
    startCountdown({ seconds: IMEI_REBOOT_SECONDS });
    scheduleRefresh();
    try {
      const data = await systemAction.mutateAsync({ action: "set_imei", params: { imei: value } });
      if (data?.ok === false) {
        closeCountdown();
        cancelRefresh();
        toast.error(String(data.error ?? "") || t("common:operationFailed"));
      }
    } catch {
      // 传输层失败 = 命令未送达,必须取消乐观启动的倒计时(对齐旧 updateIMEI catch)
      closeCountdown();
      cancelRefresh();
      toast.error(t("common:operationFailed"));
    }
  }

  return (
    <Panel
      domain="system"
      icon={FingerprintIcon}
      title={t("imeiSettings")}
      footer={t("imeiTimeoutNote")}
    >
      {statusQuery.isPending && !statusQuery.data ? (
        <div className="flex flex-col gap-3" aria-busy="true">
          <StatusChip tone="info" pulse>
            {t("gettingImei")}
          </StatusChip>
          <Skeleton className="h-9 w-full" />
        </div>
      ) : statusQuery.isError && !statusQuery.data ? (
        <ErrorRetry
          message={t("failedToLoadSystemStatus")}
          onRetry={() => void statusQuery.refetch()}
          retrying={statusQuery.isFetching}
        />
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm text-muted">{t("currentImei")}</span>
            <span data-slot="current-imei" className="font-mono text-base text-ink">
              {currentImei}
            </span>
            {currentImei !== "-" && <CopyButton text={currentImei} />}
            {statusQuery.isFetching && (
              <StatusChip tone="info" pulse>
                {t("common:fetching")}
              </StatusChip>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex min-w-56 flex-1 flex-col gap-2">
              <Label htmlFor="imei-input">{t("enterNewImei")}</Label>
              <Input
                id="imei-input"
                type="text"
                inputMode="numeric"
                aria-label={t("enterNewImei")}
                placeholder={t("enterNewImei")}
                value={newImei}
                disabled={systemAction.isPending}
                onChange={(event) => setNewImei(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    void handleUpdate();
                  }
                }}
              />
            </div>
            <Button
              className="mt-6"
              onClick={() => void handleUpdate()}
              disabled={systemAction.isPending}
            >
              {systemAction.isPending && <LoaderCircleIcon className="animate-spin" />}
              {t("update")}
            </Button>
          </div>
        </div>
      )}
    </Panel>
  );
}
