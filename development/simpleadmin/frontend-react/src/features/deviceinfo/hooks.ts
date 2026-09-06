// deviceinfo 数据流(TanStack Query):deviceInfoData("get", { force: false }) 5s 轮询
// (旧版 deviceinfo.js DEVICE_INFO_REFRESH_MS=5000 + pauseWhenHidden,refetchInterval 回调做
// document.hidden 门控,visible 恢复立即补拉)。pending:true 是"就绪中"正常分支——保留旧值渲染
// 且绝不按失败退避(旧版注释同款纪律);data.error 抛错走重试(retry:2 ≈ 旧版连续 3 次失败才亮横幅)。
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

import { deviceInfoData } from "@/lib/api";
import type { DeviceInfoData } from "@/lib/api";
import { DEVICE_INFO_POLL_MS, visiblePollInterval } from "./lib";

export const DEVICE_INFO_QUERY_KEY = ["deviceInfo"] as const;

export interface DeviceInfoStream {
  /** pending 时为最近一次好数据(可能 undefined) */
  view: DeviceInfoData | undefined;
  pending: boolean;
  failed: boolean;
  isFetching: boolean;
  refetch: () => void;
}

export function useDeviceInfoStream(): DeviceInfoStream {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: [...DEVICE_INFO_QUERY_KEY],
    queryFn: async (): Promise<DeviceInfoData> => {
      const data = await deviceInfoData("get", { force: false });
      if (!data) throw new Error("empty device info");
      // pending 绝不 throw(旧版注释:否则被轮询器计为失败触发指数退避,就绪后用户久等)
      if (!data.pending && data.error) throw new Error(data.error);
      return data;
    },
    refetchInterval: () => visiblePollInterval(DEVICE_INFO_POLL_MS),
    retry: 2,
  });

  useEffect(() => {
    const onVisibilityChange = (): void => {
      if (document.hidden) return;
      void queryClient.invalidateQueries({ queryKey: [...DEVICE_INFO_QUERY_KEY] });
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => document.removeEventListener("visibilitychange", onVisibilityChange);
  }, [queryClient]);

  const { refetch } = query;
  const lastGoodRef = useRef<DeviceInfoData | null>(null);
  const raw = query.data;
  if (raw && !raw.pending) lastGoodRef.current = raw;
  const pending = raw?.pending === true;
  const view = pending ? (lastGoodRef.current ?? raw) : raw;
  return {
    view,
    pending,
    failed: query.isError,
    isFetching: query.isFetching,
    refetch: useCallback(() => void refetch(), [refetch]),
  };
}
