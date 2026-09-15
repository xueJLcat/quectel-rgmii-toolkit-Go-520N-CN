// 信号详情页数据层:5s 轮询仅页面激活期(refetchIntervalInBackground:false 即 visibility 门控,
// 对齐旧版 Poll.create({interval:5000, immediate:true, pauseWhenHidden:true}));
// pending 抛 SignalPendingError → query 保留上次成功数据(§5.2 开机保护期语义),5s 节奏即重试循环。
import { useQuery } from "@tanstack/react-query";

import { signalData } from "@/lib/api";
import type { SignalData } from "@/lib/api";

import { SignalPendingError } from "./lib";

export const SIGNAL_QUERY_KEY = ["signal"] as const;

export function useSignalQuery() {
  return useQuery({
    queryKey: SIGNAL_QUERY_KEY,
    queryFn: async (): Promise<SignalData> => {
      const data = await signalData();
      if (!data || data.pending === true) throw new SignalPendingError();
      return data;
    },
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
    retry: false,
  });
}

export function isSignalPendingError(error: unknown): boolean {
  return error instanceof SignalPendingError;
}
