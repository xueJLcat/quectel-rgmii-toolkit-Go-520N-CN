// settings 纯函数集合:IMEI/TTL/密码校验(对齐旧 settings.js 严格口径)、后端密码错误映射、
// 关机送达探测(旧 startPoweroff 的 waitDevicePoweredOff / confirmPoweroffByProbe 双分支,
// probe/sleep 可注入以便单测,不依赖真实定时器与网络)。

/** 对齐旧 openImeiModal:写入必须为 15 位纯数字。 */
export function isValidImeiInput(raw: string): boolean {
  return /^\d{15}$/.test(String(raw ?? "").trim());
}

/** 对齐旧 fetchSystemStatus:14-17 位数字才作为当前 IMEI 展示。 */
export function isDisplayableImei(raw: unknown): boolean {
  return /^\d{14,17}$/.test(String(raw ?? "").trim());
}

/**
 * 严格 TTL 校验(对齐旧 setTTL):仅接受 1-3 位纯数字且 0-255。
 * parseInt 会把 "0x10" 解析成 0(=禁用 TTL)、"12abc" 解析成 12,静默放行会造成
 * 与输入不符的生效值,必须整体拒绝。
 */
export function validateTtlInput(raw: string): number | null {
  const text = String(raw ?? "").trim();
  if (!/^\d{1,3}$/.test(text)) return null;
  const value = Number(text);
  return value >= 0 && value <= 255 ? value : null;
}

export const PASSWORD_MIN_LENGTH = 8;
export const PASSWORD_MAX_LENGTH = 128;

export type PasswordFormError = "empty" | "mismatch" | "lineBreaks" | "tooLong" | "tooShort" | null;

/** 密码前端拦截:非空 → 一致性 → 换行/长度上限(后端硬约束)→ 强度(至少 8 位)。 */
export function validatePasswordForm(form: {
  current: string;
  next: string;
  confirm: string;
}): PasswordFormError {
  if (!form.current || !form.next) return "empty";
  if (form.next !== form.confirm) return "mismatch";
  if (/[\r\n]/.test(form.next)) return "lineBreaks";
  if (form.next.length > PASSWORD_MAX_LENGTH) return "tooLong";
  if (form.next.length < PASSWORD_MIN_LENGTH) return "tooShort";
  return null;
}

export interface PasswordErrorInfo {
  /** 命中映射表时的 settings ns 词条键 */
  key: string | null;
  /** 后端原始 error 文本(未命中映射时直接展示) */
  raw: string;
}

/** 对齐旧 changeLoginPassword 的错误映射:HTTP 403 / body.error → 可读文案。 */
export function passwordErrorInfo(
  status: number | undefined,
  body: string | undefined,
): PasswordErrorInfo {
  let message = "";
  if (typeof body === "string" && body.trim()) {
    try {
      const parsed = JSON.parse(body) as { error?: unknown };
      if (typeof parsed.error === "string") message = parsed.error;
    } catch {
      message = "";
    }
  }
  if (status === 403 || message === "current password incorrect") {
    return { key: "currentPasswordIsIncorrect", raw: message };
  }
  switch (message) {
    case "new password is empty":
      return { key: "newPasswordCannotBeEmpty", raw: message };
    case "new password must not contain line breaks":
      return { key: "passwordNoLineBreaks", raw: message };
    case "new password is too long":
      return { key: "passwordTooLong", raw: message };
    case "password confirmation mismatch":
      return { key: "passwordConfirmationMismatch", raw: message };
    default:
      return { key: null, raw: message };
  }
}

/** pending(开机保护期)可重试错误:按 query 配置退避重试,绝不显示假空态。 */
export class PendingStatusError extends Error {
  constructor() {
    super("status pending");
    this.name = "PendingStatusError";
  }
}

// ---------------------------------------------------------------------------
// 关机送达探测(对齐旧 startPoweroff)
// ---------------------------------------------------------------------------

export const POWER_OFF_PROBE = {
  /** 成功路径首探延迟与轮询间隔(旧:2000ms) */
  intervalMs: 2000,
  /** 成功路径最多轮询次数(旧:8 次,超时仍未断电也照常提示已关机) */
  maxAttempts: 8,
  /** 传输失败路径的一次性探测延迟(旧:6000ms) */
  fallbackDelayMs: 6000,
  /** 遮罩倒计时秒数:覆盖成功路径探测预算(2s + 8×2s ≈ 18s) */
  overlaySeconds: 20,
} as const;

/** 探测免会话的登录页:可达 = 设备仍在运行,网络错误 = 已断电不可达。 */
export async function probeDeviceAlive(fetchImpl: typeof fetch = fetch): Promise<boolean> {
  try {
    await fetchImpl("/login.html", { cache: "no-store" });
    return true;
  } catch {
    return false;
  }
}

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export interface PoweroffProbeDeps {
  probe: () => Promise<boolean>;
  sleepFn?: (ms: number) => Promise<void>;
  intervalMs?: number;
  maxAttempts?: number;
  fallbackDelayMs?: number;
  /** 组件卸载时返回 true,终止循环避免卸载后 setState */
  isCancelled?: () => boolean;
}

/**
 * 命令送达后的轮询探测:断电是异步过程,设备还会继续运行数秒,轮询直到不可达;
 * 超时仍未断电也按"已关机"返回(设备随后会完成断电),对齐旧 waitDevicePoweredOff。
 */
export async function waitForDevicePoweredOff(
  deps: PoweroffProbeDeps,
): Promise<"off" | "cancelled"> {
  const sleepFn = deps.sleepFn ?? sleep;
  const intervalMs = deps.intervalMs ?? POWER_OFF_PROBE.intervalMs;
  const maxAttempts = deps.maxAttempts ?? POWER_OFF_PROBE.maxAttempts;
  await sleepFn(intervalMs);
  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    if (deps.isCancelled?.()) return "cancelled";
    const alive = await deps.probe();
    if (!alive) return "off";
    if (attempt < maxAttempts - 1) await sleepFn(intervalMs);
  }
  if (deps.isCancelled?.()) return "cancelled";
  return "off";
}

/**
 * 传输中断无法区分"设备正在断电"与"命令未送达":延时探测一次,
 * 设备仍可达 = 未送达(alive),不可达 = 已关机(off)。对齐旧 confirmPoweroffByProbe。
 */
export async function confirmPoweroffByProbe(
  deps: PoweroffProbeDeps,
): Promise<"off" | "alive" | "cancelled"> {
  const sleepFn = deps.sleepFn ?? sleep;
  await sleepFn(deps.fallbackDelayMs ?? POWER_OFF_PROBE.fallbackDelayMs);
  if (deps.isCancelled?.()) return "cancelled";
  const alive = await deps.probe();
  return alive ? "alive" : "off";
}
