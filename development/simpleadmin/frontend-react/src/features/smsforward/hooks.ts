// 短信转发页数据 hooks:
// - useSmsWebhook:Webhook 通道配置 get/set(自 sms 页迁入并扩展 method/headers/
//   timeoutSec/template,保存后 invalidate 回读同步);
// - useSmsServerChan:Server酱 通道配置 get/set(契约与 webhook 同构);
// - useSmsForwardTest:按当前已保存配置同步发送一条测试消息,toast 展示结果。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import {
  smsForwardTest,
  smsServerChanGet,
  smsServerChanSet,
  smsWebhookGet,
  smsWebhookSet,
} from "@/lib/api";
import { useT } from "@/lib/i18n";

export interface SmsWebhookForm {
  enabled: boolean;
  url: string;
  method: string;
  headers: string;
  timeoutSec: string;
  template: string;
}

export function useSmsWebhook() {
  const { t } = useT("smsforward");
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ["smsWebhook"], queryFn: () => smsWebhookGet(), retry: 1 });
  const mutation = useMutation({
    mutationFn: (vars: SmsWebhookForm) =>
      smsWebhookSet({
        enabled: vars.enabled ? "1" : "0",
        url: vars.url,
        method: vars.method,
        headers: vars.headers,
        timeoutSec: vars.timeoutSec,
        template: vars.template,
      }),
    onSuccess: (data) => {
      if (data.ok === false) {
        toast.error(data.error ?? t("saveFailed", { ns: "common" }));
      } else {
        toast.success(t("saved", { ns: "common" }));
      }
      void queryClient.invalidateQueries({ queryKey: ["smsWebhook"] });
    },
    onError: () => {
      toast.error(t("saveFailed", { ns: "common" }));
    },
  });
  return { query, mutation };
}

export interface SmsServerChanForm {
  enabled: boolean;
  sendKey: string;
}

export function useSmsServerChan() {
  const { t } = useT("smsforward");
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["smsServerChan"],
    queryFn: () => smsServerChanGet(),
    retry: 1,
  });
  const mutation = useMutation({
    mutationFn: (vars: SmsServerChanForm) =>
      smsServerChanSet({ enabled: vars.enabled ? "1" : "0", sendKey: vars.sendKey }),
    onSuccess: (data) => {
      if (data.ok === false) {
        toast.error(data.error ?? t("saveFailed", { ns: "common" }));
      } else {
        toast.success(t("saved", { ns: "common" }));
      }
      void queryClient.invalidateQueries({ queryKey: ["smsServerChan"] });
    },
    onError: () => {
      toast.error(t("saveFailed", { ns: "common" }));
    },
  });
  return { query, mutation };
}

export type SmsForwardTestChannel = "webhook" | "serverchan";

export function useSmsForwardTest() {
  const { t } = useT("smsforward");
  return useMutation({
    mutationFn: (channel: SmsForwardTestChannel) => smsForwardTest(channel),
    onSuccess: (data) => {
      if (data.ok === false) {
        toast.error(
          data.error ? `${t("testSendFailed")}: ${data.error}` : t("testSendFailed"),
        );
      } else {
        toast.success(t("testSendSuccess"));
      }
    },
    onError: () => {
      toast.error(t("testSendFailed"));
    },
  });
}
