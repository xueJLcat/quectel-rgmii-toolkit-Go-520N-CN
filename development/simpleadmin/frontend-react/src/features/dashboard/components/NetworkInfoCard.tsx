// 网络信息卡(设计方案 §6.2 区块⑤):精简/完整切换(旧版 compactNetworkInfo,localStorage 键
// simpleadmin.dashboard.compactNetworkInfo 沿用),完整态额外显示 MCCMNC/CELL ID/eNB ID/TAC 四行;
// 字段值 mono 字体 + CopyButton("-"占位不显示复制);tools 区承载 pending/失败 StatusChip(旧值保留)。
import { NetworkIcon } from "lucide-react";

import { CopyButton } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import type { DashboardData } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import type { TrafficView } from "../lib";

function InfoRow({
  label,
  value,
  copyable = true,
}: {
  label: string;
  value: string;
  copyable?: boolean;
}) {
  const empty = !value || value === "-";
  return (
    <div className="flex items-center justify-between gap-3 border-b border-line py-2 last:border-b-0">
      <span className="shrink-0 text-sm text-muted">{label}</span>
      <span className="flex min-w-0 items-center gap-1">
        <span
          className={cn("truncate font-mono text-sm", empty ? "text-muted" : "text-ink")}
          title={value}
        >
          {empty ? "-" : value}
        </span>
        {copyable && !empty && <CopyButton text={value} className="size-7 shrink-0" />}
      </span>
    </div>
  );
}

export interface NetworkInfoCardProps {
  data: DashboardData | undefined;
  compact: boolean;
  onToggleCompact: () => void;
  traffic: TrafficView;
  activeSimText: string;
  uptimeText: string;
  assessmentText: string;
}

export function NetworkInfoCard({
  data,
  compact,
  onToggleCompact,
  traffic,
  activeSimText,
  uptimeText,
  assessmentText,
}: NetworkInfoCardProps) {
  const { t } = useT("dashboard");
  const { t: tc } = useT();
  const scc = data?.scc_pci ?? "-";
  const pciText =
    scc !== "-" && scc !== "" ? `${data?.pcc_pci ?? "-"} / ${scc}` : (data?.pcc_pci ?? "-");
  const rsrpAntennas = [data?.prxqrsrp, data?.drxqrsrp, data?.rx2qrsrp, data?.rx3qrsrp]
    .map((value) => value || "-")
    .join("  ");
  return (
    <Panel
      domain="monitor"
      icon={NetworkIcon}
      title={t("networkInfo")}
      tools={
        <Button variant="secondary" size="sm" onClick={onToggleCompact}>
          {compact ? t("fullView") : t("compactView")}
        </Button>
      }
    >
      <dl className="grid gap-x-8 sm:grid-cols-2">
        <InfoRow label={t("activeSim")} value={activeSimText} copyable={false} />
        <InfoRow label={t("carrier")} value={data?.network_provider ?? "-"} copyable={false} />
        {!compact && <InfoRow label={t("mccmnc")} value={data?.mccmnc ?? "-"} />}
        <InfoRow label={t("apn")} value={data?.apn ?? "-"} />
        <InfoRow label={t("networkMode")} value={data?.network_mode ?? "-"} copyable={false} />
        <InfoRow label={t("bands")} value={data?.bands ?? "-"} />
        <InfoRow label={tc("bandwidth")} value={data?.bandwidth ?? "-"} copyable={false} />
        <InfoRow label={t("pci")} value={pciText} />
        <InfoRow label={t("ipv4")} value={data?.ipv4 ?? "-"} />
        <InfoRow label={t("ipv6")} value={data?.ipv6 ?? "-"} />
        {!compact && <InfoRow label={t("cellId")} value={data?.cellID ?? "-"} />}
        {!compact && <InfoRow label={t("enbId")} value={data?.eNBID ?? "-"} />}
        <InfoRow label={t("earfcn")} value={data?.earfcns ?? "-"} />
        {!compact && <InfoRow label={t("tac")} value={data?.tac ?? "-"} />}
        <InfoRow
          label={t("totalTraffic")}
          value={`${traffic.rxTotal} / ${traffic.txTotal}`}
          copyable={false}
        />
        <InfoRow label={t("speed")} value={`${traffic.dl} / ${traffic.ul}`} copyable={false} />
        <InfoRow label={t("signalAssessment")} value={assessmentText} copyable={false} />
        <InfoRow label={t("rsrpAntennas")} value={rsrpAntennas} copyable={false} />
        <InfoRow label={t("uptime")} value={uptimeText} copyable={false} />
      </dl>
    </Panel>
  );
}
