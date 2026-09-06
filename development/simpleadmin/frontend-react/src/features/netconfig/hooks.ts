// 网络设置页数据层(设计方案 §6.7):status 查询(pending/全默认值退避重试,对齐旧版
// fetchStatus 8 次 × 1500ms×min(n,4) 节奏)+ 各写操作 mutation(in-flight 由 isPending 禁用、
// 成功/失败 toast、成功 invalidate status)。IP 透传禁用为后台重启流程:
// 确认 → 立即启动倒计时(网口可能随时断开,不依赖响应到达)→ 响应按 reboot 契约重置倒计时 →
// onServerEvent("ip_passthrough_result") 收尾(卸载取消订阅),对齐旧版 netconfig.js init 监听。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryResult } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { gateway, networkConfigData } from "@/lib/api";
import type { NetworkConfigResponse } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";
import { REBOOT_DEFAULT_SECONDS, useRebootStore } from "@/stores/reboot";

import {
  NetconfigPendingError,
  ensureOk,
  ipPassthroughDisableParams,
  ipPassthroughEnableParams,
  isUntrustedStatus,
  readRebootNotice,
  usbNetParams,
} from "./lib";

export const STATUS_QUERY_KEY = ["netconfig", "status"] as const;
export const MAC_BIND_QUERY_KEY = ["netconfig", "mac-bind"] as const;
export const DNS_UPSTREAM_QUERY_KEY = ["netconfig", "dns-upstream"] as const;

export interface NetconfigStatus {
  query: UseQueryResult<NetworkConfigResponse, Error>;
  data: NetworkConfigResponse | undefined;
  /** 首次加载或 pending 退避重试进行中(保留旧值 + "获取中" chip) */
  pending: boolean;
  /** 重试耗尽/运行时错误(ErrorRetry 入口) */
  failed: boolean;
}

export function useNetconfigStatus(): NetconfigStatus {
  // 曾加载成功的可信数据:后续"全默认值"响应沿用旧值(对齐旧版 wasLoaded 分支)
  const trustedRef = useRef<NetworkConfigResponse | null>(null);
  const query = useQuery({
    queryKey: STATUS_QUERY_KEY,
    queryFn: async (): Promise<NetworkConfigResponse> => {
      const data = await networkConfigData("status", { force: "1" });
      // 后端读取失败返回 error 字段,语义同保护期,走退避重试
      if (!data || data.pending === true || data.error) throw new NetconfigPendingError();
      if (isUntrustedStatus(data)) {
        if (trustedRef.current) return trustedRef.current;
        throw new NetconfigPendingError();
      }
      trustedRef.current = data;
      return data;
    },
    retry: (failureCount, error) => error instanceof NetconfigPendingError && failureCount < 8,
    retryDelay: (attempt) => 1500 * Math.min(attempt + 1, 4),
    refetchOnMount: "always",
    staleTime: 0,
  });
  const pending = query.isPending || (query.isError && query.isFetching);
  const failed = query.isError && !query.isFetching;
  return { query, data: query.data, pending, failed };
}

export function isNetconfigPendingError(error: unknown): boolean {
  return error instanceof NetconfigPendingError;
}

/**
 * IP 透传启停。启用/禁用路径不同:
 * - 启用:参数带 mode,成功 toast + invalidate status;
 * - 禁用:danger 确认 → 后台重启流程(reboot store 倒计时遮罩 + WS 事件收尾)。
 */
