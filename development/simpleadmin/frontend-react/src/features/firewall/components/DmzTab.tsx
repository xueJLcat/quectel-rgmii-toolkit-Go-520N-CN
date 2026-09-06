// Tab4 DMZ(从网络设置迁入):开关 + IP 表单(IPv4 校验),经 networkConfigData("dmz")。
// 状态读取 pending/不可信(开机保护期全默认值)时保持退避重试并显示"获取中"chip,绝不渲染假状态。
import { LoaderCircleIcon, ShieldAlertIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import { useDmzStatus, useSetDmz } from "../hooks";
import type { DmzSetParams } from "../hooks";
import { apiErrorMessage, isValidIPv4, resolveDmzMode } from "../lib";
import { StaggerGroup, StaggerItem } from "./Stagger";

export function DmzTab() {
  const { t } = useT("firewall");
  const confirm = useConfirmStore((state) => state.confirm);
  const dmzQuery = useDmzStatus();
  const setDmz = useSetDmz();
  const data = dmzQuery.data;
  const [ipInput, setIpInput] = useState("");

  // 服务端状态就绪后同步输入框("-" 为模块占位值,视为空)
  useEffect(() => {
    if (!data) return;
    const current = String(data.dmzIP ?? "").trim();
    setIpInput(current === "-" ? "" : current);
  }, [data]);

  const enabled = data
    ? resolveDmzMode(data.dmzMode, String(data.dmzIP ?? "").trim()) === "1"
    : false;
  const busy = setDmz.isPending;

  function submit(nextEnabled: boolean): void {
    const ip = ipInput.trim();
    if (nextEnabled && !isValidIPv4(ip)) {
      toast.error(t("pleaseEnterAValidIpAddress"));
      return;
    }
    const params: DmzSetParams = nextEnabled ? { enabled: "1", ip } : { enabled: "0" };
    setDmz.mutate(params, {
      onSuccess: () => {
        toast.success(t("common:saved"));
      },
      onError: (error) => {
        toast.error(apiErrorMessage(error, t("common:operationFailed")));
      },
    });
  }

  async function handleToggle(next: boolean): Promise<void> {
    if (!next) {
      // 关闭 DMZ 会立即改变入站流量走向,破坏性操作二次确认(§5.2)
      const ok = await confirm({
        title: t("common:disable"),
        message: t("dmzNote"),
        confirmText: t("common:disable"),
        danger: true,
      });
      if (!ok) return;
    }
    submit(next);
  }

  return (
    <StaggerGroup>
      <StaggerItem>
        <Panel
          domain="security"
          icon={ShieldAlertIcon}
          title={t("dmzSettings")}
          footer={t("dmzNote")}
        >
          {!data && dmzQuery.isPending ? (
            <div className="flex flex-col gap-3" aria-busy="true">
              <Skeleton className="h-6 w-32" />
              <Skeleton className="h-9 w-full" />
            </div>
          ) : !data ? (
            <ErrorRetry
              message={t("common:failedToLoadStatus")}
              onRetry={() => void dmzQuery.refetch()}
              retrying={dmzQuery.isFetching}
            />
          ) : (
            <div className="flex flex-col gap-4">
              <div className="flex flex-wrap items-center gap-3">
                <StatusChip tone={enabled ? "success" : "muted"}>
                  {`${t("common:status")}: ${enabled ? t("common:enabled") : t("common:disabled")}`}
                </StatusChip>
                {dmzQuery.isFetching && (
                  <StatusChip tone="info" pulse>
                    {t("common:fetching")}
                  </StatusChip>
                )}
                <div className="ml-auto flex items-center gap-2">
                  <Label htmlFor="dmz-switch">
                    {enabled ? t("common:enabled") : t("common:notEnabled")}
                  </Label>
                  <Switch
                    id="dmz-switch"
                    aria-label={t("dmzSettings")}
                    checked={enabled}
                    disabled={busy}
                    onCheckedChange={(next) => void handleToggle(next)}
                  />
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  className="w-56"
                  aria-label={t("dmzIpAddress")}
                  placeholder={t("dmzIpAddress")}
                  value={ipInput}
                  disabled={busy}
                  onChange={(event) => setIpInput(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" && enabled) {
                      event.preventDefault();
                      submit(true);
                    }
                  }}
                />
                <Button onClick={() => submit(true)} disabled={busy || !enabled}>
                  {busy && <LoaderCircleIcon className="animate-spin" />}
                  {t("common:save")}
                </Button>
              </div>
            </div>
          )}
        </Panel>
      </StaggerItem>
    </StaggerGroup>
  );
}
