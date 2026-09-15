// 接口状态表(设计方案 §6.6):name/MAC/状态 chip/MTU/累计上下行(人性化)/当前速率内嵌 Sparkline。
// 速率历史来自组件内滚动窗口(useRateHistory,≤30 点),下行=network 域色、上行=comm 域色。
import { DOMAIN_GRADIENTS, Sparkline } from "@/components/charts";
import { EmptyState, StatusChip } from "@/components/common";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { NetworkInterfaceStat } from "@/lib/api";
import { useT } from "@/lib/i18n";

import { formatBytes, formatRate, interfaceStateTone } from "../lib";
import type { RateHistory } from "../lib";

export interface InterfaceTableProps {
  interfaces: readonly NetworkInterfaceStat[];
  history: RateHistory;
}

export function InterfaceTable({ interfaces, history }: InterfaceTableProps) {
  const { t } = useT("netdetail");
  if (interfaces.length === 0) {
    return <EmptyState title={t("noInterfaceData")} className="py-6" />;
  }
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("interface")}</TableHead>
            <TableHead>{t("mac")}</TableHead>
            <TableHead>{t("common:status")}</TableHead>
            <TableHead>{t("mtu")}</TableHead>
            <TableHead>{t("totalDownload")}</TableHead>
            <TableHead>{t("totalUpload")}</TableHead>
            <TableHead>{t("downloadRate")}</TableHead>
            <TableHead>{t("uploadRate")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {interfaces.map((iface, index) => {
            const name = iface.name ?? "";
            const series = history[name];
            return (
              <TableRow key={name || index}>
                <TableCell className="font-medium text-ink">{name || "-"}</TableCell>
                <TableCell className="font-mono text-sm">{iface.mac || "-"}</TableCell>
                <TableCell>
                  <StatusChip tone={interfaceStateTone(iface.state)}>
                    {iface.state || "-"}
                  </StatusChip>
                </TableCell>
                <TableCell className="tabular-nums">{iface.mtu ?? "-"}</TableCell>
                <TableCell className="tabular-nums">{formatBytes(iface.rxBytes)}</TableCell>
                <TableCell className="tabular-nums">{formatBytes(iface.txBytes)}</TableCell>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <span className="text-sm tabular-nums">{formatRate(iface.rxRate)}</span>
                    <Sparkline
                      data={series?.rx ?? []}
                      color={DOMAIN_GRADIENTS.network[0]}
                      className="h-6 w-20 shrink-0"
                    />
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <span className="text-sm tabular-nums">{formatRate(iface.txRate)}</span>
                    <Sparkline
                      data={series?.tx ?? []}
                      color={DOMAIN_GRADIENTS.comm[0]}
                      className="h-6 w-20 shrink-0"
                    />
                  </div>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
