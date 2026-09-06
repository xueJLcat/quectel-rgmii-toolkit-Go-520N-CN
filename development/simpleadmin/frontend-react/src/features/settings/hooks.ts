// settings 数据层:系统状态(IMEI,pending 退避重试对齐旧 fetchSystemStatus)、TTL 状态、
// 设备默认主题(服务端无有效值回退当前浏览器主题,对齐旧 fetchThemeSetting)+ 各写操作。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { getTTLStatus, setPassword, setTTL, systemData, themeGet } from "@/lib/api";
import type { ApiParams, SystemDataResponse } from "@/lib/api";
import { readCurrentTheme } from "@/lib/theme";
import type { ThemeMode } from "@/lib/theme";

import { PendingStatusError } from "./lib";

export const SYSTEM_STATUS_KEY = ["settings", "systemStatus"] as const;
export const TTL_STATUS_KEY = ["settings", "ttl"] as const;
export const THEME_KEY = ["settings", "deviceTheme"] as const;

/**
 * 系统状态(IMEI):pending(开机保护期)抛可重试错误,按旧版节奏退避重试 8 次
 * (1500×min(n,4)ms);data.error(AT 读取失败)直接进失败态,不静默保留旧 IMEI。
 */
export function useSystemStatus() {
  return useQuery({
    queryKey: SYSTEM_STATUS_KEY,
    queryFn: async () => {
      const data = await systemData<SystemDataResponse>("status", { force: "1" });
      if (data?.pending === true) throw new PendingStatusError();
      if (typeof data?.error === "string" && data.error !== "") throw new Error(data.error);
      return data;
    },
    retry: (count, error) => error instanceof PendingStatusError && count < 8,
    retryDelay: (attempt) => 1500 * Math.min(attempt + 1, 4),
  });
}

export function useTtlStatus() {
  return useQuery({ queryKey: TTL_STATUS_KEY, queryFn: () => getTTLStatus(), retry: false });
}

/** 设备默认主题:读取失败/无有效值时回退当前浏览器正在使用的主题,避免显示假默认值。 */
export function useDeviceTheme() {
  return useQuery({
    queryKey: THEME_KEY,
    queryFn: async (): Promise<ThemeMode> => {
      try {
        const data = await themeGet();
        if (data?.theme === "dark" || data?.theme === "light") return data.theme;
        return readCurrentTheme();
      } catch {
        return readCurrentTheme();
      }
    },
    retry: false,
  });
}

/** reboot / reboot_device / poweroff_device / set_imei 共用;ok:false 由调用方按动作语义分流。 */
export function useSystemAction() {
  return useMutation({
    mutationFn: ({ action, params }: { action: string; params?: ApiParams }) =>
      systemData(action, params),
  });
}

export function useSetTtl() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (value: number) => {
      const data = await setTTL(value);
      // 后端对非法取值/规则应用失败会返回 ok:false,不能一律显示"已保存"误导用户
      if (!data || data.ok === false) throw new Error(data?.error ?? "");
      return data;
    },
    // 对齐旧 setTTL:成败都回读,保证徽章与真实规则一致
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: TTL_STATUS_KEY });
    },
  });
}

export interface PasswordFormValues {
  current: string;
  next: string;
  confirm: string;
}

export function useChangePassword() {
  return useMutation({
    mutationFn: ({ current, next, confirm }: PasswordFormValues) =>
      setPassword(current, next, confirm),
  });
}
