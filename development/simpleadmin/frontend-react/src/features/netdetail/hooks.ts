// 网络详情页数据层:轮询仅激活期(refetchIntervalInBackground:false 即 visibility 门控;
// 节奏对齐设计方案 §6.6 的 5s),pending 抛 NetdetailPendingError → 保留上次成功数据(§5.2);
// 速率历史在组件内滚动窗口累积(≤30 点),每次成功拉取(dataUpdatedAt 变化)追加一点。
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import { networkDetail } from "@/lib/api";
import type { NetworkDetail, NetworkInterfaceStat } from "@/lib/api";

import { NetdetailPendingError, appendRatePoint } from "./lib";
import type { RateHistory } from "./lib";

export const NETDETAIL_QUERY_KEY = ["netdetail"] as const;

export function useNetworkDetailQuery() {
  return useQuery({
    queryKey: NETDETAIL_QUERY_KEY,
    queryFn: async (): Promise<NetworkDetail> => {
      const data = await networkDetail();
      if (!data || data.pending === true) throw new NetdetailPendingError();
      return data;
    },
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
    retry: false,
  });
}

export function isNetdetailPendingError(error: unknown): boolean {
  return error instanceof NetdetailPendingError;
}

/** 接口速率滚动窗口:以 dataUpdatedAt 为节拍追加(同一帧重复渲染不重复累积)。 */
export function useRateHistory(
  interfaces: readonly NetworkInterfaceStat[] | undefined,
  dataUpdatedAt: number,
): RateHistory {
  const [history, setHistory] = useState<RateHistory>({});
  const interfacesRef = useRef(interfaces);
  interfacesRef.current = interfaces;
  useEffect(() => {
    if (dataUpdatedAt === 0) return;
    setHistory((prev) => appendRatePoint(prev, interfacesRef.current));
  }, [dataUpdatedAt]);
  return history;
}
