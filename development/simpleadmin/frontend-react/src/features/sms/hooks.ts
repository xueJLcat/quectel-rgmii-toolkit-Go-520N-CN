// sms 页数据 hooks:
// - useSmsInbox:收件箱状态机(5s list_meta 轻量轮询 + visibility 门控 + diff 后才拉正文,
//   对齐旧版 pages/sms.js;pending/error 绝不用空列表覆盖既有收件箱);
// - useSmsStorage:存储可视化条数据(AT+CPMS?/AT+CSCA? 组合命令经 at_data manual_at 低频拉取);
// - useSmsImsi:IMSI(device_info_data,静态字段长缓存)供号码归一化预览;
// - useSendSms:发送流程(sim_status 前置检查 → send;失败提取 CMS 错误码释义,保留表单值);
// - useSmsWebhook:短信转发 Webhook 配置 get/set(从自动化页迁入,契约不变)。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { ApiError, atData, deviceInfoData, smsData, smsWebhookGet, smsWebhookSet } from "@/lib/api";
import type { SmsDataResponse } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import {
  appliedSyncState,
  buildDeleteIndices,
  createSmsSyncState,
  evaluateListResult,
  evaluateMetaPoll,
  explainCmsError,
  parseSmsStorageResponse,
  rowKey,
  SMS_POLL_INTERVAL_MS,
  SMS_STORAGE_AT_COMMAND,
  toSmsRows,
} from "./lib";
import type { SmsRow, SmsSyncState } from "./lib";

export interface SmsInboxState {
  rows: SmsRow[];
  serviceCenters: string[];
  /** 开机/模块重启保护期:保留旧值 + "获取中…" chip(§5.2 绝不显示假空态) */
  pending: boolean;
  loadFailed: boolean;
  initialLoading: boolean;
}

const EMPTY_INBOX: SmsInboxState = {
  rows: [],
  serviceCenters: [],
  pending: false,
  loadFailed: false,
  initialLoading: true,
};

export interface UseSmsInbox extends SmsInboxState {
  selected: ReadonlySet<number>;
  allSelected: boolean;
  detailIndex: number | null;
  toggleRow: (index: number) => void;
  toggleAll: () => void;
  invertSelection: () => void;
  clearSelection: () => void;
  openDetail: (index: number) => void;
  closeDetail: () => void;
  /** 手动刷新(list_meta diff 路径,失败 toast) */
  refresh: () => Promise<void>;
  /** 强制重拉正文(删除/发送成功后) */
  forceReload: () => Promise<void>;
  deleteSelected: () => Promise<void>;
  deleteAll: () => Promise<void>;
}

