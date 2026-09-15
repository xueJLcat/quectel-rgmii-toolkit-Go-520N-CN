// firewall 数据层:status/status6/fwd_list/dmz 查询 + save/fwd_save/dmz 变更。
// 语义对齐旧 www/js/pages/firewall.js:失败不自动重试(手动 ErrorRetry 入口);
// DMZ status 的 pending/不可信数据经 PendingStatusError 走 8 次退避重试(1500×min(n,4)ms,对齐旧实现)。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { firewallData, networkConfigData } from "@/lib/api";
import type { FirewallChainInfo, FirewallStatus, OkResponse } from "@/lib/api";

import { PendingStatusError, buildSaveParams, fwdSaveParam, isUntrustedDmzStatus } from "./lib";
import type { FwdRule, PortRule } from "./lib";

export const FIREWALL_STATUS_KEY = ["firewall", "status"] as const;
export const FIREWALL_STATUS6_KEY = ["firewall", "status6"] as const;
export const FIREWALL_FWD_KEY = ["firewall", "fwd"] as const;
export const FIREWALL_DMZ_KEY = ["firewall", "dmz"] as const;

/** status6 响应:{ok:true, chains} 或 {ok:false, error}(200,ip6tables 缺失等设备侧失败)。 */
export interface Status6Response {
  ok?: boolean;
  error?: string;
  chains?: FirewallChainInfo[];
}

/** fwd_list 响应:{ok:true, forwarded, jumpInstalled} 或 {ok:false, error}。 */
export interface FwdListResponse {
  ok?: boolean;
  error?: string;
  forwarded?: unknown[];
  jumpInstalled?: boolean;
}

export function useFirewallStatus() {
  return useQuery({
    queryKey: FIREWALL_STATUS_KEY,
    queryFn: () => firewallData<FirewallStatus>("status"),
    // 对齐旧版:单次拉取,失败显示 ErrorRetry 手动重试,不做后台自动重试
    retry: false,
  });
}

export function useFirewallStatus6() {
  return useQuery({
    queryKey: FIREWALL_STATUS6_KEY,
    queryFn: async () => {
      const data = await firewallData<Status6Response>("status6");
      // 设备侧失败(ip6tables 缺失等)如实转 error 态,绝不伪装成空链列表
      if (data?.ok === false) throw new Error(data.error ?? "");
      return data;
    },
    retry: false,
  });
}

export function useFwdList() {
  return useQuery({
    queryKey: FIREWALL_FWD_KEY,
    queryFn: async () => {
      const data = await firewallData<FwdListResponse>("fwd_list");
      if (data?.ok === false) throw new Error(data.error ?? "");
      return data;
    },
    retry: false,
  });
}

export function useDmzStatus() {
  return useQuery({
    queryKey: FIREWALL_DMZ_KEY,
    queryFn: async () => {
      const data = await networkConfigData("status", { force: "1" });
      // 开机保护期 pending / 全默认值不可信 → 抛出可重试错误,保留上次数据走退避重试
      if (data?.pending === true || isUntrustedDmzStatus(data)) throw new PendingStatusError();
      return data;
    },
    retry: (count, error) => error instanceof PendingStatusError && count < 8,
    retryDelay: (attempt) => 1500 * Math.min(attempt + 1, 4),
  });
}

export function useSavePortRules() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (rules: PortRule[]) => {
      const data = await firewallData<OkResponse>("save", buildSaveParams(rules));
      if (data?.ok === false) throw new Error(data.error ?? "");
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: FIREWALL_STATUS_KEY });
    },
  });
}

export function useSaveFwdRules() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (rules: FwdRule[]) => {
      const data = await firewallData<OkResponse>("fwd_save", { rules: fwdSaveParam(rules) });
      if (data?.ok === false) throw new Error(data.error ?? "");
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: FIREWALL_FWD_KEY });
    },
  });
}

// type(而非 interface)以便隐式索引签名兼容 ApiParams
export type DmzSetParams = {
  enabled: "0" | "1";
  ip?: string;
};

export function useSetDmz() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (params: DmzSetParams) => {
      const data = await networkConfigData("dmz", params);
      if (data?.ok === false) throw new Error(typeof data.error === "string" ? data.error : "");
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: FIREWALL_DMZ_KEY });
    },
  });
}
