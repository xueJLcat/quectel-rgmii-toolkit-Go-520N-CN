// sysmon 纯函数集(可独立单测):AT+QTEMP 响应解析(行格式不符跳过,全空由 UI 归 error 态)、
// 进程 CPU%/内存% 格式化(旧版 www/js/pages/sysmon.js formatCpu/formatNum 语义)、
// 负载/运行时长文本 → 仪表盘数值折算、更新时间 HH:MM:SS(旧版 fetchMonitor 客户端时间戳)。
export const SYSMON_POLL_MS = 3_000;
export const QTEMP_POLL_MS = 60_000;
export const PROCESS_TOP_N = 20;
// 负载仪表盘满量程:后端未提供核心数,按 4 核满载折算百分比(原始三元组在 caption 展示)
export const LOAD_FULL_SCALE = 4;
// 在线时长仪表盘量程:30 天
export const UPTIME_GAUGE_MAX_DAYS = 30;

export interface QtempReading {
  sensor: string;
  temperature: number;
}

// +QTEMP:"modem-lte-sub6-pa1","40" / +QTEMP: aoss-0,43(容错可选引号与空格;负值如 -273 保留)
const QTEMP_LINE = /^\+QTEMP:\s*"?([^",]+?)"?\s*,\s*"?(-?\d+(?:\.\d+)?)"?\s*$/;

export function parseQtemp(raw: string): QtempReading[] {
  const readings: QtempReading[] = [];
  for (const line of String(raw ?? "").split(/\r?\n/)) {
    const matched = QTEMP_LINE.exec(line.trim());
    if (!matched) continue;
    const sensor = matched[1].trim();
    const temperature = Number(matched[2]);
    if (!sensor || !Number.isFinite(temperature)) continue;
    readings.push({ sensor, temperature });
  }
  return readings;
}

export function formatCpu(value: unknown): string {
  const n = Number(value);
  return Number.isFinite(n) ? `${n.toFixed(1)}%` : "-";
}

export function formatNum(value: unknown): string {
  const n = Number(value);
  return Number.isFinite(n) ? n.toFixed(1) : "-";
}

export function formatRamUsage(
  used: string | undefined,
  total: string | undefined,
  fetchingLabel: string,
): string {
  if (used && used !== "-" && total && total !== "-") return `${used} / ${total}`;
  return fetchingLabel;
}

export function toGaugeNumber(value: unknown): number {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

export function parseLoadAverage(text: string | undefined): number | null {
  const matched = /-?\d+(?:\.\d+)?/.exec(String(text ?? ""));
  if (!matched) return null;
  const value = Number(matched[0]);
  return Number.isFinite(value) ? value : null;
}

export function loadGaugePercent(text: string | undefined): number {
  const load = parseLoadAverage(text);
  if (load === null) return 0;
  return Math.max(0, Math.min(100, (load / LOAD_FULL_SCALE) * 100));
}

// 旧版 index.js parseUptimeParts 同款正则(day/hour/min 与 "H:M," 时钟格式),折算为天数
export function parseUptimeDays(text: string | undefined): number | null {
  const source = String(text ?? "");
  const days = /(\d+)\s+day/i.exec(source);
  const hours = /(\d+)\s+hour/i.exec(source);
  const minutes = /(\d+)\s+min/i.exec(source);
  const clock = /(\d+):(\d+)/.exec(source);
  if (!days && !hours && !minutes && !clock) return null;
  const h = clock ? Number(clock[1]) : hours ? Number(hours[1]) : 0;
  const m = clock ? Number(clock[2]) : minutes ? Number(minutes[1]) : 0;
  return (days ? Number(days[1]) : 0) + h / 24 + m / 1440;
}

export function formatClock(ms: number): string {
  const date = new Date(ms);
  const pad = (value: number): string => String(value).padStart(2, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

/** document.hidden 时返回 false 暂停轮询(refetchInterval 回调形态)。 */
export function visiblePollInterval(ms: number): number | false {
  return typeof document !== "undefined" && document.hidden ? false : ms;
}
