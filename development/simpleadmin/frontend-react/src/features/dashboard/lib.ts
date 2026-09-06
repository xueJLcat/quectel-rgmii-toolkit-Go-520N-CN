// dashboard 纯函数集(可独立单测,无 React 依赖):localStorage 键与读写沿用旧版 www/js/pages/index.js
// (refreshRate / simpleadmin.dashboard.compactNetworkInfo),流量增量测速、uptime/激活 SIM 格式化、
// 信号评估映射与历史趋势取值均与旧版语义等价;visibility 门控轮询间隔供 hooks 的 refetchInterval 回调使用。
import type { StatusTone } from "@/components/common";
import type { DashboardData, HistoryPoint, UptimeParts } from "@/lib/api";

export const REFRESH_RATE_KEY = "refreshRate";
export const NETWORK_COMPACT_KEY = "simpleadmin.dashboard.compactNetworkInfo";
export const REFRESH_RATE_MIN = 2;
export const REFRESH_RATE_MAX = 60;
export const REFRESH_RATE_DEFAULT = 2;
export const HISTORY_POLL_MS = 60_000;
export const AT_CACHE_DEBOUNCE_MS = 800;

export function clampRefreshRate(value: number): number {
  if (!Number.isFinite(value)) return REFRESH_RATE_DEFAULT;
  return Math.max(REFRESH_RATE_MIN, Math.min(REFRESH_RATE_MAX, value));
}

export function readStoredRefreshRate(): number {
  try {
    const stored = Number(localStorage.getItem(REFRESH_RATE_KEY));
    // 与旧版 init 一致:仅接受 >= 2 的有限值,再钳制防手改 localStorage 留下超范围值
    if (Number.isFinite(stored) && stored >= REFRESH_RATE_MIN) return clampRefreshRate(stored);
  } catch {
    // localStorage 不可用(隐私模式等)按默认值
  }
  return REFRESH_RATE_DEFAULT;
}

export function storeRefreshRate(rate: number): void {
  try {
    localStorage.setItem(REFRESH_RATE_KEY, String(rate));
  } catch {
    // 存储不可用时仅内存生效
  }
}

export function readNetworkCompact(): boolean {
  try {
    return localStorage.getItem(NETWORK_COMPACT_KEY) === "1";
  } catch {
    return false;
  }
}

export function storeNetworkCompact(enabled: boolean): void {
  try {
    localStorage.setItem(NETWORK_COMPACT_KEY, enabled ? "1" : "0");
  } catch {
    // 存储不可用时仅内存生效
  }
}

/** document.hidden 时返回 false 暂停轮询(refetchInterval 回调形态)。 */
export function visiblePollInterval(ms: number): number | false {
  return typeof document !== "undefined" && document.hidden ? false : ms;
}

export function toNumber(value: unknown): number {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

// 旧版 humanBytes/humanBytesPerSec(B..PB;<10 保留 1 位小数;负数/NaN → "-")
const BYTE_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];

export function humanBytes(bytes: number): string {
  let value = Number(bytes);
  if (!Number.isFinite(value) || value < 0) return "-";
  let i = 0;
  while (value >= 1024 && i < BYTE_UNITS.length - 1) {
    value /= 1024;
    i += 1;
  }
  const n = i > 0 && value < 10 ? value.toFixed(1) : String(Math.round(value));
  return `${n} ${BYTE_UNITS[i]}`;
}

export function humanBytesPerSec(bytesPerSecond: number): string {
  const value = humanBytes(bytesPerSecond);
  return value === "-" ? "-" : `${value}/s`;
}

// 旧版 formatActiveSimStatus 的 i18n 化描述符:已知枚举走词条,其余原样透出
export type ActiveSimDescriptor =
  | { kind: "activeSlot"; index: string }
  | { kind: "active" }
  | { kind: "inactiveSlot"; index: string }
  | { kind: "inactive" }
  | { kind: "plain"; text: string; index?: string };

export function describeActiveSim(sim?: string, activeSim?: string): ActiveSimDescriptor {
  const state = String(sim ?? "").trim();
  const slot = String(activeSim ?? "").trim();
  const index = slot !== "" && slot !== "-" ? slot.replace(/^卡/, "") : "";
  if (state === "已激活") return index ? { kind: "activeSlot", index } : { kind: "active" };
  if (state === "未激活") return index ? { kind: "inactiveSlot", index } : { kind: "inactive" };
  if (state === "" || state === "-") return { kind: "plain", text: "-" };
  return index ? { kind: "plain", text: state, index } : { kind: "plain", text: state };
}

// 旧版 formatUptimeParts/formatUptimeUnit:天/小时为 0 时省略,分钟恒显示;en 单数去尾 s
export interface UptimeLabels {
  days: string;
  hours: string;
  minutes: string;
}

function formatUptimeUnit(
  value: number | undefined,
  label: string,
  en: boolean,
  force: boolean,
): string {
  const amount = Number(value ?? 0);
  if (!Number.isFinite(amount) || amount < 0) return "";
  if (amount === 0 && !force) return "";
  const unit = en && amount === 1 && label.endsWith("s") ? label.slice(0, -1) : label;
  return `${amount} ${unit}`;
}

export function formatUptimeParts(
  parts: UptimeParts | undefined,
  labels: UptimeLabels,
  unknownLabel: string,
  en: boolean,
): string {
  if (!parts) return unknownLabel;
  const items = [
    formatUptimeUnit(parts.days, labels.days, en, false),
    formatUptimeUnit(parts.hours, labels.hours, en, false),
    formatUptimeUnit(parts.minutes, labels.minutes, en, true),
  ].filter(Boolean);
  return items.length ? items.join(" ") : formatUptimeUnit(0, labels.minutes, en, true);
}