export function useSmsInbox(): UseSmsInbox {
  const { t } = useT("sms");
  const confirm = useConfirmStore((state) => state.confirm);
  const [inbox, setInbox] = useState<SmsInboxState>(EMPTY_INBOX);
  const [selected, setSelected] = useState<ReadonlySet<number>>(new Set());
  const [detailIndex, setDetailIndex] = useState<number | null>(null);

  const syncRef = useRef<SmsSyncState>(createSmsSyncState());
  const busyRef = useRef(false);
  const rowsRef = useRef<SmsRow[]>([]);
  const selectedRef = useRef<ReadonlySet<number>>(new Set());
  const detailRef = useRef<number | null>(null);
  rowsRef.current = inbox.rows;
  selectedRef.current = selected;
  detailRef.current = detailIndex;

  /** 应用正文:按行 key 恢复选中与详情(对齐旧版 keepSelection/keepDetail)。 */
  const applyData = useCallback((data: SmsDataResponse) => {
    const rows = toSmsRows(data.messages ?? []);
    const previousRows = rowsRef.current;
    const selectedKeys = new Set(
      [...selectedRef.current]
        .map((index) => previousRows[index])
        .filter(Boolean)
        .map(rowKey),
    );
    const nextSelected = new Set<number>();
    rows.forEach((row, index) => {
      if (selectedKeys.has(rowKey(row))) nextSelected.add(index);
    });
    const detailKey =
      detailRef.current !== null && previousRows[detailRef.current]
        ? rowKey(previousRows[detailRef.current])
        : "";
    const nextDetail = detailKey ? rows.findIndex((row) => rowKey(row) === detailKey) : -1;

    syncRef.current = appliedSyncState(data);
    setInbox({
      rows,
      serviceCenters: data.serviceCenters ?? [],
      pending: false,
      loadFailed: false,
      initialLoading: false,
    });
    setSelected(nextSelected);
    setDetailIndex(nextDetail >= 0 ? nextDetail : null);
  }, []);

  /** pending/error 应答统一处理:保留旧值,只更新标志(返回 true 表示中止应用)。 */
  const applyPendingOrError = useCallback((data: SmsDataResponse): boolean => {
    if (data.pending === true) {
      setInbox((prev) => ({ ...prev, pending: true, initialLoading: false }));
      return true;
    }
    if (data.error) {
      setInbox((prev) => ({ ...prev, pending: false, loadFailed: true, initialLoading: false }));
      return true;
    }
    return false;
  }, []);

  const fetchList = useCallback(
    async (expectedIndexSignature: string, silent: boolean) => {
      const data = await smsData("list", { force: "1" });
      if (applyPendingOrError(data)) return;
      const evaluation = evaluateListResult(
        syncRef.current,
        data,
        expectedIndexSignature,
        Date.now(),
      );
      syncRef.current = evaluation.state;
      if (evaluation.decision.kind === "apply") applyData(data);
      else if (!silent) setInbox((prev) => ({ ...prev, initialLoading: false }));
    },
    [applyData, applyPendingOrError],
  );

  /** 初次加载:直接拉正文(force),随后进入轮询节奏。 */
  useEffect(() => {
    let cancelled = false;
    busyRef.current = true;
    smsData("list", { force: "1" })
      .then((data) => {
        if (cancelled) return;
        if (applyPendingOrError(data)) return;
        applyData(data);
      })
      .catch(() => {
        if (!cancelled) {
          setInbox((prev) => ({ ...prev, loadFailed: true, initialLoading: false }));
        }
      })
      .finally(() => {
        busyRef.current = false;
      });
    return () => {
      cancelled = true;
    };
  }, [applyData, applyPendingOrError]);

  /** 5s list_meta 轻量轮询:visibility 门控(后台标签不占用 AT 通道),diff 后才拉正文。 */
  useEffect(() => {
    const tick = () => {
      if (typeof document !== "undefined" && document.hidden) return;
      if (busyRef.current) return;
      busyRef.current = true;
      smsData("list_meta")
        .then((data) => {
          const step = evaluateMetaPoll(syncRef.current, data, Date.now());
          syncRef.current = step.state;
          if (!step.fetch) return undefined;
          return fetchList(step.fetch.expectedIndexSignature, true);
        })
        .catch((error: unknown) => {
          // 轮询失败静默(下一轮自动补取),与旧版 smsPollFailCount 仅告警一致。
          console.warn("短信轮询失败", error);
        })
        .finally(() => {
          busyRef.current = false;
        });
    };
    const timer = setInterval(tick, SMS_POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [fetchList]);

  const refresh = useCallback(async () => {
    if (busyRef.current) return;
    busyRef.current = true;
    try {
      const data = await smsData("list_meta");
      if (applyPendingOrError(data)) return;
      const step = evaluateMetaPoll(syncRef.current, data, Date.now());
      syncRef.current = step.state;
      if (step.fetch) await fetchList(step.fetch.expectedIndexSignature, false);
      setInbox((prev) => ({ ...prev, loadFailed: false }));
    } catch {
      setInbox((prev) => ({ ...prev, loadFailed: true }));
      toast.error(t("failedToReadSms"));
    } finally {
      busyRef.current = false;
    }
  }, [applyPendingOrError, fetchList, t]);

  const forceReload = useCallback(async () => {
    if (busyRef.current) return;
    busyRef.current = true;
    try {
      await fetchList("", false);
    } catch {
      toast.error(t("failedToReadSms"));
    } finally {
      busyRef.current = false;
    }
  }, [fetchList, t]);

  const toggleRow = useCallback((index: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(index)) next.delete(index);
      else next.add(index);
      return next;
    });
  }, []);

  const toggleAll = useCallback(() => {
    setSelected((prev) =>
      prev.size === rowsRef.current.length && prev.size > 0
        ? new Set()
        : new Set(rowsRef.current.map((_, index) => index)),
    );
  }, []);

  const invertSelection = useCallback(() => {
    setSelected((prev) => {
      const next = new Set<number>();
      rowsRef.current.forEach((_, index) => {
        if (!prev.has(index)) next.add(index);
      });
      return next;
    });
  }, []);

  const clearSelection = useCallback(() => setSelected(new Set()), []);
  const openDetail = useCallback((index: number) => setDetailIndex(index), []);
  const closeDetail = useCallback(() => setDetailIndex(null), []);

  const deleteAll = useCallback(async () => {
    const ok = await confirm({
      title: t("clearSms"),
      message: t("deleteAllMessages"),
      confirmText: t("clearAll", { ns: "common" }),
      danger: true,
    });
    if (!ok) return;
    try {
      const data = await smsData("delete_all");
      if (data.ok === false) {
        toast.error(`${t("deleteFailed")}: ${data.error ?? t("unknownError", { ns: "common" })}`);
        return;
      }
      setSelected(new Set());
      setDetailIndex(null);
      await forceReload();
    } catch {
      toast.error(t("deleteFailed"));
    }
  }, [confirm, forceReload, t]);

  const deleteSelected = useCallback(async () => {
    const rows = rowsRef.current;
    const chosen = [...selectedRef.current].map((index) => rows[index]).filter(Boolean);
    if (chosen.length === 0) return;
    // 全选即清空(对齐旧版:选中数 === 总数时走清空确认流程)。
    if (chosen.length === rows.length) {
      await deleteAll();
      return;
    }
    const ok = await confirm({
      title: t("deleteSms"),
      message: t("deleteTheSelectedMessages"),
      confirmText: t("delete", { ns: "common" }),
      danger: true,
    });
    if (!ok) return;
    const indices = buildDeleteIndices(chosen);
    if (indices === "") {
      toast.error(t("noValidSmsIndex"));
      return;
    }
    try {
      const data = await smsData("delete_indices", { indices });
      if (data.ok === false) {
        toast.error(`${t("deleteFailed")}: ${data.error ?? t("unknownError", { ns: "common" })}`);
        return;
      }
      setSelected(new Set());
      await forceReload();
    } catch {
      toast.error(t("deleteFailed"));
    }
  }, [confirm, deleteAll, forceReload, t]);

  return {
    ...inbox,
    selected,
    allSelected: inbox.rows.length > 0 && selected.size === inbox.rows.length,
    detailIndex,
    toggleRow,
    toggleAll,
    invertSelection,
    clearSelection,
    openDetail,
    closeDetail,
    refresh,
    forceReload,
    deleteSelected,
    deleteAll,
  };
}

