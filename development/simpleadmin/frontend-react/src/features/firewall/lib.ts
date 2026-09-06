// firewall 纯函数集合:规则归一化/校验矩阵/暂存 diff/保存参数拼装/字节与计数格式化/DMZ 判别。
// 行为对齐旧 www/js/pages/firewall.js(去重、排序、hasChanges、saveRules 参数、formatBytes/Count、
// resolveDmzMode、untrusted 状态判别)与 Go page_firewall.go(validateFirewallRules 冲突与 64 上限、
// normalizeFirewallFwdRule 的 IPv4/协议/(extPort,proto) 唯一/32 上限)。无 DOM 依赖,可独立单测。
import type { FirewallRule, NetworkConfigResponse } from "@/lib/api";

export const MAX_PORT_RULES = 64;
export const MAX_FWD_RULES = 32;

export type RuleAction = "block" | "accept";

export interface PortRule {
  port: string;
  action: RuleAction;
}

export function normalizeRules(raw: FirewallRule[] | undefined): PortRule[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((rule) => ({
    port: String(rule?.port ?? ""),
    action: rule?.action === "accept" ? "accept" : "block",
  }));
}

export function sortRulesByPort(rules: readonly PortRule[]): PortRule[] {
  return rules.slice().sort((a, b) => Number(a.port) - Number(b.port));
}

export function ruleKey(rule: PortRule): string {
  return `${rule.action}:${rule.port}`;
}

/** 暂存区与服务端规则是否等价(排序后逐项对比,对齐旧 hasChanges)。 */
export function rulesEqual(a: readonly PortRule[], b: readonly PortRule[]): boolean {
  if (a.length !== b.length) return false;
  const left = sortRulesByPort(a);
  const right = sortRulesByPort(b);
  return left.every((rule, index) => ruleKey(rule) === ruleKey(right[index]));
}

export interface RuleDiff {
  added: PortRule[];
  removed: PortRule[];
}

/** 暂存 diff:新增 = 草稿有而服务端无;删除 = 服务端有而草稿无(按 action:port 键)。 */
export function computeRuleDiff(server: readonly PortRule[], draft: readonly PortRule[]): RuleDiff {
  const serverKeys = new Set(server.map(ruleKey));
  const draftKeys = new Set(draft.map(ruleKey));
  return {
    added: draft.filter((rule) => !serverKeys.has(ruleKey(rule))),
    removed: server.filter((rule) => !draftKeys.has(ruleKey(rule))),
  };
}

export type AddRuleError = "invalidPort" | "duplicate" | "conflict" | "limit";

/**
 * 新增规则校验矩阵:端口必须为 1-65535 的纯数字;同端口同动作 = 重复;
 * 同端口反动作 = 阻止/放行冲突(对齐后端 validateFirewallRules);总数不超过上限。
 */
export function validateAddRule(
  rules: readonly PortRule[],
  rawPort: string,
  action: RuleAction,
  max = MAX_PORT_RULES,
): AddRuleError | null {
  const input = String(rawPort ?? "").trim();
  if (!/^\d+$/.test(input)) return "invalidPort";
  const portNumber = Number(input);
  if (portNumber < 1 || portNumber > 65535) return "invalidPort";
  const port = String(portNumber);
  const existing = rules.find((rule) => rule.port === port);
  if (existing) return existing.action === action ? "duplicate" : "conflict";
  if (rules.length >= max) return "limit";
  return null;
}

/** save 动作参数:阻止与放行分别逗号拼接(对齐旧 saveRules,block = 非 accept)。 */
export function buildSaveParams(rules: readonly PortRule[]): {
  block_ports: string;
  accept_ports: string;
} {
  const blockPorts = rules.filter((rule) => rule.action !== "accept").map((rule) => rule.port);
  const acceptPorts = rules.filter((rule) => rule.action === "accept").map((rule) => rule.port);
  return { block_ports: blockPorts.join(","), accept_ports: acceptPorts.join(",") };
}

export function isValidIPv4(ip: string): boolean {
  const parts = String(ip ?? "")
    .trim()
    .split(".");
  if (parts.length !== 4) return false;
  return parts.every((part) => {
    if (!/^\d+$/.test(part)) return false;
    const num = Number.parseInt(part, 10);
    return num >= 0 && num <= 255;
  });
}

/** 字节人性化(对齐旧 formatBytes:<1024 直接 B,K/M/G 保留 1 位小数)。 */
export function formatBytes(n: number): string {
  const value = Number(n);
  if (!Number.isFinite(value)) return "-";
  if (value < 1024) return `${value} B`;
  const units = ["K", "M", "G"];
  let scaled = value;
  for (let i = 0; i < units.length; i++) {
    scaled /= 1024;
    if (scaled < 1024 || i === units.length - 1) {
      return `${scaled.toFixed(1)} ${units[i]}B`;
    }
  }
  return `${scaled.toFixed(1)} GB`;
}

