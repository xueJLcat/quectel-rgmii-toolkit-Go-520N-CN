// network 页数据层:settings/bands 查询 + lock_bands/reset_bands/save_settings 变更。
// 行为对齐旧版 www/js/pages/network.js:
// - 写操作成功 → toast + 延迟 3s 回读(等待模块 settle,对齐旧版 sleep(3000) 后 refetch);
// - 200+ok:false → danger toast "操作失败: <error>"(reportNetworkActionFailure 语义);
// - pending → 保留旧值 + chip(useLastGood);不做轮询,路由重挂载即 refetch(onPageReturn 语义)。
// 配置档 useBandProfiles:localStorage 读写 + 旧版全部校验 toast(重名/无名/写入失败)。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";

import { networkData } from "@/lib/api";
import type { ApiParams, NetworkDataResponse } from "@/lib/api";
import { useT } from "@/lib/i18n";

import { loadBandProfiles, persistBandProfiles } from "./lib";
import type { BandProfile, BandProfileData } from "./lib";

export const SETTINGS_QUERY_KEY = ["network", "settings"] as const;
export const BANDS_QUERY_KEY = ["network", "bands"] as const;

/** 写操作后延迟回读间隔(对齐旧版 sleep(3000):AT 写入后模块需要时间生效)。 */
export const REFRESH_DELAY_MS = 3000;

/** pending/error 时保留上一份好数据(§5.2:pending 绝不闪空态/假默认值)。 */
export function useLastGood<T>(data: T | undefined, isGood: (value: T) => boolean): T | undefined {
  const lastGood = useRef<T | undefined>(undefined);
  if (data !== undefined && isGood(data)) lastGood.current = data;
  return data !== undefined && isGood(data) ? data : lastGood.current;
}

export function isGoodSettings(data: NetworkDataResponse): boolean {
  return data.pending !== true && !data.error;
}

export function isGoodBands(data: NetworkDataResponse): boolean {
  return data.pending !== true && !data.error;
}

export function useNetworkSettingsQuery() {
  const query = useQuery({
    queryKey: SETTINGS_QUERY_KEY,
    queryFn: () => networkData("settings"),
  });
  const settings = useLastGood(query.data, isGoodSettings);
  return { ...query, settings };
}

export function useLockedBandsQuery() {
  const query = useQuery({
    queryKey: BANDS_QUERY_KEY,
    queryFn: () => networkData("bands", { force: "1", wait: "1" }),
  });
  const bands = useLastGood(query.data, isGoodBands);
  return { ...query, bands };
}

/** 200+ok:false 判定(对齐旧版 applyNetworkActionResult:!!(result && result.ok !== false))。 */
export function isActionFailure(result?: NetworkDataResponse | null): boolean {
  return !result || result.ok === false;
}

function useNetworkActions() {
  const queryClient = useQueryClient();
  const { t } = useT("network");

  const scheduleRefresh = useCallback(
    (keys: readonly (readonly string[])[]) => {
      window.setTimeout(() => {
        for (const queryKey of keys) void queryClient.invalidateQueries({ queryKey });
      }, REFRESH_DELAY_MS);
    },
    [queryClient],
  );

  const reportFailure = useCallback(
    (result?: NetworkDataResponse | null) => {
      const message =
        t("common:operationFailed") + (result && result.error ? `: ${result.error}` : "");
      toast.error(message);
    },
    [t],
  );

  return { queryClient, scheduleRefresh, reportFailure, t };
}

export function useLockBands() {
  const { scheduleRefresh, reportFailure, t } = useNetworkActions();
  return useMutation({
    mutationFn: (args: { mode: string; values: string }) =>
      networkData("lock_bands", { mode: args.mode, values: args.values }),
    onSuccess: (result) => {
      if (isActionFailure(result)) {
        reportFailure(result);
        return;
      }
      toast.info(t("bandLockSubmittedRefreshingStatus"));
      scheduleRefresh([BANDS_QUERY_KEY, SETTINGS_QUERY_KEY]);
    },
    onError: () => toast.error(t("common:operationFailed")),
  });
}

export function useResetBands() {
  const { scheduleRefresh, reportFailure, t } = useNetworkActions();
  return useMutation({
    mutationFn: (args: { lte: string; nsa: string; sa: string }) =>
      networkData("reset_bands", args),
    onSuccess: (result) => {
      if (isActionFailure(result)) {
        reportFailure(result);
        return;
      }
      toast.info(t("bandLockSubmittedRefreshingStatus"));
      scheduleRefresh([BANDS_QUERY_KEY, SETTINGS_QUERY_KEY]);
    },
    onError: () => toast.error(t("common:operationFailed")),
  });
}

export function useSaveSettings() {
  const { scheduleRefresh, reportFailure, t } = useNetworkActions();
  return useMutation({
    mutationFn: (payload: ApiParams) => networkData("save_settings", payload),
    onSuccess: (result) => {
      if (isActionFailure(result)) {
        reportFailure(result);
        return;
      }
      toast.success(t("common:saved"));
      // 旧版仅重读设置字段,避免整体 init 丢弃用户未提交的频段勾选
      scheduleRefresh([SETTINGS_QUERY_KEY]);
    },
    onError: () => toast.error(t("common:operationFailed")),
  });
}

export interface BandProfilesApi {
  profiles: BandProfile[];
  /** 返回是否保存成功(校验失败/写入失败为 false,调用方据此决定是否清空输入) */
  saveProfile: (name: string, data: BandProfileData) => boolean;
  deleteProfile: (name: string) => void;
}

export function useBandProfiles(): BandProfilesApi {
  const { t } = useT("network");
  const [profiles, setProfiles] = useState<BandProfile[]>(() => loadBandProfiles());

  const saveProfile = useCallback(
    (name: string, data: BandProfileData): boolean => {
      const trimmed = name.trim();
      if (!trimmed) {
        toast.error(t("pleaseEnterAProfileName"));
        return false;
      }
      if (profiles.some((profile) => profile.name === trimmed)) {
        toast.error(t("aProfileWithTheSameNameAlreadyExists"));
        return false;
      }
      const next: BandProfile[] = [
        ...profiles,
        { name: trimmed, savedAt: new Date().toISOString(), data },
      ];
      if (!persistBandProfiles(next)) {
        toast.error(t("failedToSaveProfile"));
        return false;
      }
      setProfiles(next);
      toast.success(t("profileSaved"));
      return true;
    },
    [profiles, t],
  );

  const deleteProfile = useCallback(
    (name: string) => {
      const next = profiles.filter((profile) => profile.name !== name);
      if (!persistBandProfiles(next)) {
        toast.error(t("failedToSaveProfile"));
        return;
      }
      setProfiles(next);
      toast.success(t("profileDeleted"));
    },
    [profiles, t],
  );

  return { profiles, saveProfile, deleteProfile };
}