// ---------------------------------------------------------------------------
// 存储可视化条(AT+CPMS? / AT+CSCA?,低频以免占用 AT 通道)
// ---------------------------------------------------------------------------

export const SMS_STORAGE_REFRESH_MS = 60_000;

export function useSmsStorage() {
  return useQuery({
    queryKey: ["smsStorage"],
    queryFn: async () => {
      const resp = await atData("manual_at", { command: SMS_STORAGE_AT_COMMAND });
      return parseSmsStorageResponse(resp.response ?? "");
    },
    refetchInterval: SMS_STORAGE_REFRESH_MS,
    refetchIntervalInBackground: false,
    staleTime: SMS_STORAGE_REFRESH_MS / 2,
    retry: 1,
  });
}

// ---------------------------------------------------------------------------
// IMSI(号码归一化预览用;静态字段,会话级长缓存)
// ---------------------------------------------------------------------------

export function useSmsImsi(): string {
  const { data } = useQuery({
    queryKey: ["deviceInfoImsi"],
    queryFn: () => deviceInfoData("get"),
    staleTime: Infinity,
    retry: 1,
  });
  return typeof data?.imsi === "string" ? data.imsi : "";
}

// ---------------------------------------------------------------------------
// 发送短信
// ---------------------------------------------------------------------------

