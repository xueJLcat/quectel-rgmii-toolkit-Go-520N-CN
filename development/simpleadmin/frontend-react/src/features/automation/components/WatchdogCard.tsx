// 断网自愈看门狗卡(设计方案 §6.13):Switch 即时启用(仅提交 enabled,后端按参数存在性合并)
// + 数值表单(失败次数 1-60 / 冷却 1-1440 / 间隔 1-30,范围校验前端拦截)+ 探测目标 textarea
// (每行 host[:port] ≤4,空=内置提示)+ 动作策略 radio + 运行状态行(pollerRunning/连续失败/上次自愈)。
// 保存后回读同步(get/set 契约,对齐旧 fetchWatchdog)。
import { HeartPulseIcon, LoaderCircleIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { ApiParams } from "@/lib/api";
import { useT } from "@/lib/i18n";

import { useSaveWatchdog, useWatchdog } from "../hooks";
import {
  MAX_WATCHDOG_TARGETS,
  buildWatchdogPayload,
  isIntegerInRange,
  validateTargets,
  watchdogActionLabelKey,
  WATCHDOG_RANGES,
} from "../lib";
import type { WatchdogForm } from "../lib";

const DEFAULT_FORM: WatchdogForm = {
  enabled: false,
  failThreshold: "5",
  cooldownMinutes: "30",
  checkIntervalMinutes: "3",
  targets: "",
  actionPolicy: "reboot",
};

export function WatchdogCard() {
  const { t } = useT("automation");
  const query = useWatchdog();
  const save = useSaveWatchdog();
  const data = query.data;
  const [form, setForm] = useState<WatchdogForm>(DEFAULT_FORM);

  useEffect(() => {
    if (!data) return;
    setForm({
      enabled: data.enabled === true,
      failThreshold: String(
        Number(data.failThreshold) > 0 ? Number(data.failThreshold) : DEFAULT_FORM.failThreshold,
      ),
      cooldownMinutes: String(
        Number(data.cooldownMinutes) > 0
          ? Number(data.cooldownMinutes)
          : DEFAULT_FORM.cooldownMinutes,
      ),
      checkIntervalMinutes: String(
        Number(data.checkIntervalMinutes) > 0
          ? Number(data.checkIntervalMinutes)
          : DEFAULT_FORM.checkIntervalMinutes,
      ),
      targets: (data.targets ?? []).join("\n"),
      actionPolicy: data.actionPolicy === "escalate" ? "escalate" : "reboot",
    });
  }, [data]);

  function mutate(params: ApiParams): void {
    save.mutate(params, {
      onSuccess: () => toast.success(t("common:saved")),
      onError: () => toast.error(t("common:saveFailed")),
    });
  }

  function handleToggle(next: boolean): void {
    // 开关即时生效(§5.2):乐观更新本地,失败由回读纠正
    setForm((current) => ({ ...current, enabled: next }));
    mutate({ enabled: next ? "1" : "0" });
  }

  function handleSave(): void {
    const { failThreshold, cooldownMinutes, checkIntervalMinutes } = WATCHDOG_RANGES;
    if (!isIntegerInRange(form.failThreshold, failThreshold.min, failThreshold.max)) {
      toast.error(t("failuresMustBeAnIntegerBetween1And60"));
      return;
    }
    if (!isIntegerInRange(form.cooldownMinutes, cooldownMinutes.min, cooldownMinutes.max)) {
      toast.error(t("cooldownMinutesMustBeAnIntegerBetween1And1440"));
      return;
    }
    if (
      !isIntegerInRange(
        form.checkIntervalMinutes,
        checkIntervalMinutes.min,
        checkIntervalMinutes.max,
      )
    ) {
      toast.error(t("checkIntervalMustBeAnIntegerBetween1And30"));
      return;
    }
    const targetsError = validateTargets(form.targets);
    if (targetsError?.reason === "tooMany") {
      toast.error(t("targetsTooMany", { max: MAX_WATCHDOG_TARGETS }));
      return;
    }
    if (targetsError?.reason === "invalidLine") {
      toast.error(t("invalidTargetLine", { line: targetsError.line }));
      return;
    }
    mutate(buildWatchdogPayload(form));
  }

  function lastActionText(): string {
    const time = data?.lastActionTime ?? "";
    if (!time) return t("never");
    const labelKey = watchdogActionLabelKey(data?.lastActionType);
    return labelKey ? `${time} · ${t(labelKey)}` : time;
  }

  const loaded = Boolean(data);

  return (
    <Panel
      domain="system"
      icon={HeartPulseIcon}
      title={t("offlineSelfHealingWatchdog")}
      tools={
        <div className="flex items-center gap-2">
          <Label htmlFor="watchdog-enabled" className="text-muted">
            {t("enableWatchdog")}
          </Label>
          <Switch
            id="watchdog-enabled"
            aria-label={t("enableWatchdog")}
            checked={form.enabled}
            disabled={!loaded || save.isPending}
            onCheckedChange={handleToggle}
          />
        </div>
      }
      footer={t(
        "whenEnabledConnectivityIsProbedAtTheCheckIntervalTheRecoveryActionRunsAfterConsecutiveFailuresReachTheThresholdAndTheCooldownHasPassedDisablingStopsThePoller",
      )}
    >
      {query.isPending ? (
        <div className="flex flex-col gap-3" aria-busy="true">
          <Skeleton className="h-6 w-64" />
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      ) : query.isError ? (
        <ErrorRetry
          message={t("failedToLoadWatchdogSettings")}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      ) : (
        <div className="flex flex-col gap-4">
          <div data-slot="watchdog-status" className="flex flex-wrap items-center gap-2">
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
            <StatusChip tone={(data?.consecutiveFailures ?? 0) > 0 ? "warning" : "muted"}>
              {`${t("consecutiveFailures")}: ${data?.consecutiveFailures ?? 0} / ${
                data?.failThreshold ?? 0
              }`}
            </StatusChip>
            <StatusChip tone="muted">{`${t("lastRecovery")}: ${lastActionText()}`}</StatusChip>
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="watchdog-threshold">{t("failures")}</Label>
              <Input
                id="watchdog-threshold"
                type="number"
                min={WATCHDOG_RANGES.failThreshold.min}
                max={WATCHDOG_RANGES.failThreshold.max}
                aria-label={t("failures")}
                value={form.failThreshold}
                disabled={save.isPending}
                onChange={(event) => setForm({ ...form, failThreshold: event.target.value })}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="watchdog-cooldown">{t("cooldownMin")}</Label>
              <Input
                id="watchdog-cooldown"
                type="number"
                min={WATCHDOG_RANGES.cooldownMinutes.min}
                max={WATCHDOG_RANGES.cooldownMinutes.max}
                aria-label={t("cooldownMin")}
                value={form.cooldownMinutes}
                disabled={save.isPending}
                onChange={(event) => setForm({ ...form, cooldownMinutes: event.target.value })}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="watchdog-interval">{t("checkIntervalMinutes")}</Label>
              <Input
                id="watchdog-interval"
                type="number"
                min={WATCHDOG_RANGES.checkIntervalMinutes.min}
                max={WATCHDOG_RANGES.checkIntervalMinutes.max}
                aria-label={t("checkIntervalMinutes")}
                value={form.checkIntervalMinutes}
                disabled={save.isPending}
                onChange={(event) => setForm({ ...form, checkIntervalMinutes: event.target.value })}
              />
            </div>
          </div>
          <div className="flex flex-col gap-2">
            <Label>{t("recoveryAction")}</Label>
            <RadioGroup
              aria-label={t("recoveryAction")}
              value={form.actionPolicy}
              onValueChange={(value) => setForm({ ...form, actionPolicy: value })}
              className="sm:flex-row sm:gap-6"
            >
              <div className="flex items-center gap-2">
                <RadioGroupItem value="reboot" id="policy-reboot" />
                <Label htmlFor="policy-reboot" className="font-normal">
                  {t("rebootModuleDirectly")}
                </Label>
              </div>
              <div className="flex items-center gap-2">
                <RadioGroupItem value="escalate" id="policy-escalate" />
                <Label htmlFor="policy-escalate" className="font-normal">
                  {t("reRegisterNetworkFirstRebootIfStillOffline")}
                </Label>
              </div>
            </RadioGroup>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="watchdog-targets">{t("probeTargets")}</Label>
            <Textarea
              id="watchdog-targets"
              rows={3}
              aria-label={t("probeTargets")}
              placeholder={t("onePerLineHostOrHostPortLeaveEmptyToUseBuiltInTargets")}
              value={form.targets}
              disabled={save.isPending}
              onChange={(event) => setForm({ ...form, targets: event.target.value })}
            />
            <p className="text-sm text-muted">
              {t(
                "whenEmptyBuiltInPublicDnsIsUsed22355511118888Port53UpTo4CustomTargetsInvalidEntriesAreIgnored",
              )}
            </p>
          </div>
          <div>
            <Button onClick={handleSave} disabled={save.isPending}>
              {save.isPending && <LoaderCircleIcon className="animate-spin" />}
              {t("saveWatchdogSettings")}
            </Button>
          </div>
        </div>
      )}
    </Panel>
  );
}
