// celllock 页数据层(行为对齐旧版 www/js/pages/celllock.js):
// - settings(小区锁定状态)与 earfcn_lock_status(频点锁状态)查询,pending 保留旧值;
// - 扫描 = 长任务 mutation(网关默认超时 240s 已覆盖"最长约 3 分钟"的邻区扫描);
// - 锁定所选 = NR5G→LTE 依次锁定,失败即中断(NR 已锁成功而 LTE 失败时仍回读刷新,
//   对齐旧版注释语义);手动锁/解锁/一键锁定/频点锁全部 mutation+toast+延迟 3s 回读。
// 旧版无任何动作携带 rebootAfterSeconds/rebootCountdownSeconds(后端 page_network.go 亦无),
// 故本页不接 reboot store 倒计时。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef } from "react";
import { toast } from "sonner";

import { networkData } from "@/lib/api";
import type { NeighbourCell, NetworkDataResponse } from "@/lib/api";
import { useT } from "@/lib/i18n";

import { findScannedCell } from "./lib";
import type { SelectedCell } from "./lib";

export const CELLLOCK_SETTINGS_QUERY_KEY = ["celllock", "settings"] as const;
export const EARFCN_STATUS_QUERY_KEY = ["celllock", "earfcnStatus"] as const;

/** 写操作后延迟回读间隔(对齐旧版 sleep(3000) 后 init():AT 写入后模块需要时间生效)。 */
export const REFRESH_DELAY_MS = 3000;

/** pending/error 时保留上一份好数据(§5.2:pending 绝不闪假状态)。 */
export function useLastGood<T>(data: T | undefined, isGood: (value: T) => boolean): T | undefined {
  const lastGood = useRef<T | undefined>(undefined);
  if (data !== undefined && isGood(data)) lastGood.current = data;
  return data !== undefined && isGood(data) ? data : lastGood.current;
}

const isGoodResponse = (data: NetworkDataResponse): boolean =>
  data.pending !== true && data.ok !== false && !data.error;

export function useCellLockSettingsQuery() {
  const query = useQuery({
    queryKey: CELLLOCK_SETTINGS_QUERY_KEY,
    queryFn: () => networkData("settings"),
  });
  return { ...query, settings: useLastGood(query.data, isGoodResponse) };
}

export function useEarfcnLockStatusQuery() {
  const query = useQuery({
    queryKey: EARFCN_STATUS_QUERY_KEY,
    queryFn: () => networkData("earfcn_lock_status"),
  });
  return { ...query, status: useLastGood(query.data, isGoodResponse) };
}

function useCellLockActions() {
  const queryClient = useQueryClient();
  const { t } = useT("celllock");

  const scheduleRefresh = useCallback(
    (
      keys: readonly (readonly string[])[] = [CELLLOCK_SETTINGS_QUERY_KEY, EARFCN_STATUS_QUERY_KEY],
    ) => {
      window.setTimeout(() => {
        for (const queryKey of keys) void queryClient.invalidateQueries({ queryKey });
      }, REFRESH_DELAY_MS);
    },
    [queryClient],
  );

  const refreshNow = useCallback(
    (keys: readonly (readonly string[])[] = [EARFCN_STATUS_QUERY_KEY]) => {
      for (const queryKey of keys) void queryClient.invalidateQueries({ queryKey });
    },
    [queryClient],
  );

  // 对齐旧版 reportNetworkActionFailure:t(label||'操作失败') + ': ' + error
  const reportFailure = useCallback(
    (result?: NetworkDataResponse | null, labelKey?: string) => {
      const label = labelKey ? t(labelKey) : t("common:operationFailed");
      toast.error(label + (result && result.error ? `: ${result.error}` : ""));
    },
    [t],
  );

  return { queryClient, scheduleRefresh, refreshNow, reportFailure, t };
}

/** 200+ok:false 判定(对齐旧版 applyNetworkActionResult)。 */
export function isActionFailure(result?: NetworkDataResponse | null): boolean {
  return !result || result.ok === false;
}

export function useCellScan() {
  const { t } = useCellLockActions();
  return useMutation({
    mutationFn: (mode: string) => networkData("scan", { mode }),
    onError: () => toast.error(t("scanFailedPleaseRetry")),
    onSuccess: (result) => {
      // 对齐旧版:danger toast = 后端 error 文案优先,否则"扫描失败，请重试"
      if (isActionFailure(result)) toast.error(result?.error || t("scanFailedPleaseRetry"));
    },
  });
}

export interface LockSelectedArgs {
  nr: SelectedCell | null;
  lte: SelectedCell[];
  nrCells: NeighbourCell[];
  lteCells: NeighbourCell[];
}

export type LockSelectedResult =
  | { ok: true; nr5gLocked: boolean }
  | {
      ok: false;
      reason: "nr-lock-failed" | "lte-lock-failed" | "cell-not-found";
      nr5gLocked: boolean;
      error?: string;
    };

/**
 * 锁定所选小区(对齐旧版 doLockSelectedCells):NR5G 先锁(lock_scanned_cells "NR5G Only",
 * 不带 scs → 后端守护探测 30/15),成功后再锁 LTE("LTE Only",earfcn/pci 逗号并列);
 * 任一环节失败立即中断;小区数据缺失 → cell-not-found(提示重新扫描)。
 */
