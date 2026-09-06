// 重启倒计时状态(zustand):语义对齐 Vue 版 simpleadmin-reboot.js(默认 40s,可被后端覆盖)。
// 倒计时基于真实时间戳 endsAt(而非递减计数)——后台标签页 rAF/interval 被节流,
// 回到前台按 endsAt 重算剩余,不漂移;每秒 tick 由 <RebootOverlay /> 内 interval 驱动。
// endsAtServerMs 供服务端返回绝对截止时间戳的场景(如 IP 透传重启预告)直接采用。
import { create } from "zustand";

export const REBOOT_DEFAULT_SECONDS = 40;

export interface RebootCountdownOptions {
  seconds: number;
  /** 倒计时文案(i18n 后字符串);缺省用 settings:rebootingPleaseWaitDoNotCloseThisPage */
  label?: string;
  /** 服务端绝对截止时间戳(ms);给出则忽略 seconds 推算 */
  endsAtServerMs?: number;
}

export interface RebootState {
  countdownActive: boolean;
  /** 倒计时结束的 epoch ms(真实时间戳基准) */
  endsAt: number;
  /** 总秒数(CountdownGauge 的 total) */
  total: number;
  label: string | null;
  startCountdown: (options: RebootCountdownOptions) => void;
  closeCountdown: () => void;
}

function normalizeTotal(seconds: number): number {
  return Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds) : REBOOT_DEFAULT_SECONDS;
}

export const useRebootStore = create<RebootState>()((set) => ({
  countdownActive: false,
  endsAt: 0,
  total: REBOOT_DEFAULT_SECONDS,
  label: null,
  startCountdown: ({ seconds, label, endsAtServerMs }) => {
    const total = normalizeTotal(seconds);
    const endsAt =
      typeof endsAtServerMs === "number" && Number.isFinite(endsAtServerMs) && endsAtServerMs > 0
        ? endsAtServerMs
        : Date.now() + total * 1000;
    set({ countdownActive: true, endsAt, total, label: label ?? null });
  },
  closeCountdown: () => set({ countdownActive: false, label: null }),
}));
