// automation 数据层:watchdog/scheduler/timesync 的 get 查询与 set 变更。
// 保存后回读(get/set 契约):变更 onSettled 一律 invalidate 对应查询,对齐旧版 finally → fetch*。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  schedulerGet,
  schedulerSet,
  timesyncGet,
  timesyncNow,
  timesyncSet,
  watchdogGet,
  watchdogSet,
} from "@/lib/api";
import type { ApiParams, ConfigSetResult } from "@/lib/api";

export const WATCHDOG_KEY = ["automation", "watchdog"] as const;
export const SCHEDULER_KEY = ["automation", "scheduler"] as const;
export const TIMESYNC_KEY = ["automation", "timesync"] as const;

// 后端写失败为 200 + ok:false(error),如实转异常;500 由 endpoints 层抛 ApiError。
function ensureOk<T>(data: ConfigSetResult<T> | undefined): ConfigSetResult<T> {
  if (data?.ok === false) throw new Error(data.error ?? "");
  return data as ConfigSetResult<T>;
}

export function useWatchdog() {
  return useQuery({ queryKey: WATCHDOG_KEY, queryFn: () => watchdogGet(), retry: false });
}

export function useScheduler() {
  return useQuery({ queryKey: SCHEDULER_KEY, queryFn: () => schedulerGet(), retry: false });
}

export function useTimesync() {
  return useQuery({ queryKey: TIMESYNC_KEY, queryFn: () => timesyncGet(), retry: false });
}

export function useSaveWatchdog() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (params: ApiParams) => ensureOk(await watchdogSet(params)),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: WATCHDOG_KEY });
    },
  });
}

export function useSaveScheduler() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (params: ApiParams) => ensureOk(await schedulerSet(params)),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: SCHEDULER_KEY });
    },
  });
}

export function useSaveTimesync() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (params: ApiParams) => ensureOk(await timesyncSet(params)),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: TIMESYNC_KEY });
    },
  });
}

export function useTimesyncNow() {
  const queryClient = useQueryClient();
  return useMutation({
    // 手动同步结果(ok/offsetMs/error)由组件消费,200 + ok:false 是运行时结果而非异常
    mutationFn: () => timesyncNow(),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: TIMESYNC_KEY });
    },
  });
}
