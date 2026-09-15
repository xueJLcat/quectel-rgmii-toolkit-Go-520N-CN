// 时间同步卡:Switch 即时启用 + 间隔(1-1440)/服务器(空=缺省 ntp.aliyun.com 提示)表单
// + 运行状态行(pollerRunning/lastSyncTime/lastSyncOK/lastOffsetMs/systemTime)
// + "手动同步一次"按钮(spinner + offset 结果 toast,对齐旧 syncTimeNow);保存后回读。
import { ClockIcon, LoaderCircleIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import type { ApiParams } from "@/lib/api";
import { useT } from "@/lib/i18n";

import { useSaveTimesync, useTimesync, useTimesyncNow } from "../hooks";
import { TIME_SYNC_RANGE, buildTimeSyncPayload, isIntegerInRange, isValidNtpServer } from "../lib";

export function TimeSyncCard() {
  const { t } = useT("automation");
  const query = useTimesync();
  const save = useSaveTimesync();
  const syncNow = useTimesyncNow();
  const data = query.data;
  const [form, setForm] = useState({ enabled: false, intervalMinutes: "60", server: "" });

  useEffect(() => {
    if (!data) return;
    setForm({
      enabled: data.enabled === true,
      intervalMinutes: String(Number(data.intervalMinutes) > 0 ? Number(data.intervalMinutes) : 60),
      server: String(data.server ?? ""),
    });
  }, [data]);

  function mutate(params: ApiParams): void {
    save.mutate(params, {
      onSuccess: () => toast.success(t("common:saved")),
      onError: () => toast.error(t("common:saveFailed")),
    });
  }

  function handleToggle(next: boolean): void {
    setForm((current) => ({ ...current, enabled: next }));
    mutate({ enabled: next ? "1" : "0" });
  }

  function handleSave(): void {
    if (!isIntegerInRange(form.intervalMinutes, TIME_SYNC_RANGE.min, TIME_SYNC_RANGE.max)) {
      toast.error(t("syncIntervalMustBeAnIntegerBetween1And1440"));
      return;
    }
    if (!isValidNtpServer(form.server)) {
      toast.error(t("invalidNtpServerFormat"));
      return;
    }
    mutate(buildTimeSyncPayload(form.enabled, form.intervalMinutes, form.server));
  }

  function handleSyncNow(): void {
    syncNow.mutate(undefined, {
      onSuccess: (result) => {
        // 对齐旧 syncTimeNow:成功拼接偏差,失败带后端 error 说明
        if (result?.ok) {
          toast.success(`${t("timeSynced")}（${t("offset")} ${Number(result.offsetMs ?? 0)} ms）`);
        } else {
          toast.error(`${t("timeSyncFailed")}${result?.error ? `：${result.error}` : ""}`);
        }
      },
      onError: () => toast.error(t("timeSyncFailed")),
    });
  }

  function lastSyncText(): string {
    const time = data?.lastSyncTime ?? "";
    if (!time) return t("never");
    if (data?.lastSyncOK === true) {
      return `${time} · ${t("offset")} ${Number(data?.lastOffsetMs ?? 0)} ms`;
    }
    if (data?.lastSyncOK === false) return `${time} · ${t("common:failed")}`;
    return time;
  }

  return (
    <Panel
      domain="system"
      icon={ClockIcon}
      title={t("timeSync")}
      tools={
        <div className="flex items-center gap-2">
          <Label htmlFor="timesync-enabled" className="text-muted">
            {t("enableTimeSync")}
          </Label>
          <Switch
            id="timesync-enabled"
            aria-label={t("enableTimeSync")}
            checked={form.enabled}
            disabled={!data || save.isPending}
            onCheckedChange={handleToggle}
          />
        </div>
      }
      footer={t(
        "whenEnabledTheNtpServerSingleSourceIsPolledAtTheSyncIntervalAndTheSystemClockIsCorrectedDirectlyDisablingStopsThePoller",
      )}
    >
      {query.isPending ? (
        <div className="flex flex-col gap-3" aria-busy="true">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-6 w-52" />
        </div>
      ) : query.isError ? (
        <ErrorRetry
          message={t("failedToLoadTimeSyncSettings")}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      ) : (
        <div className="flex flex-col gap-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="timesync-interval">{t("syncIntervalMinutes")}</Label>
              <Input
                id="timesync-interval"
                type="number"
                min={TIME_SYNC_RANGE.min}
                max={TIME_SYNC_RANGE.max}
                aria-label={t("syncIntervalMinutes")}
                value={form.intervalMinutes}
                disabled={save.isPending}
                onChange={(event) => setForm({ ...form, intervalMinutes: event.target.value })}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="timesync-server">{t("ntpServer")}</Label>
              <Input
                id="timesync-server"
                type="text"
                placeholder="ntp.aliyun.com"
                aria-label={t("ntpServer")}
                value={form.server}
                disabled={save.isPending}
                onChange={(event) => setForm({ ...form, server: event.target.value })}
              />
              <p className="text-sm text-muted">{t("defaultsToTheAliyunNtpServerNtpAliyunCom")}</p>
            </div>
          </div>
          <div data-slot="timesync-status" className="flex flex-wrap items-center gap-2">
            <StatusChip
              tone={data?.enabled ? "success" : "muted"}
              pulse={data?.enabled && data?.pollerRunning}
            >
              {`${t("common:status")}: ${
                data?.enabled
                  ? data?.pollerRunning
                    ? t("polling")
                    : t("common:enabled")
                  : t("common:notEnabled")
              }`}
            </StatusChip>
            <StatusChip tone="muted">{`${t("lastSync")}: ${lastSyncText()}`}</StatusChip>
            {data?.systemTime && (
              <StatusChip tone="muted">{`${t("systemTime")}: ${data.systemTime}`}</StatusChip>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button onClick={handleSave} disabled={save.isPending}>
              {save.isPending && <LoaderCircleIcon className="animate-spin" />}
              {t("saveTimeSync")}
            </Button>
            <Button variant="secondary" onClick={handleSyncNow} disabled={syncNow.isPending}>
              {syncNow.isPending && <LoaderCircleIcon className="animate-spin" />}
              {syncNow.isPending ? t("syncing") : t("syncNow")}
            </Button>
          </div>
        </div>
      )}
    </Panel>
  );
}