export interface UseSendSmsOptions {
  onSent: () => void;
}

export function useSendSms({ onSent }: UseSendSmsOptions) {
  const { t } = useT("sms");
  const queryClient = useQueryClient();

  return useMutation<{ segments: number }, Error, { number: string; message: string }>({
    mutationFn: async ({ number, message }) => {
      // SIM 前置检查:保护期应答 pending 时按已插卡放行(后端发送流程有精确报错)。
      try {
        const simStatus = await smsData("sim_status");
        if (simStatus.inserted === false && simStatus.pending !== true) {
          toast.error(t("noSimCardDetected"));
          throw new Error("sim-missing");
        }
      } catch (error) {
        if (error instanceof Error && error.message === "sim-missing") throw error;
        // sim_status 检查失败不阻断发送(对齐旧版 catch{})。
      }
      try {
        const result = await smsData("send", { number, message });
        if (result.ok === false) {
          throw new Error(result.error ?? t("unknownError", { ns: "common" }));
        }
        return { segments: Number(result.segments ?? 0) };
      } catch (error) {
        // 输入校验失败走 HTTP 400:ApiError.body 里有后端 {ok:false,error} 详情。
        let message = error instanceof Error ? error.message : String(error);
        if (error instanceof ApiError) {
          try {
            const body = JSON.parse(error.body) as SmsDataResponse;
            if (typeof body.error === "string" && body.error !== "") message = body.error;
          } catch {
            // body 非 JSON 时用 HTTP 层错误文本
          }
        }
        throw new Error(message);
      }
    },
    onSuccess: (result) => {
      toast.success(t("smsSentSuccessfully"));
      void queryClient.invalidateQueries({ queryKey: ["smsStorage"] });
      onSent();
      return result;
    },
    onError: (failure) => {
      if (failure.message === "sim-missing") return;
      // CMS 错误码释义(设计方案 §6.9):+CMS ERROR 350 — 运营商拒绝发送(物联卡常见)
      const text = describeSendFailure(failure, (key) => t(key));
      toast.error(t("smsSendFailedDetail", { ns: "common", reason: text }));
    },
  });
}

/** 发送失败错误的展示文本(CMS 释义映射,供表单错误区使用)。 */
export function describeSendFailure(error: unknown, t: (key: string) => string): string {
  const raw = error instanceof Error ? error.message : String(error ?? "");
  const info = explainCmsError(raw);
  return info ? `${info.label} — ${t(info.hintKey)}` : raw;
}

// ---------------------------------------------------------------------------
// 短信转发 Webhook(从自动化页迁入,get/set 契约不变)
// ---------------------------------------------------------------------------

export function useSmsWebhook() {
  const { t } = useT("sms");
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ["smsWebhook"], queryFn: () => smsWebhookGet(), retry: 1 });
  const mutation = useMutation({
    mutationFn: (vars: { enabled: boolean; url: string }) =>
      smsWebhookSet({ enabled: vars.enabled ? "1" : "0", url: vars.url }),
    onSuccess: (data) => {
      if (data.ok === false) {
        toast.error(data.error ?? t("saveFailed", { ns: "common" }));
      } else {
        toast.success(t("saved", { ns: "common" }));
      }
      void queryClient.invalidateQueries({ queryKey: ["smsWebhook"] });
    },
    onError: () => {
      toast.error(t("saveFailed", { ns: "common" }));
    },
  });
  return { query, mutation };
}
