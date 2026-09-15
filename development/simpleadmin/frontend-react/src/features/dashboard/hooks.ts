// dashboard 数据流(TanStack Query):主轮询 dashboardData 由刷新频率驱动(refetchInterval 回调做
// document.hidden 门控),historyData 60s 低频;at_cache_updated 服务端事件 → 800ms 去抖后静默
// invalidate(对齐旧版 _pushRefreshTimer);visible 恢复时立即补拉一次(对齐旧版 Poll pauseWhenHidden)。
// pending:true 视为"就绪中":保留最近一次好数据渲染,绝不落到假空态(旧版 applyDashboardData 提前返回语义)。
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";

import { dashboardData, gateway, historyData } from "@/lib/api";
import type { DashboardData, HistoryPoint } from "@/lib/api";
import {
  AT_CACHE_DEBOUNCE_MS,
  HISTORY_POLL_MS,
  REFRESH_RATE_MIN,
  clampRefreshRate,
  readNetworkCompact,
  readStoredRefreshRate,
  resolveTraffic,
  storeNetworkCompact,
  storeRefreshRate,
  visiblePollInterval,
} from "./lib";
import type { TrafficSample, TrafficView } from "./lib";
import { EMPTY_TRAFFIC } from "./lib";

export const DASHBOARD_QUERY_KEY = ["dashboardData"] as const;
export const HISTORY_QUERY_KEY = ["historyData"] as const;

const EMPTY_POINTS: readonly HistoryPoint[] = [];

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

export interface DashboardStream {
  /** pending/error 时为最近一次好数据(可能 undefined) */
  view: DashboardData | undefined;
  pending: boolean;
  /** 服务端 !pending && error 的文案(旧版 dataError) */
  errorText: string;
  /** 传输失败且没有任何可显示数据 */
  failed: boolean;
  /** 传输失败但仍有旧值可显示 */
  stale: boolean;
  isFetching: boolean;
  refetch: () => void;
}

export function useDashboardStream(refreshSeconds: number): DashboardStream {
  const query = useQuery({
    queryKey: [...DASHBOARD_QUERY_KEY],
    queryFn: () => dashboardData(),
    refetchInterval: () => visiblePollInterval(Math.max(REFRESH_RATE_MIN, refreshSeconds) * 1000),
  });
  useVisibleResume(DASHBOARD_QUERY_KEY);

  const { refetch } = query;
  const lastGoodRef = useRef<DashboardData | null>(null);
  const raw = query.data;
  if (raw && !raw.pending && !raw.error) lastGoodRef.current = raw;
  const pending = raw?.pending === true;
  const errorText = !pending && raw?.error ? String(raw.error) : "";
  const view = pending || errorText ? (lastGoodRef.current ?? raw) : raw;
  return {
    view,
    pending,
    errorText,
    failed: query.isError && !view,
    stale: query.isError && !!view,
    isFetching: query.isFetching,
    refetch: useCallback(() => void refetch(), [refetch]),
  };
}

export function useHistoryStream(): readonly HistoryPoint[] {
  const query = useQuery({
    queryKey: [...HISTORY_QUERY_KEY],
    queryFn: () => historyData(),
    refetchInterval: () => visiblePollInterval(HISTORY_POLL_MS),
  });
  useVisibleResume(HISTORY_QUERY_KEY);
  return query.data?.points ?? EMPTY_POINTS;
}

// 服务端 AT 缓存刷新推送 → 去抖 800ms 后静默重拉(旧版 _atCacheUpdatedHandler 语义)
export function useAtCacheSilentRefresh(): void {
  const queryClient = useQueryClient();
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const off = gateway.onServerEvent("at_cache_updated", () => {
      if (timer !== null) return;
      timer = setTimeout(() => {
        timer = null;
        void queryClient.invalidateQueries({ queryKey: [...DASHBOARD_QUERY_KEY] });
      }, AT_CACHE_DEBOUNCE_MS);
    });
    return () => {
      off();
      if (timer !== null) clearTimeout(timer);
    };
  }, [queryClient]);
}

export function useRefreshRate(): { rate: number; applyRate: (raw: string) => boolean } {
  const [rate, setRate] = useState<number>(readStoredRefreshRate);
  const applyRate = useCallback((raw: string) => {
    if (raw.trim() === "") return false;
    const value = Number(raw);
    if (!Number.isFinite(value)) return false;
    const next = clampRefreshRate(value);
    setRate(next);
    storeRefreshRate(next);
    return true;
  }, []);
  return { rate, applyRate };
}

export function useNetworkCompact(): { compact: boolean; toggle: () => void } {
  const [compact, setCompact] = useState<boolean>(readNetworkCompact);
  const toggle = useCallback(() => {
    setCompact((prev) => !prev);
  }, []);
  useEffect(() => {
    storeNetworkCompact(compact);
  }, [compact]);
  return { compact, toggle };
}

// 旧版 updateTraffic 的 React 形态:跨采样保留 prev,按数据对象身份去重(StrictMode 双渲染安全)
export function useTrafficView(data: DashboardData | undefined): TrafficView {
  const sampleRef = useRef<TrafficSample | null>(null);
  const lastDataRef = useRef<DashboardData | undefined>(undefined);
  const viewRef = useRef<TrafficView>(EMPTY_TRAFFIC);
  if (data !== undefined && data !== lastDataRef.current) {
    lastDataRef.current = data;
    const resolved = resolveTraffic(data, sampleRef.current, Date.now());
    sampleRef.current = resolved.next;
    viewRef.current = resolved.view;
  }
  return viewRef.current;
}
