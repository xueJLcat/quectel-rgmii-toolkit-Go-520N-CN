// WAN/LAN 地址卡组(设计方案 §6.6):mono 大字 + CopyButton(复制微交互 §5.2);
// 值无效("-")时隐藏复制按钮,CopyButton 图标缺省即"复制"语义。
import { GlobeIcon, NetworkIcon } from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { CopyButton } from "@/components/common";
import { Card } from "@/components/ui/card";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

import { hasAddress } from "../lib";

interface AddressCardProps {
  label: string;
  value: string | undefined;
  icon: LucideIcon;
}

function AddressCard({ label, value, icon: Icon }: AddressCardProps) {
  const text = hasAddress(value) ? String(value) : "-";
  return (
    <Card interactive className="p-4">
      <div className="flex items-center gap-2 text-xs text-muted">
        <Icon className="size-4 shrink-0 text-network" aria-hidden="true" />
        <span className="truncate font-medium">{label}</span>
        {hasAddress(value) && <CopyButton text={text} className="ml-auto size-7" />}
      </div>
      <div className="mt-2 break-all font-mono text-xl font-semibold text-ink">{text}</div>
    </Card>
  );
}

export interface AddressCardsProps {
  wanIPv4?: string;
  wanIPv6?: string;
  lanGateway?: string;
  className?: string;
}

export function AddressCards({ wanIPv4, wanIPv6, lanGateway, className }: AddressCardsProps) {
  const { t } = useT("netdetail");
  return (
    <div className={cn("grid grid-cols-1 gap-4 sm:grid-cols-3", className)}>
      <AddressCard label={t("wanIPv4")} value={wanIPv4} icon={GlobeIcon} />
      <AddressCard label={t("wanIPv6")} value={wanIPv6} icon={GlobeIcon} />
      <AddressCard label={t("lanGateway")} value={lanGateway} icon={NetworkIcon} />
    </div>
  );
}