export function useLockSelectedCells() {
  const { scheduleRefresh, reportFailure, t } = useCellLockActions();
  return useMutation({
    mutationFn: async (args: LockSelectedArgs): Promise<LockSelectedResult> => {
      let nr5gLocked = false;
      if (args.nr) {
        const details = findScannedCell(args.nrCells, args.nr);
        if (!details) return { ok: false, reason: "cell-not-found", nr5gLocked };
        const result = await networkData("lock_scanned_cells", {
          mode: "NR5G Only",
          pci: details.pci,
          earfcn: details.earfcn,
          band: details.band,
        }).catch(() => undefined);
        if (isActionFailure(result)) {
          return { ok: false, reason: "nr-lock-failed", nr5gLocked, error: result?.error };
        }
        nr5gLocked = true;
      }
      if (args.lte.length > 0) {
        const earfcns: string[] = [];
        const pcis: string[] = [];
        for (const cell of args.lte) {
          const details = findScannedCell(args.lteCells, cell);
          if (!details) return { ok: false, reason: "cell-not-found", nr5gLocked };
          earfcns.push(details.earfcn);
          pcis.push(details.pci);
        }
        const result = await networkData("lock_scanned_cells", {
          mode: "LTE Only",
          earfcn: earfcns.join(","),
          pci: pcis.join(","),
        }).catch(() => undefined);
        if (isActionFailure(result)) {
          return { ok: false, reason: "lte-lock-failed", nr5gLocked, error: result?.error };
        }
      }
      return { ok: true, nr5gLocked };
    },
    onSuccess: (outcome) => {
      if (outcome.ok) {
        toast.success(t("operationSucceededRefreshingStatus"));
        scheduleRefresh();
        return;
      }
      if (outcome.reason === "cell-not-found") {
        toast.error(t("cellDataNotFoundPleaseScanAgain"));
      } else {
        reportFailure({ error: outcome.error }, "lockFailed");
      }
      // NR5G 已锁成功而 LTE 失败:状态已变化,仍回读刷新(对齐旧版)
      if (outcome.nr5gLocked) scheduleRefresh();
    },
  });
}

export function useLockLteManual() {
  const { scheduleRefresh, reportFailure, t } = useCellLockActions();
  return useMutation({
    mutationFn: (args: { cellNum: number; pairs: string }) => networkData("lock_lte_manual", args),
    onSuccess: (result) => {
      if (isActionFailure(result)) {
        reportFailure(result);
        return;
      }
      toast.success(t("operationSucceededRefreshingStatus"));
      scheduleRefresh();
    },
    onError: () => toast.error(t("common:operationFailed")),
  });
}

export function useLockNrManual() {
  const { scheduleRefresh, reportFailure, t } = useCellLockActions();
  return useMutation({
    // scs="auto" 时不提交 scs,由后端 inferNRSCSFromBand 推断并守护探测
    mutationFn: (args: { pci: string; earfcn: string; scs: string; band: string }) =>
      networkData("lock_nr_manual", {
        pci: args.pci,
        earfcn: args.earfcn,
        band: args.band,
        scs: args.scs === "auto" ? undefined : args.scs,
      }),
    onSuccess: (result) => {
      if (isActionFailure(result)) {
        // 失败自动解锁提示由后端 error 文案携带(SCS 探测失败，锁定已自动解除…)
        reportFailure(result);
        return;
      }
      toast.success(t("operationSucceededRefreshingStatus"));
      scheduleRefresh();
    },
    onError: () => toast.error(t("common:operationFailed")),
  });
}

export function useUnlockCell() {
  const { scheduleRefresh, reportFailure, t } = useCellLockActions();
  return useMutation({
    mutationFn: (action: "unlock_lte" | "unlock_nr") => networkData(action),
    onSuccess: (result) => {
      if (isActionFailure(result)) {
        reportFailure(result, "unlockFailed");
        return;
      }
      toast.success(t("operationSucceededRefreshingStatus"));
      scheduleRefresh();
    },
    onError: () => toast.error(t("unlockFailed")),
  });
}

export function useLockServingCell() {
  const { scheduleRefresh, t } = useCellLockActions();
  return useMutation({
    mutationFn: (scs: string) => networkData("lock_serving_cell", scs === "auto" ? {} : { scs }),
    onSuccess: (result) => {
      if (isActionFailure(result)) {
        toast.error(result?.error || t("scsProbeFailedLockAutoReleasedPickScsManuallyAndRetry"));
        return;
      }
      // 对齐旧版:locked / pci / freq / band 非空字段以 " / " 拼接展示
      const details = [result.locked, result.pci, result.freq, result.band]
        .filter((value) => value !== undefined && value !== null && value !== "")
        .join(" / ");
      toast.success(details);
      scheduleRefresh();
    },
    onError: () => toast.error(t("scsProbeFailedLockAutoReleasedPickScsManuallyAndRetry")),
  });
}

export function useSetEarfcnLock() {
  const { refreshNow, t } = useCellLockActions();
  return useMutation({
    mutationFn: (args: { rat: "NR5G" | "LTE"; arfcns: string[] }) =>
      networkData("set_earfcn_lock", { rat: args.rat, arfcns: args.arfcns.join(",") }),
    onSuccess: (result) => {
      if (isActionFailure(result)) {
        toast.error(result?.error || t("common:operationFailed"));
        return;
      }
      toast.success(t("operationSucceededRefreshingStatus"));
      refreshNow();
    },
    onError: () => toast.error(t("common:operationFailed")),
  });
}

export function useClearEarfcnLock() {
  const { refreshNow, reportFailure, t } = useCellLockActions();
  return useMutation({
    mutationFn: (rat: "NR5G" | "LTE") => networkData("clear_earfcn_lock", { rat }),
    onSuccess: (result) => {
      if (isActionFailure(result)) {
        reportFailure(result, "unlockFailed");
        return;
      }
      toast.success(t("operationSucceededRefreshingStatus"));
      refreshNow();
    },
    onError: () => toast.error(t("unlockFailed")),
  });
}
