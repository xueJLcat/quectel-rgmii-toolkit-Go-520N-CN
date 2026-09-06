// DNS 代理卡(设计方案 §6.7④):V4 开关(dnsV4QueryFailed → 系统托管只读态)/
// V6 开关;开关 checked 绑定权威 status 值(慢速 AT 期间不显示虚假状态,对齐旧版回弹语义),
// 即时生效 + toast + 回读(见 hooks.useDnsProxy)。
// 整行卡片布局:V4 与 V6 的开关/状态合并为单行,中间竖分割线(窄屏换行时隐藏),
// 托管长说明移至 Panel footer 说明条。
import { GlobeIcon } from "lucide-react";

import { StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import type { NetworkConfigResponse } from "@/lib/api";
import { useT } from "@/lib/i18n";

import type { useDnsProxy } from "../hooks";

export interface DnsProxyCardProps {
  data: NetworkConfigResponse | undefined;
  ready: boolean;
  dnsProxy: ReturnType<typeof useDnsProxy>;
}

export function DnsProxyCard({ data, ready, dnsProxy }: DnsProxyCardProps) {
  const { t } = useT("netconfig");
  const v4Managed = data?.dnsV4QueryFailed === true;
  const v4Enabled = data?.DNSV4ProxyStatus === true;
  const v6Enabled = data?.DNSV6ProxyStatus === true;
  const disabled = !ready || dnsProxy.isPending;
  return (
    <Panel
      domain="network"
      icon={GlobeIcon}
      title={t("dnsProxy")}
      footer={
        v4Managed
          ? t("dnsV4IsManagedInternallyByTheSystemAndIsAlwaysActiveItCannotBeQueriedOrModified")
          : undefined
      }
    >
      <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
        <div className="flex min-w-0 items-center gap-3">
          <span className="text-sm font-medium text-ink">DNS V4</span>
          {v4Managed ? (
            <StatusChip tone="success" className="shrink-0">
              {t("systemManaged")}
            </StatusChip>
          ) : (
            <>
              <span className="text-sm text-muted">
                {v4Enabled ? t("common:enabled") : t("common:notEnabled")}
              </span>
              <Switch
                aria-label={t("dnsV4ProxyToggle")}
                checked={v4Enabled}
                disabled={disabled}
                onCheckedChange={(checked) => dnsProxy.mutate({ family: "4", enabled: checked })}
              />
            </>
          )}
        </div>
        <Separator orientation="vertical" className="h-6 max-sm:hidden" aria-hidden="true" />
        <div className="flex items-center gap-3">
          <span className="text-sm font-medium text-ink">DNS V6</span>
          <span className="text-sm text-muted">
            {v6Enabled ? t("common:enabled") : t("common:notEnabled")}
          </span>
          <Switch
            aria-label={t("dnsV6ProxyToggle")}
            checked={v6Enabled}
            disabled={disabled}
            onCheckedChange={(checked) => dnsProxy.mutate({ family: "6", enabled: checked })}
          />
        </div>
      </div>
    </Panel>
  );
}
