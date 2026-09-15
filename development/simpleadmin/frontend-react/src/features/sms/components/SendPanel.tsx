// 发送表单(设计方案 §6.9):号码归一化预览(IMSI MCC 补 +、≤6 位短号不补,口径对齐后端
// sms_number.go,发送时后端仍会再归一)、正文 UTF-16 码元计数 + 分段数实时显示(≤70/段);
// 发送 mutation:失败 toast + 表单内错误区显示 "+CMS ERROR 350 — 释义"(CMS 字典映射),
// 写操作失败保留表单值(§5.2)。
import { LoaderCircleIcon, SendIcon } from "lucide-react";

import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

import { describeSendFailure, useSendSms } from "../hooks";
import { normalizeNumberPreview, segmentCount, utf16Units } from "../lib";

export interface SendPanelProps {
  number: string;
  onNumberChange: (value: string) => void;
  message: string;
  onMessageChange: (value: string) => void;
  /** IMSI(device_info_data),供号码归一化预览 */
  imsi: string;
  /** 发送成功回调(页面级:强制刷新收件箱) */
  onSent: () => void;
}

export function SendPanel({
  number,
  onNumberChange,
  message,
  onMessageChange,
  imsi,
  onSent,
}: SendPanelProps) {
  const { t } = useT("sms");
  const send = useSendSms({
    onSent: () => {
      onNumberChange("");
      onMessageChange("");
      onSent();
    },
  });

  const preview = normalizeNumberPreview(number, imsi);
  const units = utf16Units(message);
  const segments = segmentCount(message);
  const canSend =
    number.trim() !== "" &&
    message.trim() !== "" &&
    preview.status !== "letters" &&
    !send.isPending;

  const submit = () => {
    if (!canSend) return;
    send.mutate({ number, message });
  };

  return (
    <Panel domain="comm" icon={SendIcon} title={t("sendMessage")}>
      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        {/* 整行卡下号码输入限宽(电话号码不需要通栏),正文保持全宽 */}
        <div className="flex flex-col gap-1.5 sm:max-w-sm">
          <Label htmlFor="sms-send-number">{t("recipientNumber")}</Label>
          <Input
            id="sms-send-number"
            className="font-mono"
            value={number}
            placeholder={t("enterRecipientNumber")}
            invalid={preview.status === "letters"}
            onChange={(event) => onNumberChange(event.target.value)}
          />
          {/* 号码归一化预览(纯前端提示;发送时后端按同一规则再归一) */}
          {preview.status === "letters" && (
            <p className="text-xs text-danger">{t("numberLettersError")}</p>
          )}
          {preview.status === "ready" && preview.value !== number.trim() && (
            <p className="text-xs text-muted">
              {t("numberPreviewValue", { value: preview.value })}
            </p>
          )}
          {preview.status === "short" && (
            <p className="text-xs text-muted">{t("shortNumberHint")}</p>
          )}
          {preview.status === "auto" && (
            <p className="text-xs text-muted">
              {t("numberPreviewValue", { value: preview.value })}
              {t("autoCountryCodeSuffix")}
            </p>
          )}
          {preview.status === "imsi-missing" && (
            <p className="text-xs text-warning">{t("imsiMissingHint")}</p>
          )}
          {preview.status === "mcc-missing" && (
            <p className="text-xs text-warning">{t("mccMissingHint", { mcc: preview.mcc })}</p>
          )}
        </div>

        <div className="flex flex-col gap-1.5">
          <div className="flex items-baseline justify-between gap-2">
            <Label htmlFor="sms-send-message">{t("messageContent")}</Label>
            {/* UTF-16 码元计数 + 分段数实时显示(每段 ≤70 码元,emoji 按代理对计 2) */}
            <span
              className={cn("text-xs tabular-nums", segments > 1 ? "text-warning" : "text-muted")}
            >
              {t("charCountInfo", { units, segments })}
            </span>
          </div>
          <Textarea
            id="sms-send-message"
            rows={4}
            value={message}
            placeholder={t("enterMessageContent")}
            onChange={(event) => onMessageChange(event.target.value)}
          />
        </div>

        {/* 发送失败错误区:CMS 错误码释义映射(设计方案 §6.9 新增点),保留表单值 */}
        {send.isError && (
          <p
            role="alert"
            className="rounded-md border border-danger/30 bg-danger-soft p-3 text-sm break-words text-danger"
          >
            {t("smsSendingFailedPrefix")}
            {describeSendFailure(send.error, (key) => t(key))}
          </p>
        )}

        <div className="flex justify-end">
          <Button type="submit" disabled={!canSend}>
            {send.isPending ? <LoaderCircleIcon className="animate-spin" /> : <SendIcon />}
            {t("sendSms")}
          </Button>
        </div>
      </form>
    </Panel>
  );
}
