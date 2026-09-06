// 网络详情页纯函数:字节/速率人性化、剩余租期格式化(对齐旧版 www/js/pages/netdetail.js)、
// 接口速率滚动窗口累积(≤30 点,前端自持,不依赖后端历史)。
import type { NetworkInterfaceStat } from "@/lib/api";

/** 开机保护期/后台未就绪:queryFn 抛此错误进入 pending 语义(保留旧值 + "获取中" chip)。 */
export class NetdetailPendingError extends Error {
  constructor() {
    super("network detail pending");
    this.name = "NetdetailPendingError";
  }
}

/** 字节人性化(对齐旧版 formatBytes):≤0 → "0 B";B 整数,KB 及以上两位小数。 */
export function formatBytes(value: number | undefined): string {
  const bytes = Number(value) || 0;
  if (bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = bytes;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size.toFixed(index === 0 ? 0 : 2)} ${units[index]}`;
}

export function formatRate(value: number | undefined): string {
  return `${formatBytes(value)}/s`;
}

export interface LeaseUnits {
  days: string;
  hours: string;
  minutes: string;
  underOneMinute: string;
}

/**
 * 剩余租期(对齐旧版 formatLease):-1(ARP/静态)或非法 → "—";
 * ≥1天 → "X 天[ X 小时]";≥1小时 → "X 小时[ X 分钟]";≥1分 → "X 分钟";否则"不足1分钟"。
 */
export function formatLease(seconds: number | undefined, units: LeaseUnits): string {
  const value = Number(seconds);
  if (!Number.isFinite(value) || value < 0) return "—";
  const days = Math.floor(value / 86400);
  const hours = Math.floor((value % 86400) / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  if (days > 0) {
    return hours > 0 ? `${days} ${units.days} ${hours} ${units.hours}` : `${days} ${units.days}`;
  }
  if (hours > 0) {
    return minutes > 0
      ? `${hours} ${units.hours} ${minutes} ${units.minutes}`
      : `${hours} ${units.hours}`;
  }
  if (minutes > 0) return `${minutes} ${units.minutes}`;
  return units.underOneMinute;
}

/** 接口状态 chip 色调(对齐旧版:up/unknown 视为正常)。 */
export function interfaceStateTone(state: string | undefined): "success" | "muted" {
  return state === "up" || state === "unknown" ? "success" : "muted";
}

/** 剩余租期 chip 色调:-1(ARP/静态,"—")→ muted,其余 → info。 */
export function leaseTone(seconds: number | undefined): "info" | "muted" {
  const value = Number(seconds);
  return Number.isFinite(value) && value >= 0 ? "info" : "muted";
}

export function hasAddress(value: string | undefined): boolean {
  return !!value && value !== "-";
}

export const RATE_HISTORY_LIMIT = 30;

export interface RateHistory {
  [name: string]: { rx: number[]; tx: number[] };
}

/** 按接口名累积 rxRate/txRate 滚动窗口(≤30 点),不可变更新供 setState。 */
export function appendRatePoint(
  history: RateHistory,
  interfaces: readonly NetworkInterfaceStat[] | undefined,
): RateHistory {
  const next: RateHistory = { ...history };
  for (const iface of interfaces ?? []) {
    const name = iface?.name;
    if (!name) continue;
    const prev = next[name] ?? { rx: [], tx: [] };
    next[name] = {
      rx: [...prev.rx, Number(iface.rxRate) || 0].slice(-RATE_HISTORY_LIMIT),
      tx: [...prev.tx, Number(iface.txRate) || 0].slice(-RATE_HISTORY_LIMIT),
    };
  }
  return next;
}
