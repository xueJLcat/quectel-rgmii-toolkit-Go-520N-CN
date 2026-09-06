// IP 透传卡(设计方案 §6.7②):ETH/USB 模式选择 + 启停。启用/禁用路径不同——
// 启用直接下发(mode 必填,未指定 → toast);禁用走 danger 确认 + 后台重启流程
// (reboot store 倒计时遮罩 + ip_passthrough_result WS 事件收尾,见 hooks.useIpPassthrough)。
import { ArrowLeftRightIcon, LoaderCircleIcon } from "lucide-react";
import { useState } from "react";

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

import type { useIpPassthrough } from "../hooks";

export interface IpPassthroughCardProps {
  ipPassStatus: boolean;
  /** status 已加载(未加载时控件禁用,对齐旧版 !statusLoaded) */
  ready: boolean;
  actions: ReturnType<typeof useIpPassthrough>;
}

export function IpPassthroughCard({ ipPassStatus, ready, actions }: IpPassthroughCardProps) {
  const { t } = useT("netconfig");
  const [mode, setMode] = useState("");
  const busy = actions.enable.isPending || actions.disable.isPending;
  return (
    <Panel domain="network" icon={ArrowLeftRightIcon} title={t("ipPassthrough")}>
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted">
          {t("common:currentValue", {
            value: ipPassStatus ? t("common:enabled") : t("common:notEnabled"),
          })}
        </p>
        {!ipPassStatus && (
          <Select value={mode} onValueChange={setMode} disabled={busy || !ready}>
            <SelectTrigger aria-label={t("ipPassthrough")} className="w-full">
              <SelectValue placeholder={t("unspecified")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="ETH">ETH</SelectItem>
              <SelectItem value="USB">{t("usbEcmRndis")}</SelectItem>
            </SelectContent>
          </Select>
        )}
        <div className="flex gap-2">
          {ipPassStatus ? (
            <Button
              variant="danger"
              disabled={busy || !ready}
              onClick={() => actions.disable.mutate()}
            >
              {actions.disable.isPending && <LoaderCircleIcon className="animate-spin" />}
              {t("common:disable")}
            </Button>
          ) : (
            <Button disabled={busy || !ready} onClick={() => actions.enable.mutate(mode)}>
              {actions.enable.isPending && <LoaderCircleIcon className="animate-spin" />}
              {t("common:enable")}
            </Button>
          )}
        </div>
      </div>
    </Panel>
  );
}
