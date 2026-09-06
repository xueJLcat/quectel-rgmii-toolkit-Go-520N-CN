// USB 网卡模式卡(设计方案 §6.7③):RMNET/ECM/MBIM/RNDIS 切换需 confirm;
// 响应带重启契约(reboot/rebootCountdownSeconds)时启动倒计时遮罩;成功置"待重启生效"
// amber pulse chip(状态回读到目标模式后自动清除,可手动关闭)。
import { LoaderCircleIcon, UsbIcon } from "lucide-react";
import { useState } from "react";

import { StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useT } from "@/lib/i18n";

import type { useUsbNet } from "../hooks";
import { usbNetModeText } from "../lib";

export interface UsbNetCardProps {
  currentMode: string | undefined;
  ready: boolean;
  usb: ReturnType<typeof useUsbNet>;
}

export function UsbNetCard({ currentMode, ready, usb }: UsbNetCardProps) {
  const { t } = useT("netconfig");
  const [mode, setMode] = useState("");
  return (
    <Panel domain="network" icon={UsbIcon} title={t("usbProtocol")}>
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted">
          {t("common:currentValue", {
            value: usbNetModeText(currentMode, t("unknownMode")),
          })}
        </p>
        <Select value={mode} onValueChange={setMode} disabled={usb.mutation.isPending || !ready}>
          <SelectTrigger aria-label={t("usbProtocol")} className="w-full">
            <SelectValue placeholder={t("unspecified")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="RMNET">RMNET</SelectItem>
            <SelectItem value="ECM">{t("ecmRecommended")}</SelectItem>
            <SelectItem value="MBIM">MBIM</SelectItem>
            <SelectItem value="RNDIS">RNDIS</SelectItem>
          </SelectContent>
        </Select>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            disabled={usb.mutation.isPending || !ready}
            onClick={() => usb.mutation.mutate(mode)}
          >
            {usb.mutation.isPending && <LoaderCircleIcon className="animate-spin" />}
            {t("change")}
          </Button>
          {usb.pendingMode && (
            <StatusChip tone="warning" pulse>
              {t("pendingRebootToTakeEffect")}
            </StatusChip>
          )}
        </div>
      </div>
    </Panel>
  );
}
