// 上游 DNS 编辑卡(设计方案 §6.7⑤):≤4 行编辑器,行内 IPv4/IPv6 校验(非法行红框;
// 保存时第一个非法行 → common:dnsLineInvalid 行号插值 toast,对齐旧版),大小写不敏感去重;
// 启用自定义/保存前 confirm(dnsmasq 重启 ~1s,mutation isPending 即 loading),恢复运营商 danger confirm;
// DNS V6 代理未开启时显示"可能不生效"联动警告(对齐 dns-proxy 优化方案)。
import { LoaderCircleIcon, PlusIcon, SaveIcon, ServerIcon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import type { useDnsUpstreamQuery, useDnsUpstreamSave } from "../hooks";
import { DNS_UPSTREAM_LIMIT, parseDnsLines, validIPv4, validIPv6 } from "../lib";

export interface DnsUpstreamCardProps {
  query: ReturnType<typeof useDnsUpstreamQuery>;
  actions: ReturnType<typeof useDnsUpstreamSave>;
  dnsV6Enabled: boolean;
  statusReady: boolean;
}

export function DnsUpstreamCard({
  query,
  actions,
  dnsV6Enabled,
  statusReady,
}: DnsUpstreamCardProps) {
  const { t } = useT("netconfig");
  const confirm = useConfirmStore((state) => state.confirm);
  const data = query.data;
  const [lines, setLines] = useState<string[]>([""]);

  // 服务端值到达/保存成功回读后填充编辑器
  useEffect(() => {
    if (!data) return;
    setLines(data.servers.length > 0 ? [...data.servers] : [""]);
  }, [data]);

  const busy = actions.save.isPending || actions.restore.isPending;
  const loaded = !!data;

  const setLine = (index: number, value: string) => {
    setLines((prev) => prev.map((line, i) => (i === index ? value : line)));
  };
  const addLine = () => {
    setLines((prev) => (prev.length >= DNS_UPSTREAM_LIMIT ? prev : [...prev, ""]));
  };
  const removeLine = (index: number) => {
    setLines((prev) => (prev.length <= 1 ? prev : prev.filter((_, i) => i !== index)));
  };

  const onSave = async () => {
    const { servers, invalidLine } = parseDnsLines(lines);
    if (invalidLine !== null) {
      toast.error(t("common:dnsLineInvalid", { line: invalidLine }));
      return;
    }
    if (servers.length === 0) {
      toast.error(t("pleaseEnterAtLeastOneDnsServer"));
      return;
    }
    if (servers.length > DNS_UPSTREAM_LIMIT) {
      toast.error(t("youCanConfigureUpTo4UpstreamDnsServers"));
      return;
    }
    const ok = await confirm({
      title: t("customUpstreamDns"),
      message: t("savingWillBrieflyRestartTheLocalDnsServiceAbout1SecondSave"),
      confirmText: t("common:save"),
    });
    if (ok) actions.save.mutate(servers);
  };

  const onRestore = async () => {
    const ok = await confirm({
      title: t("restoreCarrierDns"),
      message: t("upstreamDnsWillFollowTheCarrierAfterRestoringRestore"),
      confirmText: t("restoreCarrier"),
      danger: true,
    });
    if (ok) actions.restore.mutate();
  };

  return (
    <Panel
      domain="network"
      icon={ServerIcon}
      title={t("upstreamDns")}
      tools={
        data ? (
          <StatusChip tone={data.enabled ? "success" : "muted"}>
            {data.enabled ? t("custom") : t("carrierProvided")}
          </StatusChip>
        ) : undefined
      }
      footer={t("savingBrieflyRestartsTheLocalDnsServiceAbout1SecondSupportsIpv4Ipv6UpTo4Servers")}
    >
      <div className="flex flex-col gap-3">
        {statusReady && !dnsV6Enabled && (
          <p className="text-xs text-warning">
            {t("theDnsV6ProxyIsCurrentlyDisabledCustomUpstreamDnsMayNotTakeEffect")}
          </p>
        )}
        {query.isError ? (
          <ErrorRetry
            message={query.error?.message || t("common:failedToLoadStatus")}
            onRetry={() => void query.refetch()}
            retrying={query.isFetching}
          />
        ) : (
          <>
            {/* 整行卡片:≤4 个服务器输入宽屏两列排布 */}
            <div className="grid gap-2 sm:grid-cols-2">
              {lines.map((line, index) => {
                const trimmed = line.trim();
                const invalid = trimmed !== "" && !validIPv4(trimmed) && !validIPv6(trimmed);
                return (
                  <div key={index} className="flex items-center gap-2">
                    <Input
                      value={line}
                      invalid={invalid}
                      disabled={busy || !loaded}
                      onChange={(event) => setLine(index, event.target.value)}
                      placeholder={t("onePerLineEG223555")}
                      aria-label={`${t("upstreamDnsServers")} ${index + 1}`}
                    />
                    {lines.length > 1 && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="shrink-0"
                        aria-label={t("common:delete")}
                        disabled={busy || !loaded}
                        onClick={() => removeLine(index)}
                      >
                        <XIcon />
                      </Button>
                    )}
                  </div>
                );
              })}
            </div>
            {/* 底部一行:左"添加",右动作组(保存/恢复运营商 或 启用自定义,右下角) */}
            <div className="flex flex-wrap items-center justify-between gap-2">
              <Button
                variant="secondary"
                size="sm"
                className="w-fit"
                disabled={busy || !loaded || lines.length >= DNS_UPSTREAM_LIMIT}
                onClick={addLine}
              >
                <PlusIcon />
                {t("common:add")}
              </Button>
              <div className="flex flex-wrap gap-2">
                {data?.enabled ? (
                  <>
                    <Button disabled={busy || !loaded} onClick={() => void onSave()}>
                      {actions.save.isPending ? (
                        <LoaderCircleIcon className="animate-spin" />
                      ) : (
                        <SaveIcon />
                      )}
                      {t("common:save")}
                    </Button>
                    <Button
                      variant="danger"
                      disabled={busy || !loaded}
                      onClick={() => void onRestore()}
                    >
                      {actions.restore.isPending && <LoaderCircleIcon className="animate-spin" />}
                      {t("restoreCarrier")}
                    </Button>
                  </>
                ) : (
                  <Button disabled={busy || !loaded} onClick={() => void onSave()}>
                    {actions.save.isPending ? (
                      <LoaderCircleIcon className="animate-spin" />
                    ) : (
                      <SaveIcon />
                    )}
                    {t("enableCustom")}
                  </Button>
                )}
              </div>
            </div>
          </>
        )}
      </div>
    </Panel>
  );
}
