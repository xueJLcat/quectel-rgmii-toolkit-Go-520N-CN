// Server酱 推送卡(https://sct.ftqq.com):开关 + SendKey(官方 SDK 口径自动识别
// SCT…=Turbo 与 sctp{uid}t…=Server酱³ 两种端点)。保存走 /api/set_sms_serverchan,
// 测试推送走 /api/test_sms_forward(channel=serverchan,用已保存配置同步发一条
// 样例消息,应答 code!=0 的原因经 ok:false+error 透传 toast)。
import { LoaderCircleIcon, SendHorizonalIcon, SendIcon } from "lucide-react";
import { useEffect, useState } from "react";

import { ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useT } from "@/lib/i18n";

import { useSmsForwardTest, useSmsServerChan } from "../hooks";
import { isServerChanKeyValid, SERVERCHAN_SITE_URL } from "../lib";

export function ServerChanPanel() {
  const { t } = useT("smsforward");
  const { query, mutation } = useSmsServerChan();
  const testPush = useSmsForwardTest();
  const config = query.data;

  const [enabled, setEnabled] = useState(false);
  const [sendKey, setSendKey] = useState("");
  useEffect(() => {
    if (!config) return;
    setEnabled(config.enabled === true);
    setSendKey(config.sendKey ?? "");
  }, [config]);

  const loaded = query.isSuccess;
  const keyInvalid = sendKey.trim() !== "" && !isServerChanKeyValid(sendKey);
  const keyRequired = enabled && sendKey.trim() === "";
  const busy = mutation.isPending || testPush.isPending;
  const canSave = loaded && !busy && !keyInvalid && !keyRequired;
  // 测试推送使用已保存配置:SendKey 未保存(与回读值不一致)时禁用。
  const canTest =
    loaded && !busy && sendKey === (config?.sendKey ?? "") && isServerChanKeyValid(config?.sendKey ?? "");

  const save = () => {
    if (!canSave) return;
    mutation.mutate({ enabled, sendKey: sendKey.trim() });
  };

  return (
    <Panel
      domain="comm"
      icon={SendIcon}
      title={t("serverChanTitle")}
      tools={
        loaded ? (
          <StatusChip tone={enabled ? "success" : "muted"}>
            {enabled ? t("enabled", { ns: "common" }) : t("disabled", { ns: "common" })}
          </StatusChip>
        ) : undefined
      }
      footer={t("serverChanFooterHint")}
    >
      {query.isError ? (
        <ErrorRetry message={t("serverChanLoadFailed")} onRetry={() => void query.refetch()} />
      ) : (
        <div className="flex flex-1 flex-col gap-4">
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="sms-serverchan-enabled">{t("serverChanTitle")}</Label>
            <Switch
              id="sms-serverchan-enabled"
              checked={enabled}
              disabled={!loaded || busy}
              onCheckedChange={setEnabled}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sms-serverchan-sendkey">{t("serverChanSendKey")}</Label>
            <Input
              id="sms-serverchan-sendkey"
              className="font-mono"
              value={sendKey}
              placeholder={t("serverChanSendKeyPlaceholder")}
              invalid={keyInvalid}
              disabled={!loaded || busy}
              onChange={(event) => setSendKey(event.target.value)}
            />
            {keyInvalid && <p className="text-xs text-danger">{t("serverChanSendKeyInvalid")}</p>}
            {keyRequired && loaded && (
              <p className="text-xs text-danger">{t("serverChanSendKeyRequired")}</p>
            )}
          </div>
          <p className="text-sm text-muted">
            {t("serverChanGetKeyHint")}{" "}
            <a
              href={SERVERCHAN_SITE_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="text-accent hover:underline"
            >
              sct.ftqq.com
            </a>
          </p>
          <div className="flex items-center justify-end gap-2">
            <Button
              variant="secondary"
              onClick={() => testPush.mutate("serverchan")}
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
      )}
    </Panel>
  );
}