export function useIpPassthrough() {
  const { t } = useT("netconfig");
  const queryClient = useQueryClient();
  const confirm = useConfirmStore((state) => state.confirm);
  const startCountdown = useRebootStore((state) => state.startCountdown);
  const closeCountdown = useRebootStore((state) => state.closeCountdown);
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // WS 事件收尾(对齐旧版 ip_passthrough_result 监听):ok="false" → 失败 toast + 取消倒计时;
  // ok="true" → 成功 toast(倒计时遮罩继续走完,由 RebootOverlay 提供关闭)。
  useEffect(() => {
    return gateway.onServerEvent("ip_passthrough_result", (message) => {
      const ok = String(message.data?.ok ?? "");
      if (ok === "false") {
        toast.error(t("operationFailedFollowUpStepsCancelled"));
        closeCountdown();
      } else if (ok === "true") {
        toast.success(t("ipPassthroughDisableOk"));
      }
    });
  }, [closeCountdown, t]);

  useEffect(
    () => () => {
      if (refreshTimerRef.current !== null) clearTimeout(refreshTimerRef.current);
    },
    [],
  );

  // 重启完成后回读状态(对齐旧版 refreshAfterReboot:倒计时结束 + 5s)
  const scheduleStatusRefresh = useCallback(
    (countdownSeconds: number) => {
      if (refreshTimerRef.current !== null) clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = setTimeout(
        () => {
          refreshTimerRef.current = null;
          void queryClient.invalidateQueries({ queryKey: STATUS_QUERY_KEY });
        },
        (countdownSeconds + 5) * 1000,
      );
    },
    [queryClient],
  );

  const enable = useMutation({
    mutationFn: async (mode: string) => {
      const params = ipPassthroughEnableParams(mode);
      if (!params) throw new Error(t("ipPassthroughModeIsNotSpecified"));
      return ensureOk(
        await networkConfigData("ip_passthrough", params),
        t("common:operationFailed"),
      );
    },
    onSuccess: () => {
      toast.success(t("ipPassthroughEnabled"));
      void queryClient.invalidateQueries({ queryKey: STATUS_QUERY_KEY });
    },
    onError: (error) => {
      toast.error(error.message || t("common:unknownError"));
    },
  });

  const disable = useMutation({
    mutationFn: async (): Promise<"cancelled" | "rebooting" | "failed"> => {
      const confirmed = await confirm({
        title: t("disableIpPassthrough"),
        message: t("disableIpPassthroughConfirm"),
        confirmText: t("common:disable"),
        danger: true,
      });
      if (!confirmed) return "cancelled";
      // 立即启动倒计时(对齐旧版:禁用期间网口/WS 随时断开,不依赖响应到达)
      startCountdown({ seconds: REBOOT_DEFAULT_SECONDS });
      toast.info(
        t("disablingIpPassthroughTheNetworkPortWillRestartPleaseWaitForTheCountdownToFinish"),
      );
      try {
        const data = await networkConfigData("ip_passthrough", ipPassthroughDisableParams());
        if (data && data.ok === false) {
          toast.error(data.error || t("common:operationFailed"));
          closeCountdown();
          return "failed";
        }
        const notice = readRebootNotice(data);
        const seconds = notice?.countdownSeconds ?? REBOOT_DEFAULT_SECONDS;
        if (notice) startCountdown({ seconds });
        scheduleStatusRefresh(seconds);
        return "rebooting";
      } catch {
        // 网口/WebSocket 断开是常态:保持倒计时,收尾交给 ip_passthrough_result 事件
        scheduleStatusRefresh(REBOOT_DEFAULT_SECONDS);
        return "rebooting";
      }
    },
  });

  return { enable, disable };
}

/**
 * USB 网卡模式切换:确认 → usbnet 下发 → 响应若带重启契约则启动倒计时;
 * 成功置"待重启生效"(amber pulse chip),状态回读到目标模式后自动清除。
 */
export function useUsbNet(statusData: NetworkConfigResponse | undefined) {
  const { t } = useT("netconfig");
  const queryClient = useQueryClient();
  const confirm = useConfirmStore((state) => state.confirm);
  const startCountdown = useRebootStore((state) => state.startCountdown);
  const [pendingMode, setPendingMode] = useState<string | null>(null);

  useEffect(() => {
    const current = statusData?.currentUsbNetMode;
    if (pendingMode && current && current === pendingMode) setPendingMode(null);
  }, [statusData, pendingMode]);

  const mutation = useMutation({
    mutationFn: async (mode: string): Promise<string | null> => {
      const params = usbNetParams(mode);
      if (!params) throw new Error(t("usbNetworkModeIsNotSpecified"));
      const confirmed = await confirm({
        title: t("switchUsbNetMode"),
        message: t("switchUsbNetModeConfirm"),
        confirmText: t("common:apply"),
      });
      if (!confirmed) return null;
      const data = await networkConfigData("usbnet", params);
      ensureOk(data, t("common:operationFailed"));
      const notice = readRebootNotice(data);
      if (notice) startCountdown({ seconds: notice.countdownSeconds });
      return params.mode;
    },
    onSuccess: (mode) => {
      if (!mode) return;
      toast.success(t("usbNetModeSwitched"));
      setPendingMode(mode);
      void queryClient.invalidateQueries({ queryKey: STATUS_QUERY_KEY });
    },
    onError: (error) => {
      toast.error(error.message || t("common:unknownError"));
    },
  });

  const dismissPending = useCallback(() => setPendingMode(null), []);
  return { mutation, pendingMode, dismissPending };
}

/** DNS 代理开关(v4/v6):即时生效 + toast,成功回读 status。 */
export function useDnsProxy() {
  const { t } = useT("netconfig");
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ family, enabled }: { family: "4" | "6"; enabled: boolean }) =>
      ensureOk(
        await networkConfigData("dns_proxy", { family, enabled: enabled ? "1" : "0" }),
        t("common:operationFailed"),
      ),
    onSuccess: () => {
      toast.success(t("dnsProxySettingSubmitted"));
      void queryClient.invalidateQueries({ queryKey: STATUS_QUERY_KEY });
    },
    onError: (error) => {
      toast.error(error.message || t("common:unknownError"));
    },
  });
}

