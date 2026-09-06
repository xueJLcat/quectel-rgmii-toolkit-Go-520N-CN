// 设备信息定义列表卡(设计方案 §6.16):制造商/型号/固件/IMEI/IMSI/ICCID/电话号码/SIM 状态/
// LAN IP/WAN IPv4/IPv6,值一律 mono 字体 + CopyButton("-"占位不显示复制);
// 电话号码缺失显示"无本机号码"词条;SIM 状态走 StatusChip(服务端固定中文枚举映射)。
import type { ReactNode } from "react";

import { CopyButton, StatusChip } from "@/components/common";
import type { DeviceInfoData } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { isPlaceholderValue, simStatusView } from "../lib";

function InfoRow({
  label,
  value,
  copyable = true,
  muted = false,
  children,
}: {
  label: string;
  value?: string;
  copyable?: boolean;
  muted?: boolean;
  children?: ReactNode;
}) {
  const placeholder = isPlaceholderValue(value);
  return (
    <div className="flex items-center justify-between gap-3 border-b border-line py-2 last:border-b-0">
      <dt className="shrink-0 text-sm text-muted">{label}</dt>
      <dd className="flex min-w-0 items-center gap-1">
        {children ?? (
          <>
            <span
              className={cn(
                "truncate font-mono text-sm",
                muted || placeholder ? "text-muted" : "text-ink",
              )}
              title={value}
            >
              {placeholder ? "-" : value}
            </span>
            {copyable && !placeholder && !muted && (
              <CopyButton text={value ?? ""} className="size-7 shrink-0" />
            )}
          </>
        )}
      </dd>
    </div>
  );
}

export interface InfoListProps {
  data: DeviceInfoData | undefined;
}

export function InfoList({ data }: InfoListProps) {
  const { t } = useT("deviceinfo");
  const sim = simStatusView(data?.simStatus);
  const phoneMissing = isPlaceholderValue(data?.phoneNumber);
  return (
    <dl>
      <InfoRow label={t("manufacturer")} value={data?.manufacturer} copyable={false} />
      <InfoRow label={t("modelName")} value={data?.modelName} />
      <InfoRow label={t("firmwareVersion")} value={data?.firmwareVersion} />
      <InfoRow label={t("imei")} value={data?.imei} />
      <InfoRow label={t("imsi")} value={data?.imsi} />
      <InfoRow label={t("iccid")} value={data?.iccid} />
      <InfoRow
        label={t("phoneNumber")}
        value={data?.phoneNumber}
        muted={phoneMissing}
        copyable={!phoneMissing}
      >
        {phoneMissing ? (
          <span className="text-sm text-muted">{t("noPhoneNumberAvailable")}</span>
        ) : undefined}
      </InfoRow>
      <InfoRow label={t("simStatus")} value={data?.simStatus} copyable={false}>
        <StatusChip tone={sim.tone}>{t(sim.labelKey)}</StatusChip>
      </InfoRow>
      <InfoRow label={t("lanIp")} value={data?.lanIp} />
      <InfoRow label={t("wanIpv4")} value={data?.wwanIpv4} />
      <InfoRow label={t("wanIpv6")} value={data?.wwanIpv6} />
    </dl>
  );
}
