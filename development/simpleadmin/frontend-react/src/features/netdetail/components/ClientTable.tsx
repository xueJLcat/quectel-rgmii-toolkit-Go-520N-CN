// 局域网设备表(设计方案 §6.6):主机名/MAC/IP/剩余租期 chip(倒计时语义;-1=ARP/静态 → "—" muted)。
import { EmptyState, StatusChip } from "@/components/common";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { LanClient } from "@/lib/api";
import { useT } from "@/lib/i18n";

import { formatLease, leaseTone } from "../lib";

export function ClientTable({ clients }: { clients: readonly LanClient[] }) {
  const { t } = useT("netdetail");
  const units = {
    days: t("common:days"),
    hours: t("common:hours"),
    minutes: t("common:minutes"),
    underOneMinute: t("underOneMinute"),
  };
  if (clients.length === 0) {
    return <EmptyState title={t("noOnlineDevices")} className="py-6" />;
  }
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("hostname")}</TableHead>
            <TableHead>{t("mac")}</TableHead>
            <TableHead>{t("ip")}</TableHead>
            <TableHead>{t("leaseRemaining")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {clients.map((client, index) => (
            <TableRow key={`${client.mac ?? ""}-${client.ip ?? ""}-${index}`}>
              <TableCell className="max-w-48 truncate font-medium text-ink">
                {client.hostname || "-"}
              </TableCell>
              <TableCell className="font-mono text-sm">{client.mac || "-"}</TableCell>
              <TableCell className="font-mono text-sm">{client.ip || "-"}</TableCell>
              <TableCell>
                <StatusChip tone={leaseTone(client.leaseSeconds)}>
                  {formatLease(client.leaseSeconds, units)}
                </StatusChip>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
