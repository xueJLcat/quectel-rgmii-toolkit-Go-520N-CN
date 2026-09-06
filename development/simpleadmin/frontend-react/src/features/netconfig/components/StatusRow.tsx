// 顶部状态徽章行(设计方案 §6.7①):IP 透传 / USB 协议 / DNS V4(dnsV4QueryFailed → 系统托管)/
// DNS V6 / DMZ / LAN IP 段;待重启生效 = amber StatusChip pulse(可手动关闭);
// pending → "获取中" chip(保留旧值);重试耗尽 → ErrorRetry(有旧值时并列展示)。
import { InfoIcon, XIcon } from "lucide-react";

import { ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { NetworkConfigResponse } from "@/lib/api";
import { useT } from "@/lib/i18n";

import { usbNetModeText } from "../lib";

export interface StatusRowProps {
  data: NetworkConfigResponse | undefined;
  pending: boolean;
  failed: boolean;
  retrying: boolean;
  onRetry: () => void;
  usbNetPendingMode: string | null;
  onDismissUsbPending: () => void;
}

export function StatusRow({
  data,
  pending,
  failed,
  retrying,
  onRetry,
  usbNetPendingMode,
  onDismissUsbPending,
}: StatusRowProps) {
  const { t } = useT("netconfig");
  const dnsV4Managed = data?.dnsV4QueryFailed === true;
  const dmzEnabled = data?.dmzMode === "1";
  const lanRange = data?.lanGwIp
    ? `${data.lanGwIp} (${data.lanIpStart || "-"} - ${data.lanIpEnd || "-"})`
    : "-";
  return (
    <Panel
      domain="network"
      icon={InfoIcon}
      title={t("currentStatus")}
      tools={
        pending ? (
          <StatusChip tone="info" pulse>
            {t("common:fetching")}
          </StatusChip>
        ) : undefined
      }
    >
      {data ? (
        <div className="flex flex-wrap items-center gap-2">
          <StatusChip tone={data.ipPassStatus ? "success" : "muted"}>{`${t("ipPassthrough")}: ${
            data.ipPassStatus ? t("common:enabled") : t("common:notEnabled")
          }`}</StatusChip>
          <StatusChip tone="muted">{`${t("usbProtocol")}: ${usbNetModeText(
            data.currentUsbNetMode,
            t("unknownMode"),
          )}`}</StatusChip>
          <StatusChip
            tone={dnsV4Managed || data.DNSV4ProxyStatus ? "success" : "muted"}
          >{`DNS V4: ${
            dnsV4Managed
              ? t("systemManaged")
              : data.DNSV4ProxyStatus
                ? t("common:enabled")
                : t("common:notEnabled")
          }`}</StatusChip>
          <StatusChip tone={data.DNSV6ProxyStatus ? "success" : "muted"}>{`DNS V6: ${
            data.DNSV6ProxyStatus ? t("common:enabled") : t("common:notEnabled")
          }`}</StatusChip>
          <StatusChip tone={dmzEnabled ? "success" : "muted"}>{`${t("dmz")}: ${
            dmzEnabled && data.dmzIP
              ? `${t("common:enabled")} (${data.dmzIP})`
              : dmzEnabled
                ? t("common:enabled")
                : t("common:notEnabled")
          }`}</StatusChip>
          <StatusChip tone="muted">{`${t("lanIpRange")}: ${lanRange}`}</StatusChip>
          {usbNetPendingMode && (
            <span className="inline-flex items-center gap-1">
              <StatusChip tone="warning" pulse>
                {t("pendingRebootToTakeEffect")}
              </StatusChip>
              <Button
                variant="ghost"
                size="icon"
                className="size-6"
                aria-label={t("common:close")}
                onClick={onDismissUsbPending}
              >
                <XIcon />
              </Button>
            </span>
          )}
        </div>
      ) : failed ? null : (
        <div aria-busy="true" className="flex flex-wrap items-center gap-2">
          <Skeleton className="h-6 w-36 rounded-sm" />
          <Skeleton className="h-6 w-28 rounded-sm" />
          <Skeleton className="h-6 w-32 rounded-sm" />
          <span className="text-sm text-muted">{t("common:loadingStatus")}</span>
        </div>
      )}
      {failed && (
        <ErrorRetry
          className={data ? "mt-3" : undefined}
          message={t("common:failedToLoadStatus")}
          onRetry={onRetry}
          retrying={retrying}
        />
      )}
    </Panel>
  );
}
