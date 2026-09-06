// 短信转发 Webhook 卡(设计方案 §6.9,从自动化页迁入):开关 + URL 校验(http/https)+
// 最近转发状态(lastNotifiedIndex)。保存走 /api/set_sms_webhook(enabled=1/0&url=),
// 保存后回读同步(契约与旧版 automation.js saveWebhook 一致)。
import { WebhookIcon } from "lucide-react";
import { useEffect, useState } from "react";

import { ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useT } from "@/lib/i18n";

import { useSmsWebhook } from "../hooks";

/** URL 校验:仅接受 http/https 回调地址(旧版 placeholder 口径)。 */
export function isWebhookUrlValid(url: string): boolean {
  return /^https?:\/\/\S+$/i.test(url.trim());
}

export function WebhookPanel() {
  const { t } = useT("sms");
  const { query, mutation } = useSmsWebhook();
  const config = query.data;

  const [enabled, setEnabled] = useState(false);
  const [url, setUrl] = useState("");
  // 配置回读后同步表单(保存后 invalidate 回读同样走这里)
  useEffect(() => {
    if (!config) return;
    setEnabled(config.enabled === true);
    setUrl(config.url ?? "");
  }, [config]);

  const loaded = query.isSuccess;
  const urlInvalid = url.trim() !== "" && !isWebhookUrlValid(url);
  const canSave = loaded && !mutation.isPending && !urlInvalid && !(enabled && url.trim() === "");

  const save = () => {
    if (!canSave) return;
    mutation.mutate({ enabled, url: url.trim() });
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
        // 卡片被网格拉高时:开关行贴顶、URL 组居中、状态+保存行贴底
        <div className="flex flex-1 flex-col justify-between gap-4">
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="sms-webhook-enabled">{t("forwardNewSmsToWebhook")}</Label>
            <Switch
              id="sms-webhook-enabled"
              checked={enabled}
              disabled={!loaded || mutation.isPending}
              onCheckedChange={setEnabled}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sms-webhook-url">{t("webhookUrl")}</Label>
            <Input
              id="sms-webhook-url"
              className="font-mono"
              value={url}
              placeholder={t("webhookUrlPlaceholder")}
              invalid={urlInvalid}
              disabled={!loaded || mutation.isPending}
              onChange={(event) => setUrl(event.target.value)}
            />
            {urlInvalid && <p className="text-xs text-danger">{t("webhookUrlInvalid")}</p>}
            {enabled && url.trim() === "" && loaded && (
              <p className="text-xs text-danger">{t("webhookUrlRequired")}</p>
            )}
          </div>
          <div className="flex items-center justify-between gap-2">
            {/* 最近转发状态:lastNotifiedIndex=-1 表示尚未转发过高水位索引 */}
            <span className="text-sm text-muted">
              {typeof config?.lastNotifiedIndex === "number" && config.lastNotifiedIndex >= 0
                ? t("webhookLastNotified", { index: config.lastNotifiedIndex })
                : t("webhookNeverNotified")}
            </span>
            <Button onClick={save} disabled={!canSave}>
              {t("save", { ns: "common" })}
            </Button>
          </div>
        </div>
      )}
    </Panel>
  );
}
