// 载波聚合表(设计方案 §6.3):PCC=violet 徽章(comm 域 soft 底)/ SCC=sky 徽章(info 语义色);
// 聚合状态 chip:单载波(无聚合)=muted / 聚合生效=success pulse;空列表 → 空态"无"。
import type { SignalCarrier } from "@/lib/api";

import { EmptyState, StatusChip } from "@/components/common";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useT } from "@/lib/i18n";

import { aggregationState } from "../lib";

export function CarrierRoleBadge({ role }: { role?: string }) {
  const { t } = useT("signal");
  if (role === "PCC") {
    return <Badge className="bg-comm-soft text-comm">{t("primaryCarrierPcc")}</Badge>;
  }
  if (role === "SCC") {
    return <Badge variant="info">{t("secondaryCarrierScc")}</Badge>;
  }
  return <Badge variant="muted">{role && role !== "" ? role : "-"}</Badge>;
}

export function AggregationStatusChip({ carriers }: { carriers: readonly SignalCarrier[] }) {
  const { t } = useT("signal");
  const { state, count } = aggregationState(carriers);
  if (state === "none") return <StatusChip tone="muted">{t("common:none")}</StatusChip>;
  if (state === "single") {
    return <StatusChip tone="muted">{t("singleCarrierNoAggregation")}</StatusChip>;
  }
  return (
    <StatusChip tone="success" pulse>
      {`${t("carrierAggregationActive")} (${count} ${t("carriers")})`}
    </StatusChip>
  );
}

export function CarrierTable({ carriers }: { carriers: readonly SignalCarrier[] }) {
  const { t } = useT("signal");
  if (carriers.length === 0) {
    return <EmptyState title={t("common:none")} className="py-6" />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("role")}</TableHead>
          <TableHead>{t("band")}</TableHead>
          <TableHead>{t("arfcn")}</TableHead>
          <TableHead>{t("common:bandwidth")}</TableHead>
          <TableHead>{t("pci")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {carriers.map((carrier, index) => (
          <TableRow key={`${carrier.role ?? ""}-${carrier.arfcn ?? ""}-${index}`}>
            <TableCell>
              <CarrierRoleBadge role={carrier.role} />
            </TableCell>
            <TableCell>{carrier.band || "-"}</TableCell>
            <TableCell className="tabular-nums">{carrier.arfcn || "-"}</TableCell>
            <TableCell>{carrier.bandwidth || "-"}</TableCell>
            <TableCell className="tabular-nums">{carrier.pci || "-"}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