// 服务端 signalAssessment 为固定中文枚举(page_dashboard.go signalQualityGo)
export type AssessmentKey = "excellent" | "good" | "fair" | "poor" | "noSignal";

export interface AssessmentView {
  key?: AssessmentKey;
  tone: StatusTone;
  text?: string;
}

const ASSESSMENT_MAP: Record<string, { key: AssessmentKey; tone: StatusTone }> = {
  优秀: { key: "excellent", tone: "success" },
  良好: { key: "good", tone: "info" },
  一般: { key: "fair", tone: "warning" },
  差: { key: "poor", tone: "danger" },
  无信号: { key: "noSignal", tone: "muted" },
};

export function describeAssessment(value?: string): AssessmentView {
  const text = String(value ?? "").trim();
  const mapped = ASSESSMENT_MAP[text];
  if (mapped) return mapped;
  return { tone: "muted", text: text === "" || text === "-" ? undefined : text };
}

/** 信号质量卡按当前驻留制式过滤:network_mode 含 NR5G → 仅 5G 行,含 LTE → 仅 4G 行;未就绪/未知保留双制式(诚实显示"无")。 */
export function visibleSignalRats(networkMode: string | undefined | null): ("lte" | "nr")[] {
  const mode = String(networkMode ?? "").toUpperCase();
  if (mode.includes("NR5G")) return ["nr"];
  if (mode.includes("LTE")) return ["lte"];
  return ["lte", "nr"];
}

// 旧版 updateTraffic:服务端速率优先,缺失时按两次采样字节增量折算;计数器回绕按 0 增量处理
export interface TrafficSample {
  rx: number;
  tx: number;
  t: number;
}

export interface TrafficView {
  dl: string;
  ul: string;
  dlRate: number | null;
  ulRate: number | null;
  rxTotal: string;
  txTotal: string;
  rxBytes: number | null;
  txBytes: number | null;
}

export const EMPTY_TRAFFIC: TrafficView = {
  dl: "-",
  ul: "-",
  dlRate: null,
  ulRate: null,
  rxTotal: "-",
  txTotal: "-",
  rxBytes: null,
  txBytes: null,
};

export function resolveTraffic(
  data: DashboardData | undefined,
  prev: TrafficSample | null,
  now: number,
): { view: TrafficView; next: TrafficSample | null } {
  if (!data) return { view: EMPTY_TRAFFIC, next: prev };
  const rx = Number(data.nr_rx_bytes);
  const tx = Number(data.nr_tx_bytes);
  const rxValid = Number.isFinite(rx) && rx >= 0;
  const txValid = Number.isFinite(tx) && tx >= 0;
  const rxTotal = data.nr_rx_human || (rxValid ? humanBytes(rx) : "-");
  const txTotal = data.nr_tx_human || (txValid ? humanBytes(tx) : "-");
  const serverDl = data.nr_dl_speed && data.nr_dl_speed !== "-" ? data.nr_dl_speed : "";
  const serverUl = data.nr_ul_speed && data.nr_ul_speed !== "-" ? data.nr_ul_speed : "";
  if (!rxValid || !txValid) {
    return {
      view: {
        dl: serverDl || "-",
        ul: serverUl || "-",
        dlRate: null,
        ulRate: null,
        rxTotal,
        txTotal,
        rxBytes: null,
        txBytes: null,
      },
      next: prev,
    };
  }
  let dlRate: number | null = null;
  let ulRate: number | null = null;
  if (prev) {
    const dt = Math.max(0.001, (now - prev.t) / 1000);
    dlRate = Math.max(0, rx - prev.rx) / dt;
    ulRate = Math.max(0, tx - prev.tx) / dt;
  }
  return {
    view: {
      dl: serverDl || (dlRate !== null ? humanBytesPerSec(dlRate) : "-"),
      ul: serverUl || (ulRate !== null ? humanBytesPerSec(ulRate) : "-"),
      dlRate: serverDl ? null : dlRate,
      ulRate: serverUl ? null : ulRate,
      rxTotal,
      txTotal,
      rxBytes: rx,
      txBytes: tx,
    },
    next: { rx, tx, t: now },
  };
}

// 旧版 historyLatest*/historyTimeRange(趋势图下方 meta 行)
export function historyLatestSignal(points: readonly HistoryPoint[]): string {
  if (!points.length) return "-";
  return `${toNumber(points[points.length - 1]?.sig)}%`;
}

export function historyLatestRxRate(points: readonly HistoryPoint[]): string {
  if (!points.length) return "-";
  return humanBytesPerSec(toNumber(points[points.length - 1]?.rxr));
}

export function historyLatestTxRate(points: readonly HistoryPoint[]): string {
  if (!points.length) return "-";
  return humanBytesPerSec(toNumber(points[points.length - 1]?.txr));
}

export function historyTimeRange(points: readonly HistoryPoint[]): string {
  if (points.length < 2) return "-";
  const fmt = (ts: number | undefined): string => {
    const date = new Date(toNumber(ts) * 1000);
    const hh = String(date.getHours()).padStart(2, "0");
    const mm = String(date.getMinutes()).padStart(2, "0");
    return `${hh}:${mm}`;
  };
  return `${fmt(points[0]?.t)} - ${fmt(points[points.length - 1]?.t)}`;
}
