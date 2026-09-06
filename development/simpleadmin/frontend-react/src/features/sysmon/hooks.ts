// sysmon 数据流(TanStack Query):systemMonitor 3s 轮询(refetchInterval 回调做 document.hidden 门控,
// sort=cpu|mem 切换经 queryKey 变化立即重拉,keepPreviousData 防闪烁,对齐旧版 watch sortBy→fetchMonitor);
// AT+QTEMP 温度面板 60s 低频轮询 + 手动刷新(占用 AT 通道,失败重试收敛为 2 次)。
// visible 恢复时立即补拉一次(对齐旧版 Poll pauseWhenHidden 语义)。
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect } from "react";

import { atData, systemMonitor } from "@/lib/api";
import type { OkResponse, SystemMonitor } from "@/lib/api";
import { QTEMP_POLL_MS, SYSMON_POLL_MS, parseQtemp, visiblePollInterval } from "./lib";
import type { QtempReading } from "./lib";

export type ProcessSort = "cpu" | "mem";

export const SYSMON_QUERY_KEY = "systemMonitor";
export const QTEMP_QUERY_KEY = ["atData", "qtemp"] as const;

export const QTEMP_COMMAND = "AT+QTEMP";

// visible 恢复立即补拉(同时让 refetchInterval 回调重新计时,恢复轮询)
function useVisibleResume(queryKey: readonly unknown[]): void {
  const queryClient = useQueryClient();
  useEffect(() => {
    const onVisibilityChange = (): void => {
      if (document.hidden) return;
      void queryClient.invalidateQueries({ queryKey: [...queryKey] });
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => document.removeEventListener("visibilitychange", onVisibilityChange);
  }, [queryClient, queryKey]);
}

export function useSysmonQuery(sort: ProcessSort) {
  const queryKey = [SYSMON_QUERY_KEY, sort];
  const query = useQuery({
    queryKey,
    queryFn: async (): Promise<SystemMonitor> => {
      const data = await systemMonitor(sort);
      // 旧版 fetchMonitor:空载荷按失败处理(monitorLoadFailed)
      if (!data) throw new Error("empty monitor data");
      return data;
    },
    refetchInterval: () => visiblePollInterval(SYSMON_POLL_MS),
    placeholderData: keepPreviousData,
  });
  useVisibleResume(queryKey);
  return query;
}

export function useQtempQuery() {
  const query = useQuery({
    queryKey: [...QTEMP_QUERY_KEY],
    queryFn: async (): Promise<QtempReading[]> => {
      const response: OkResponse = await atData("manual_at", { command: QTEMP_COMMAND });
      if (!response || response.ok === false) {
        throw new Error(response?.error || "AT+QTEMP failed");
      }
      return parseQtemp(response.response ?? "");
    },
    refetchInterval: () => visiblePollInterval(QTEMP_POLL_MS),
    retry: 2,
  });
  useVisibleResume(QTEMP_QUERY_KEY);
  const { refetch } = query;
  return { ...query, refetch: useCallback(() => void refetch(), [refetch]) };
}
