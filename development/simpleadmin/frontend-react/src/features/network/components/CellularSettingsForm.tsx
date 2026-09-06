// 蜂窝设置表单(设计方案 §6.4 ②):APN 输入 + PDP 类型下拉 + 首选网络 RadioGroup +
// NR5G 模式控制下拉;底部统一"保存更改"→ save_settings(载荷构建与守卫在 lib.ts
// buildSaveSettingsPayload:空 APN=仅改 PDP 语义保留、设置未就绪拒绝提交)。
// 占位/当前值展示对齐旧版 index.html:"当前：X"(common:currentValue)、未就绪"获取中...";
// 首选网络当前值经选项映射表转可读标签(AUTO→自动)。
import { LoaderCircleIcon, SaveIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useT } from "@/lib/i18n";

import type { CellularSettingsDraft, CellularSettingsSnapshot } from "../lib";

export interface CellularSettingsFormProps {
  snapshot: CellularSettingsSnapshot;
  draft: CellularSettingsDraft;
  onDraftChange: (patch: Partial<CellularSettingsDraft>) => void;
  onSave: () => void;
  saving: boolean;
  /** 设置读取失败:禁用保存(对齐旧版 disabled=settingsLoadFailed) */
  disabled: boolean;
}

const PDP_OPTIONS: { value: string; label: string }[] = [
  { value: "IPV4V6", label: "IPv4v6" },
  { value: "IP", label: "IPv4" },
  { value: "IPV6", label: "IPv6" },
];

const PREF_NETWORK_OPTIONS: { value: string; labelKey?: string; label?: string }[] = [
  { value: "AUTO", labelKey: "auto" },
  { value: "LTE", labelKey: "lteOnly" },
  { value: "LTE:NR5G", label: "NR5G-NSA" },
  { value: "NR5G", label: "NR5G-SA" },
];

const NR_MODE_OPTIONS: { value: string; labelKey: string }[] = [
  { value: "0", labelKey: "enableAll" },
  { value: "2", labelKey: "disableNr5gNsa" },
  { value: "1", labelKey: "disableNr5gSa" },
];

export function CellularSettingsForm({
  snapshot,
  draft,
  onDraftChange,
  onSave,
  saving,
  disabled,
}: CellularSettingsFormProps) {
  const { t } = useT("network");

  const nrCurrentOption = NR_MODE_OPTIONS.find(
    (option) => option.value === snapshot.nrModeControlCurrent,
  );
  const nrCurrentLabel =
    snapshot.nrModeControlCurrent === null
      ? t("common:fetching")
      : nrCurrentOption
        ? t(nrCurrentOption.labelKey)
        : snapshot.nrModeControlCurrent;

  // 当前首选网络与下方选项同一映射表:AUTO 显示"自动"等可读标签,未收录值原样回退
  const prefCurrentOption = PREF_NETWORK_OPTIONS.find(
    (option) => option.value === String(snapshot.prefNetwork ?? "").trim().toUpperCase(),
  );
  const prefCurrentLabel =
    snapshot.prefNetwork === "-"
      ? t("common:fetching")
      : t("common:currentValue", {
          value: prefCurrentOption
            ? prefCurrentOption.labelKey
              ? t(prefCurrentOption.labelKey)
              : (prefCurrentOption.label ?? prefCurrentOption.value)
            : snapshot.prefNetwork,
        });

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        onSave();
      }}
    >
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-2">
          <Label htmlFor="network-apn">APN</Label>
          <Input
            id="network-apn"
            type="text"
            autoComplete="off"
            placeholder={snapshot.apn === "-" ? t("common:fetching") : snapshot.apn}
            value={draft.newApn ?? ""}
            onChange={(event) => onDraftChange({ newApn: event.target.value })}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="network-pdp-type">{t("ipProtocolType")}</Label>
          <Select
            value={draft.newPdpType ?? undefined}
            onValueChange={(value) => onDraftChange({ newPdpType: value })}
          >
            <SelectTrigger id="network-pdp-type" className="w-full">
              <SelectValue
                placeholder={
                  snapshot.pdpType === "-"
                    ? t("common:fetching")
                    : t("common:currentValue", { value: snapshot.pdpType })
                }
              />
            </SelectTrigger>
            <SelectContent>
              {PDP_OPTIONS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <Label>{t("selectPreferredNetwork")}</Label>
        <p className="text-xs text-muted">{prefCurrentLabel}</p>
        {/* 无草稿时默认勾选与服务端当前值一致的选项(AUTO→"自动");draft 仍为 null,
            lib.buildSaveSettingsPayload 的"未改动不提交"守卫不受影响 */}
        <RadioGroup
          value={draft.prefNetworkMode ?? prefCurrentOption?.value ?? ""}
          onValueChange={(value) => onDraftChange({ prefNetworkMode: value })}
          className="grid grid-cols-2 gap-2 sm:grid-cols-4"
        >
          {PREF_NETWORK_OPTIONS.map((option) => (
            <Label
              key={option.value}
              htmlFor={`network-pref-${option.value}`}
              className="flex cursor-pointer items-center gap-2 rounded-sm border border-line bg-surface px-3 py-2 text-sm font-normal text-ink shadow-sm transition-colors hover:bg-surface-2 has-[[data-state=checked]]:border-accent has-[[data-state=checked]]:bg-accent-soft"
            >
              <RadioGroupItem id={`network-pref-${option.value}`} value={option.value} />
              {option.labelKey ? t(option.labelKey) : option.label}
            </Label>
          ))}
        </RadioGroup>
      </div>

      <div className="flex flex-col gap-2 sm:max-w-64">
        <Label htmlFor="network-nr-mode">{t("nr5gModeControl")}</Label>
        <Select
          value={draft.nrModeControlNew ?? undefined}
          onValueChange={(value) => onDraftChange({ nrModeControlNew: value })}
        >
          <SelectTrigger id="network-nr-mode" className="w-full">
            <SelectValue placeholder={t("common:currentValue", { value: nrCurrentLabel })} />
          </SelectTrigger>
          <SelectContent>
            {NR_MODE_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {t(option.labelKey)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="flex justify-end border-t border-line pt-3">
        <Button type="submit" disabled={disabled || saving}>
          {saving ? <LoaderCircleIcon className="animate-spin" /> : <SaveIcon />}
          {t("saveChanges")}
        </Button>
      </div>
    </form>
  );
}