/** 上游 DNS 读取:{enabled, servers};ok:false/HTTP 错 → ErrorRetry 语义。 */
export function useDnsUpstreamQuery() {
  const { t } = useT("netconfig");
  return useQuery({
    queryKey: DNS_UPSTREAM_QUERY_KEY,
    queryFn: async () => {
      const data = await networkConfigData("dns_upstream");
      if (!data || data.ok === false) {
        throw new Error(data?.error || t("common:failedToLoadStatus"));
      }
      return { enabled: data.enabled === true, servers: data.servers ?? [] };
    },
    refetchOnMount: "always",
    staleTime: 0,
    retry: false,
  });
}

/** 上游 DNS 保存(启用自定义)/恢复运营商:成功 toast + 回读。 */
export function useDnsUpstreamSave() {
  const { t } = useT("netconfig");
  const queryClient = useQueryClient();
  const save = useMutation({
    mutationFn: async (servers: string[]) =>
      ensureOk(
        await networkConfigData("dns_upstream_set", { enabled: "1", servers: servers.join(",") }),
        t("common:operationFailed"),
      ),
    onSuccess: () => {
      toast.success(t("customUpstreamDnsSaved"));
      void queryClient.invalidateQueries({ queryKey: DNS_UPSTREAM_QUERY_KEY });
    },
    onError: (error) => {
      toast.error(error.message || t("common:unknownError"));
    },
  });
  const restore = useMutation({
    mutationFn: async () =>
      ensureOk(
        await networkConfigData("dns_upstream_set", { enabled: "0" }),
        t("common:operationFailed"),
      ),
    onSuccess: () => {
      toast.success(t("carrierDnsRestored"));
      void queryClient.invalidateQueries({ queryKey: DNS_UPSTREAM_QUERY_KEY });
    },
    onError: (error) => {
      toast.error(error.message || t("common:unknownError"));
    },
  });
  return { save, restore };
}

/** LAN IP 保存:成功 toast + invalidate status(表单草稿复位由调用方 onSuccess 处理)。 */
export function useLanIpSave() {
  const { t } = useT("netconfig");
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (form: { startIp: string; endIp: string; gateway: string }) =>
      ensureOk(
        await networkConfigData("lanip", {
          start: form.startIp,
          end: form.endIp,
          gateway: form.gateway,
        }),
        t("common:operationFailed"),
      ),
    onSuccess: () => {
      toast.success(t("common:saved"));
      void queryClient.invalidateQueries({ queryKey: STATUS_QUERY_KEY });
    },
    onError: (error) => {
      toast.error(error.message || t("common:unknownError"));
    },
  });
}

/** 静态绑定列表:AT 未就绪(ok:false/atReady:false)按失败处理,绝不把空列表当真实状态。 */
export function useMacBindQuery() {
  const { t } = useT("netconfig");
  return useQuery({
    queryKey: MAC_BIND_QUERY_KEY,
    queryFn: async () => {
      const data = await networkConfigData("mac_bind_list");
      if (!data || data.ok === false || data.atReady === false) {
        throw new Error(data?.error || t("common:failedToLoadStatus"));
      }
      return data.bindings ?? [];
    },
    refetchOnMount: "always",
    staleTime: 0,
    retry: false,
  });
}

/** 静态绑定增删:即时生效,成功 toast + 回读列表。 */
export function useMacBindMutations() {
  const { t } = useT("netconfig");
  const queryClient = useQueryClient();
  const add = useMutation({
    mutationFn: async ({ mac, ip }: { mac: string; ip: string }) =>
      ensureOk(await networkConfigData("mac_bind_set", { mac, ip }), t("common:operationFailed")),
    onSuccess: () => {
      toast.success(t("staticBindingAdded"));
      void queryClient.invalidateQueries({ queryKey: MAC_BIND_QUERY_KEY });
    },
    onError: (error) => {
      toast.error(error.message || t("common:unknownError"));
    },
  });
  const remove = useMutation({
    mutationFn: async (mac: string) =>
      ensureOk(await networkConfigData("mac_bind_del", { mac }), t("common:operationFailed")),
    onSuccess: () => {
      toast.success(t("staticBindingDeleted"));
      void queryClient.invalidateQueries({ queryKey: MAC_BIND_QUERY_KEY });
    },
    onError: (error) => {
      toast.error(error.message || t("common:unknownError"));
    },
  });
  return { add, remove };
}
