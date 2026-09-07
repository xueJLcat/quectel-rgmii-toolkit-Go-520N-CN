// 短信转发 Webhook 卡(自 sms 页迁入本页并扩展参数):开关 + URL 校验(http/https)+
// 请求方式(POST/GET/PUT)+ 超时(1-120s)+ 自定义请求头(每行 Name: Value)+
// JSON 载荷模板(占位符 {sender}/{date}/{text}/{index}/{storage})+ 最近转发状态
// (lastNotifiedIndex)。保存走 /api/set_sms_webhook,测试推送走 /api/test_sms_forward
// (channel=webhook,用已保存配置同步发送一条样例消息,表单未保存时禁用避免口径错位)。
import { LoaderCircleIcon, SendHorizonalIcon, WebhookIcon } from "lucide-react";
import { useEffect, useState } from "react";

import { ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useT } from "@/lib/i18n";

import { useSmsForwardTest, useSmsWebhook } from "../hooks";
import {
  findInvalidHeaderLine,
  isWebhookTemplateValid,
  isWebhookTimeoutValid,
  isWebhookUrlValid,
  normalizeWebhookMethod,
  WEBHOOK_METHODS,
  WEBHOOK_TIMEOUT_RANGE,
} from "../lib";

export function WebhookPanel() {
  const { t } = useT("smsforward");
  const { query, mutation } = useSmsWebhook();
  const testPush = useSmsForwardTest();
  const config = query.data;

  const [enabled, setEnabled] = useState(false);
  const [url, setUrl] = useState("");
  const [method, setMethod] = useState("POST");
  const [timeoutSec, setTimeoutSec] = useState("10");
  const [headers, setHeaders] = useState("");
  const [template, setTemplate] = useState("");
  // 配置回读后同步表单(保存后 invalidate 回读同样走这里)
  useEffect(() => {
    if (!config) return;
    setEnabled(config.enabled === true);
    setUrl(config.url ?? "");
    setMethod(normalizeWebhookMethod(config.method));
    setTimeoutSec(String(Number(config.timeoutSec) > 0 ? Number(config.timeoutSec) : 10));
    setHeaders(config.headers ?? "");
    setTemplate(config.template ?? "");
  }, [config]);

  const loaded = query.isSuccess;
  const urlInvalid = url.trim() !== "" && !isWebhookUrlValid(url);
  const urlRequired = enabled && url.trim() === "";
  const timeoutInvalid = timeoutSec.trim() !== "" && !isWebhookTimeoutValid(timeoutSec);
  const badHeaderLine = findInvalidHeaderLine(headers);
  const templateInvalid = !isWebhookTemplateValid(template);
  const busy = mutation.isPending || testPush.isPending;
  const canSave =
    loaded && !busy && !urlInvalid && !urlRequired && !timeoutInvalid && badHeaderLine === null && !templateInvalid;
  // 测试推送使用已保存配置:表单与已保存值不一致(未保存)时禁用,防止
  // "测试通过的地址"与实际保存的地址不是同一个。
  const dirty =
    !config ||
    url !== (config.url ?? "") ||
    method !== normalizeWebhookMethod(config.method) ||
    timeoutSec !== String(Number(config.timeoutSec) > 0 ? Number(config.timeoutSec) : 10) ||
    headers !== (config.headers ?? "") ||
    template !== (config.template ?? "");
  const canTest = loaded && !busy && !dirty && isWebhookUrlValid(config?.url ?? "");

  const save = () => {
    if (!canSave) return;
    mutation.mutate({ enabled, url: url.trim(), method, headers, timeoutSec: timeoutSec.trim(), template });
  };

  return (
    <Panel
      domain="comm"
      icon={WebhookIcon}
      title={t("forwardNewSmsToWebhook")}
      tools={
        loaded ? (
          <StatusChip tone={enabled ? "success" : "muted"}>
            {enabled ? t("enabled", { ns: "common" }) : t("disabled", { ns: "common" })}
          </StatusChip>
        ) : undefined
      }
      footer={t("webhookFooterHint")}
    >
      {query.isError ? (
        <ErrorRetry message={t("webhookLoadFailed")} onRetry={() => void query.refetch()} />
      ) : (
        <div className="flex flex-1 flex-col gap-4">
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="sms-webhook-enabled">{t("forwardNewSmsToWebhook")}</Label>
            <Switch
              id="sms-webhook-enabled"
              checked={enabled}
              disabled={!loaded || busy}
              onCheckedChange={setEnabled}
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="sms-webhook-method">{t("webhookMethod")}</Label>
              <Select value={method} onValueChange={setMethod} disabled={!loaded || busy}>
                <SelectTrigger id="sms-webhook-method" aria-label={t("webhookMethod")}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {WEBHOOK_METHODS.map((item) => (
                    <SelectItem key={item} value={item}>
                      {item}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="sms-webhook-timeout">{t("webhookTimeoutSec")}</Label>
              <Input
                id="sms-webhook-timeout"
                type="number"
                min={WEBHOOK_TIMEOUT_RANGE.min}
                max={WEBHOOK_TIMEOUT_RANGE.max}
                value={timeoutSec}
                invalid={timeoutInvalid}
                disabled={!loaded || busy}
                onChange={(event) => setTimeoutSec(event.target.value)}
              />
              {timeoutInvalid && <p className="text-xs text-danger">{t("webhookTimeoutInvalid")}</p>}
            </div>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sms-webhook-url">{t("webhookUrl")}</Label>
            <Input
              id="sms-webhook-url"
              className="font-mono"
              value={url}
              placeholder={t("webhookUrlPlaceholder")}
              invalid={urlInvalid}
              disabled={!loaded || busy}
              onChange={(event) => setUrl(event.target.value)}
            />
            {urlInvalid && <p className="text-xs text-danger">{t("webhookUrlInvalid")}</p>}
            {urlRequired && loaded && <p className="text-xs text-danger">{t("webhookUrlRequired")}</p>}
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sms-webhook-headers">{t("webhookHeaders")}</Label>
            <Textarea
              id="sms-webhook-headers"
              rows={2}
              className="font-mono"
              placeholder={t("webhookHeadersPlaceholder")}
              invalid={badHeaderLine !== null}
              value={headers}
              disabled={!loaded || busy}
              onChange={(event) => setHeaders(event.target.value)}
            />
            {badHeaderLine !== null && (
              <p className="text-xs text-danger">{t("webhookHeadersInvalid", { line: badHeaderLine })}</p>
            )}
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sms-webhook-template">{t("webhookTemplate")}</Label>
            <Textarea
              id="sms-webhook-template"
              rows={3}
              className="font-mono"
              placeholder={t("webhookTemplatePlaceholder")}
              invalid={templateInvalid}
              value={template}
              disabled={!loaded || busy}
              onChange={(event) => setTemplate(event.target.value)}
            />
            {templateInvalid ? (
              <p className="text-xs text-danger">{t("webhookTemplateInvalid")}</p>
            ) : (
              <p className="text-xs text-muted">{t("webhookTemplateHint")}</p>
            )}
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2">
            {/* 最近转发状态:lastNotifiedIndex=-1 表示尚未转发过高水位索引 */}
            <span className="text-sm text-muted">
              {typeof config?.lastNotifiedIndex === "number" && config.lastNotifiedIndex >= 0
                ? t("webhookLastNotified", { index: config.lastNotifiedIndex })
                : t("webhookNeverNotified")}
            </span>
            <div className="flex items-center gap-2">
              <Button
                variant="secondary"
                onClick={() => testPush.mutate("webhook")}
                disabled={!canTest}
              >
                {testPush.isPending ? (
                  <LoaderCircleIcon className="animate-spin" />
                ) : (
                  <SendHorizonalIcon />
                )}
                {t("testSend")}
              </Button>
              <Button onClick={save} disabled={!canSave}>
                {mutation.isPending && <LoaderCircleIcon className="animate-spin" />}
                {t("save", { ns: "common" })}
              </Button>
            </div>
          </div>
        </div>
      )}
    </Panel>
  );
}