/** 计数千分位(对齐旧 formatCount)。 */
export function formatCount(n: number): string {
  const value = Number(n);
  if (!Number.isFinite(value)) return "-";
  return String(value).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

// ---------------------------------------------------------------------------
// 端口转发(DNAT)
// ---------------------------------------------------------------------------

export type FwdProto = "tcp" | "udp";

export interface FwdRule {
  extPort: number;
  intIP: string;
  intPort: number;
  proto: FwdProto;
  enabled: boolean;
}

export interface FwdRuleDraftInput {
  extPort: string;
  intIP: string;
  intPort: string;
  proto: string;
}

export function normalizeFwdRules(raw: unknown): FwdRule[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((item) => {
    const entry = (item ?? {}) as Partial<Record<keyof FwdRule, unknown>>;
    // 对齐后端 normalizeFirewallFwdRule:协议大小写与首尾空白归一化
    const proto =
      String(entry.proto ?? "")
        .trim()
        .toLowerCase() === "udp"
        ? "udp"
        : "tcp";
    return {
      extPort: Number(entry.extPort ?? 0),
      intIP: String(entry.intIP ?? "").trim(),
      intPort: Number(entry.intPort ?? 0),
      proto,
      enabled: entry.enabled === true,
    };
  });
}

export function fwdKey(rule: FwdRule): string {
  return `${rule.proto}:${rule.extPort}`;
}

export interface FwdDiff {
  added: FwdRule[];
  removed: FwdRule[];
  changed: FwdRule[];
}

/** 转发规则 diff:键 = proto:extPort;同键但 intIP/intPort/enabled 不同计为修改。 */
export function computeFwdDiff(server: readonly FwdRule[], draft: readonly FwdRule[]): FwdDiff {
  const serverByKey = new Map(server.map((rule) => [fwdKey(rule), rule]));
  const draftByKey = new Map(draft.map((rule) => [fwdKey(rule), rule]));
  const added = draft.filter((rule) => !serverByKey.has(fwdKey(rule)));
  const removed = server.filter((rule) => !draftByKey.has(fwdKey(rule)));
  const changed = draft.filter((rule) => {
    const before = serverByKey.get(fwdKey(rule));
    if (!before) return false;
    return (
      before.intIP !== rule.intIP ||
      before.intPort !== rule.intPort ||
      before.enabled !== rule.enabled
    );
  });
  return { added, removed, changed };
}

export type AddFwdError =
  "invalidExtPort" | "invalidIp" | "invalidIntPort" | "invalidProto" | "duplicate" | "limit";

/** 新增转发规则校验矩阵(对齐后端 normalizeFirewallFwdRule + parseFirewallFwdRules)。 */
export function validateAddFwd(
  rules: readonly FwdRule[],
  input: FwdRuleDraftInput,
  max = MAX_FWD_RULES,
): AddFwdError | null {
  const extRaw = String(input.extPort ?? "").trim();
  if (!/^\d+$/.test(extRaw) || Number(extRaw) < 1 || Number(extRaw) > 65535) {
    return "invalidExtPort";
  }
  if (!isValidIPv4(String(input.intIP ?? ""))) return "invalidIp";
  const intRaw = String(input.intPort ?? "").trim();
  if (!/^\d+$/.test(intRaw) || Number(intRaw) < 1 || Number(intRaw) > 65535) {
    return "invalidIntPort";
  }
  if (input.proto !== "tcp" && input.proto !== "udp") return "invalidProto";
  const extPort = Number(extRaw);
  if (rules.some((rule) => rule.proto === input.proto && rule.extPort === extPort)) {
    return "duplicate";
  }
  if (rules.length >= max) return "limit";
  return null;
}

export function toFwdRule(input: FwdRuleDraftInput): FwdRule {
  return {
    extPort: Number(String(input.extPort).trim()),
    intIP: String(input.intIP).trim(),
    intPort: Number(String(input.intPort).trim()),
    proto: input.proto === "udp" ? "udp" : "tcp",
    enabled: true,
  };
}

/** fwd_save 的 rules 参数:JSON 数组字符串(字段与后端 firewallFwdRule 一致)。 */
export function fwdSaveParam(rules: readonly FwdRule[]): string {
  return JSON.stringify(
    rules.map((rule) => ({
      extPort: rule.extPort,
      intIP: rule.intIP,
      intPort: rule.intPort,
      proto: rule.proto,
      enabled: rule.enabled,
    })),
  );
}

// ---------------------------------------------------------------------------
// DMZ
// ---------------------------------------------------------------------------

/** 对齐旧 resolveDmzMode:有有效 IP 即为启用,否则看 dmzMode 字段。 */
export function resolveDmzMode(mode: unknown, ip: string): "0" | "1" {
  const currentMode = String(mode ?? "").trim();
  const currentIp = String(ip ?? "").trim();
  if (currentIp && currentIp !== "-") return "1";
  return currentMode === "1" ? "1" : "0";
}

/**
 * 对齐旧 fetchDmzStatus 的 untrusted 判别:开机保护期全默认值(所有开关 false、
 * USB 模式未知、DMZ/网关 IP 全空)视为不可信,继续退避重试而不是渲染假状态。
 */
export function isUntrustedDmzStatus(data: NetworkConfigResponse | undefined): boolean {
  if (!data) return false;
  return (
    data.ipPassStatus === false &&
    data.DNSV4ProxyStatus === false &&
    data.DNSV6ProxyStatus === false &&
    data.currentUsbNetMode === "未知" &&
    String(data.dmzIP ?? "") === "" &&
    String(data.lanGwIp ?? "") === ""
  );
}

/** pending/不可信状态的可重试错误:queryFn 抛出后走 React Query 指数退避(对齐旧 8 次重试)。 */
export class PendingStatusError extends Error {
  constructor() {
    super("status pending");
    this.name = "PendingStatusError";
  }
}

/**
 * 提取 API 错误文案:ApiError(带 body)解析 JSON error 字段;普通 Error 用 message;
 * 都拿不到时用 fallback(调用方经 i18n 传入)。
 */
export function apiErrorMessage(err: unknown, fallback: string): string {
  const candidate = err as { body?: unknown; message?: unknown } | null;
  if (candidate && typeof candidate.body === "string" && candidate.body.trim()) {
    try {
      const parsed = JSON.parse(candidate.body) as { error?: unknown };
      if (typeof parsed.error === "string" && parsed.error) return parsed.error;
    } catch {
      // body 非 JSON,退回 message
    }
  }
  if (candidate && typeof candidate.message === "string" && candidate.message) {
    return candidate.message;
  }
  return fallback;
}
