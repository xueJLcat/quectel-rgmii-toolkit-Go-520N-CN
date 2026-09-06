// atcommands 页数据 hooks:
// - useAtSession:transcript/历史/发送/AT&F/重启 单一会话 hook。
//   发送经 at_data manual_at(in-flight 期间输入/快捷/危险按钮全部禁用);
//   历史持久化 localStorage "simpleadmin.atHistory"(兼容旧格式,≤50 去重);
//   AT&F:危险确认 → reset_at → 成功后才弹重启确认(对齐旧版 Reboot.request 前置确认);
//   重启:确认 → RebootOverlay 倒计时(真实时间戳,40s 缺省)+ system_data reboot,
//   ok:false/传输失败即取消倒计时并报错(对齐旧版 simpleadmin-reboot.js,不假装重启成功)。
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { atData, systemData } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";
import { REBOOT_DEFAULT_SECONDS, useRebootStore } from "@/stores/reboot";

import {
  appendTranscript,
  AT_HISTORY_KEY,
  historyNext,
  historyPrev,
  INITIAL_HISTORY_CURSOR,
  parseAtHistory,
  pushAtHistory,
  removeAtHistoryEntry,
} from "./lib";
import type { HistoryCursor } from "./lib";

export interface UseAtSession {
  transcript: string;
  isClean: boolean;
  /** manual_at in-flight */
  sending: boolean;
  /** reset_at in-flight */
  resetting: boolean;
  command: string;
  setCommand: (value: string) => void;
  send: () => Promise<void>;
  sendCommand: (cmd: string) => Promise<void>;
  clearTranscript: () => void;
  historyPrev: () => void;
  historyNext: () => void;
  history: string[];
  reuseHistory: (cmd: string) => void;
  removeHistory: (cmd: string) => void;
  clearHistory: () => void;
  resetAt: () => Promise<void>;
  rebootModule: () => Promise<void>;
}

function saveHistory(items: string[]): void {
  try {
    localStorage.setItem(AT_HISTORY_KEY, JSON.stringify(items));
  } catch {
    // 存储不可用时仅内存生效(对齐旧版 saveHistory 静默降级)
  }
}

export function useAtSession(): UseAtSession {
  const { t } = useT("atcommands");
  const confirm = useConfirmStore((state) => state.confirm);
  const startCountdown = useRebootStore((state) => state.startCountdown);
  const closeCountdown = useRebootStore((state) => state.closeCountdown);

  const [transcript, setTranscript] = useState("");
  const [sending, setSending] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [command, setCommand] = useState("");
  const [history, setHistory] = useState<string[]>([]);
  const cursorRef = useRef<HistoryCursor>(INITIAL_HISTORY_CURSOR);
  const sendingRef = useRef(false);
  const resettingRef = useRef(false);
  const commandRef = useRef(command);
  commandRef.current = command;
  // history 最新快照供 ↑/↓ 召回读取(避免召回回调依赖 history 频繁重建)
  const historyRef = useRef(history);
  historyRef.current = history;

  // 历史载入(旧格式兼容);localStorage 不可用时按空历史降级
  useEffect(() => {
    try {
      setHistory(parseAtHistory(localStorage.getItem(AT_HISTORY_KEY)));
    } catch {
      setHistory([]);
    }
  }, []);

  const pushHistory = useCallback((cmd: string) => {
    setHistory((prev) => {
      const next = pushAtHistory(prev, cmd);
      saveHistory(next);
      return next;
    });
  }, []);

  const sendCommand = useCallback(
    async (rawCommand?: string) => {
      if (sendingRef.current || resettingRef.current) return;
      const cmd = rawCommand ?? commandRef.current;
      // 空输入不发(对齐旧版);分号组合命令原样透传,不做拆分改写
      if (!cmd || !cmd.trim()) return;
      sendingRef.current = true;
      setSending(true);
      try {
        const data = await atData("manual_at", { command: cmd });
        if (data.ok === false) {
          throw new Error(data.error || t("unknownError", { ns: "common" }));
        }
        setTranscript((prev) => appendTranscript(prev, cmd, data.response ?? ""));
        pushHistory(cmd);
        cursorRef.current = INITIAL_HISTORY_CURSOR;
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        toast.error(`${t("failedToSendAtCommand")} ${message}`);
      } finally {
        sendingRef.current = false;
        setSending(false);
      }
    },
    [pushHistory, t],
  );

  const send = useCallback(() => sendCommand(), [sendCommand]);

  const recallPrev = useCallback(() => {
    const step = historyPrev(cursorRef.current, historyRef.current, commandRef.current);
    cursorRef.current = step.cursor;
    if (step.input !== commandRef.current) setCommand(step.input);
  }, []);

  const recallNext = useCallback(() => {
    const step = historyNext(cursorRef.current, historyRef.current, commandRef.current);
    cursorRef.current = step.cursor;
    if (step.input !== commandRef.current) setCommand(step.input);
  }, []);

  const clearTranscript = useCallback(() => setTranscript(""), []);

  const rebootModule = useCallback(async () => {
    const ok = await confirm({
      title: t("rebootModem"),
      message: t("rebootModemConfirm"),
      confirmText: t("reboot"),
      danger: true,
    });
    if (!ok) return;
    // 倒计时基于真实时间戳(RebootOverlay 单一实现),后端拒绝/传输失败即取消
    startCountdown({ seconds: REBOOT_DEFAULT_SECONDS });
    try {
      const data = await systemData("reboot");
      if (data.ok === false) {
        closeCountdown();
        toast.error(t("rebootFailed"));
      }
    } catch {
      closeCountdown();
      toast.error(t("rebootFailed"));
    }
  }, [closeCountdown, confirm, startCountdown, t]);

  const resetAt = useCallback(async () => {
    if (sendingRef.current || resettingRef.current) return;
    const ok = await confirm({
      title: t("resetAtF"),
      message: t("thisWillRestoreTheModemAtConfigurationToFactoryDefaults"),
      confirmText: t("confirmReset"),
      danger: true,
    });
    if (!ok) return;
    resettingRef.current = true;
    setResetting(true);
    try {
      const data = await atData("reset_at");
      if (data.ok === false) {
        toast.error(t("operationFailed", { ns: "common" }));
        return;
      }
      setTranscript("");
      // 成功才弹重启确认(对齐旧版:resetAT 成功后 SimpleAdmin.Reboot.request())
      await rebootModule();
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      toast.error(`${t("resetFailed")} ${message}`);
    } finally {
      resettingRef.current = false;
      setResetting(false);
    }
  }, [confirm, rebootModule, t]);

  const reuseHistory = useCallback((cmd: string) => {
    setCommand(cmd);
    cursorRef.current = INITIAL_HISTORY_CURSOR;
  }, []);

  const removeHistory = useCallback((cmd: string) => {
    setHistory((prev) => {
      const next = removeAtHistoryEntry(prev, cmd);
      saveHistory(next);
      return next;
    });
  }, []);

  const clearHistory = useCallback(() => {
    setHistory([]);
    cursorRef.current = INITIAL_HISTORY_CURSOR;
    try {
      localStorage.removeItem(AT_HISTORY_KEY);
    } catch {
      // 存储不可用静默降级
    }
  }, []);

  return {
    transcript,
    isClean: transcript === "",
    sending,
    resetting,
    command,
    setCommand,
    send,
    sendCommand,
    clearTranscript,
    historyPrev: recallPrev,
    historyNext: recallNext,
    history,
    reuseHistory,
    removeHistory,
    clearHistory,
    resetAt,
    rebootModule,
  };
}
