// LAN IP 卡(设计方案 §6.7⑥):网关(合法 IPv4)+ DHCP 起止末段(1-254,起止顺序、
// 网关同段且不入池,校验规则对齐旧版 setLANIP,见 lib.validateLanIpForm);
// 用户草稿在 status 回读时不被覆盖(对齐旧版 _lanIpDirty 语义),保存成功后复位。
// 布局:卡片独占整行,网关/起始/结束与保存按钮单行并排(窄屏自动换行)。
import { LoaderCircleIcon, NetworkIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { NetworkConfigResponse } from "@/lib/api";
import { useT } from "@/lib/i18n";

import type { useLanIpSave } from "../hooks";
import { validateLanIpForm } from "../lib";
import type { LanIpForm } from "../lib";

export interface LanIpCardProps {
  data: NetworkConfigResponse | undefined;
  ready: boolean;
  save: ReturnType<typeof useLanIpSave>;
}

export function LanIpCard({ data, ready, save }: LanIpCardProps) {
  const { t } = useT("netconfig");
  const [form, setForm] = useState<LanIpForm>({ gateway: "", start: "", end: "" });
  const dirtyRef = useRef(false);
  const serverRef = useRef<LanIpForm>({ gateway: "", start: "", end: "" });

  // 干净态才用服务端值填充;有未保存草稿时保留输入(程序填充与用户输入用值比对区分)
  useEffect(() => {
    if (!data || dirtyRef.current) return;
    const next: LanIpForm = {
      gateway: data.lanGwIp ?? "",
      start: data.lanIpStart ?? "",
      end: data.lanIpEnd ?? "",
    };
    serverRef.current = next;
    setForm(next);
  }, [data]);

  const onChange = (field: keyof LanIpForm, value: string) => {
    setForm((prev) => ({ ...prev, [field]: value }));
    if (value !== serverRef.current[field]) dirtyRef.current = true;
  };

  const onSave = () => {
    const result = validateLanIpForm(form);
    if (!result.valid) {
      toast.error(t(result.errorKey));
      return;
    }
    save.mutate(
      { startIp: result.startIp, endIp: result.endIp, gateway: result.gateway },
      {
        onSuccess: () => {
          // 保存成功后草稿即成为服务端值,复位脏标志允许后续回读正常覆盖
          dirtyRef.current = false;
        },
      },
    );
  };

  const disabled = !ready || save.isPending;
  return (
    <Panel domain="network" icon={NetworkIcon} title={t("lanIpSettings")}>
      {/* 整行卡单行表单:网关/起始/结束并排,保存按钮在结束地址右侧;窄屏换行堆叠 */}
      <div className="flex flex-wrap items-end gap-3">
        <label className="flex min-w-44 flex-1 flex-col gap-1.5 sm:max-w-xs">
          <span className="text-xs text-muted">{t("gatewayIpAddress")}</span>
          <Input
            value={form.gateway}
            disabled={disabled}
            onChange={(event) => onChange("gateway", event.target.value)}
            placeholder="192.168.5.1"
          />
        </label>
        <label className="flex w-24 flex-col gap-1.5 sm:w-28">
          <span className="text-xs text-muted">{t("startAddress")}</span>
          <Input
            value={form.start}
            disabled={disabled}
            inputMode="numeric"
            onChange={(event) => onChange("start", event.target.value)}
            placeholder="100"
          />
        </label>
        <label className="flex w-24 flex-col gap-1.5 sm:w-28">
          <span className="text-xs text-muted">{t("endAddress")}</span>
          <Input
            value={form.end}
            disabled={disabled}
            inputMode="numeric"
            onChange={(event) => onChange("end", event.target.value)}
            placeholder="200"
          />
        </label>
        <Button className="w-fit shrink-0" disabled={disabled} onClick={onSave}>
          {save.isPending && <LoaderCircleIcon className="animate-spin" />}
          {t("common:save")}
        </Button>
      </div>
    </Panel>
  );
}
