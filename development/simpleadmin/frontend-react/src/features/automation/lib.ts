// automation 纯函数集合:数值范围校验(对齐旧 automation.js 与后端 get/set 契约)、HH:MM、
// NTP 服务器(镜像旧 validNtpServer / 后端 normalizeTimeSyncServer 的前置校验)、
// 探测目标行(镜像后端 normalizeWatchdogTarget:host / host:port / [IPv6]:port / IPv6 字面量)。
import type { ApiParams } from "@/lib/api";

export const MAX_WATCHDOG_TARGETS = 4;

export const WATCHDOG_RANGES = {
  failThreshold: { min: 1, max: 60 },
  cooldownMinutes: { min: 1, max: 1440 },
  checkIntervalMinutes: { min: 1, max: 30 },
} as const;

export const TIME_SYNC_RANGE = { min: 1, max: 1440 } as const;

/** 对齐旧版:Number() 后要求整数且在范围内(空串 → 0 → 必然低于 min)。 */
export function isIntegerInRange(raw: unknown, min: number, max: number): boolean {
  const value = Number(raw);
  return Number.isInteger(value) && value >= min && value <= max;
}

export function isValidRebootTime(value: string): boolean {
  return /^([01]\d|2[0-3]):[0-5]\d$/.test(String(value ?? "").trim());
}

/**
 * 镜像后端 normalizeTimeSyncServer 规则的旧版前置校验:非法服务器后端会静默回退
 * 缺省源并返回成功,前端不拦会显示"已保存"但值被偷换。空值合法(后端用缺省源);
 * 允许 ntp://、ntps:// 前缀;主体不得含空白与 / # ? : 等字符,长度不超过 253。
 */
export function isValidNtpServer(value: string): boolean {
  let server = String(value ?? "").trim();
  if (server === "") return true;
  server = server.replace(/^ntps?:\/\//, "");
  if (server.length === 0 || server.length > 253) return false;
  return !/[ \t/#?:]/.test(server);
}

export function parseTargetLines(text: string): string[] {
  return String(text ?? "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line !== "");
}

/** 镜像后端 normalizeWatchdogTarget:host[:port](缺省 53)、[IPv6]:port、IPv6 字面量。 */
export function isValidTargetLine(raw: string): boolean {
  const value = String(raw ?? "").trim();
  if (value === "") return false;
  if (/[\s/#?]/.test(value)) return false;
  let host = value;
  let port = "53";
  if (value.startsWith("[")) {
    // 注意:] 在字符类中必须转义,否则 [^] 会被解析为"任意字符"空否定类
    const matched = /^\[([^\]\s]+)\]:(\d+)$/.exec(value);
    if (!matched) return false;
    host = matched[1];
    port = matched[2];
  } else if ((value.match(/:/g) ?? []).length > 1) {
    // 不带端口的 IPv6 字面量
    if (!/^[0-9a-fA-F:]+$/.test(value)) return false;
    host = value;
  } else if (value.includes(":")) {
    const index = value.indexOf(":");
    host = value.slice(0, index);
    port = value.slice(index + 1);
  }
  if (host === "") return false;
  if (!/^\d+$/.test(port)) return false;
  const portNumber = Number(port);
  return portNumber >= 1 && portNumber <= 65535;
}

export type TargetsError = { reason: "tooMany" } | { reason: "invalidLine"; line: string } | null;

export function validateTargets(text: string, max = MAX_WATCHDOG_TARGETS): TargetsError {
  const lines = parseTargetLines(text);
  if (lines.length > max) return { reason: "tooMany" };
  for (const line of lines) {
    if (!isValidTargetLine(line)) return { reason: "invalidLine", line };
  }
  return null;
}

export interface WatchdogForm {
  enabled: boolean;
  failThreshold: string;
  cooldownMinutes: string;
  checkIntervalMinutes: string;
  targets: string;
  actionPolicy: string;
}

export type WatchdogValidationError =
  | "failThreshold"
  | "cooldownMinutes"
  | "checkIntervalMinutes"
  | "targetsTooMany"
  | "targetLineInvalid"
  | null;

export function validateWatchdogForm(form: WatchdogForm): WatchdogValidationError {
  const { failThreshold, cooldownMinutes, checkIntervalMinutes } = WATCHDOG_RANGES;
  if (!isIntegerInRange(form.failThreshold, failThreshold.min, failThreshold.max)) {
    return "failThreshold";
  }
  if (!isIntegerInRange(form.cooldownMinutes, cooldownMinutes.min, cooldownMinutes.max)) {
    return "cooldownMinutes";
  }
  if (
    !isIntegerInRange(form.checkIntervalMinutes, checkIntervalMinutes.min, checkIntervalMinutes.max)
  ) {
    return "checkIntervalMinutes";
  }
  const targets = validateTargets(form.targets);
  if (targets?.reason === "tooMany") return "targetsTooMany";
  if (targets?.reason === "invalidLine") return "targetLineInvalid";
  return null;
}

/** 对齐旧 setWatchdog 调用:enabled '1'/'0',数值转字符串,间隔参数名为 checkInterval。 */
export function buildWatchdogPayload(form: WatchdogForm): ApiParams {
  return {
    enabled: form.enabled ? "1" : "0",
    failThreshold: String(Number(form.failThreshold)),
    cooldownMinutes: String(Number(form.cooldownMinutes)),
    checkInterval: String(Number(form.checkIntervalMinutes)),
    targets: String(form.targets ?? ""),
    actionPolicy: form.actionPolicy === "escalate" ? "escalate" : "reboot",
  };
}

export function buildSchedulerPayload(enabled: boolean, rebootTime: string): ApiParams {
  return {
    rebootEnabled: enabled ? "1" : "0",
    rebootTime: String(rebootTime ?? "").trim(),
  };
}

export type TimeSyncValidationError = "intervalMinutes" | "server" | null;

export function validateTimeSyncForm(
  intervalMinutes: string,
  server: string,
): TimeSyncValidationError {
  if (!isIntegerInRange(intervalMinutes, TIME_SYNC_RANGE.min, TIME_SYNC_RANGE.max)) {
    return "intervalMinutes";
  }
  if (!isValidNtpServer(server)) return "server";
  return null;
}

export function buildTimeSyncPayload(
  enabled: boolean,
  intervalMinutes: string,
  server: string,
): ApiParams {
  return {
    enabled: enabled ? "1" : "0",
    intervalMinutes: String(Number(intervalMinutes)),
    server: String(server ?? "").trim(),
  };
}

/** 对齐旧 lastActionText 的动作类型 → i18n 键映射;未知类型返回 null(仅显示时间)。 */
export function watchdogActionLabelKey(actionType: string | undefined): string | null {
  if (actionType === "radio") return "radioReRegistration";
  if (actionType === "reboot") return "moduleReboot";
  return null;
}
