// 每日定时重启卡:Switch 即时启用(仅提交 rebootEnabled)+ 状态行(lastRebootDate)在前
// + 原生时间选择器(type=time,格式校验前端拦截)与保存按钮同行;
// 保存后回读(get/set 契约,对齐旧 fetchScheduler/saveScheduler)。
import { AlarmClockIcon, LoaderCircleIcon } from "lucide-react";
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

import { useSaveScheduler, useScheduler } from "../hooks";
import { buildSchedulerPayload, isValidRebootTime } from "../lib";

export function SchedulerCard() {
  const { t } = useT("automation");
  const query = useScheduler();
  const save = useSaveScheduler();
  const data = query.data;
  const [form, setForm] = useState({ enabled: false, rebootTime: "04:00" });

  useEffect(() => {
    if (!data) return;
    setForm({
      enabled: data.rebootEnabled === true,
      rebootTime: data.rebootTime ? String(data.rebootTime) : "04:00",
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
    mutate({ rebootEnabled: next ? "1" : "0" });
  }

  function handleSave(): void {
    if (!isValidRebootTime(form.rebootTime)) {
      toast.error(t("invalidRebootTimeFormatExpectedHhMm"));
      return;
    }
    mutate(buildSchedulerPayload(form.enabled, form.rebootTime));
  }

  return (
    <Panel
      domain="system"
      icon={AlarmClockIcon}
      title={t("dailyScheduledReboot")}
      tools={
        <div className="flex items-center gap-2">
          <Label htmlFor="scheduler-enabled" className="text-muted">
            {t("enableScheduledReboot")}
          </Label>
          <Switch
            id="scheduler-enabled"
            aria-label={t("enableScheduledReboot")}
            checked={form.enabled}
            disabled={!data || save.isPending}
            onCheckedChange={handleToggle}
          />
        </div>
      }
    >
      {query.isPending ? (
        <div className="flex flex-col gap-3" aria-busy="true">
          <Skeleton className="h-6 w-40" />
          <Skeleton className="h-9 w-full" />
        </div>
      ) : query.isError ? (
        <ErrorRetry
          message={t("failedToLoadScheduledRebootSettings")}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      ) : (
        <div className="flex flex-1 flex-col gap-4">
          {/* 状态行:启用状态 + 上次执行(原 chips 上移) */}
          <div data-slot="scheduler-status" className="flex flex-wrap items-center gap-2">
            <StatusChip tone={form.enabled ? "success" : "muted"}>
              {`${t("common:status")}: ${
                form.enabled ? t("common:enabled") : t("common:notEnabled")
              }`}
            </StatusChip>
            <StatusChip tone="muted">
              {`${t("lastRebootDate")}: ${data?.lastRebootDate || t("common:none")}`}
            </StatusChip>
          </div>
          {/* 控件行:原生时间选择器(type=time,替代手敲 HH:MM)+ 保存按钮紧随其右 */}
          <div className="flex flex-wrap items-center gap-3">
            <Label htmlFor="scheduler-time" className="text-muted">
              {t("rebootTime")}
            </Label>
            <Input
              id="scheduler-time"
              type="time"
              className="w-36"
              aria-label={t("rebootTime")}
              value={form.rebootTime}
              disabled={save.isPending}
              onChange={(event) => setForm({ ...form, rebootTime: event.target.value })}
            />
            <Button onClick={handleSave} disabled={save.isPending}>
              {save.isPending && <LoaderCircleIcon className="animate-spin" />}
              {t("saveScheduledReboot")}
            </Button>
          </div>
        </div>
      )}
    </Panel>
  );
}
