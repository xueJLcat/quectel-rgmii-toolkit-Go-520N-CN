// diag 纯函数集合:探测目标/域名/DNS 服务器校验(镜像后端 page_diag.go 的
// normalizeDiagProbeTarget / diagDomainPattern / normalizeDiagDNSServer 前置口径,
// 前端拦截非法输入,避免 400)+ 结果建模与延迟条换算。

/** 内置公共 DNS 探测目标(设计方案 §6.12;真机红线仅允许这些公网 DNS 目标)。 */
export const BUILTIN_PROBE_TARGETS = ["223.5.5.5", "1.1.1.1", "8.8.8.8"] as const;

/**
 * 镜像后端 normalizeDiagProbeTarget:接受 http(s):// URL 或 host[:port]
 * (无 scheme 时后端自动补 http://);file://、ftp:// 等其他 scheme 一律拒绝。
 * 返回规范化目标;非法返回 null。
 */
export function normalizeProbeTarget(raw: string): string | null {
  const value = String(raw ?? "").trim();
  if (value === "") return null;
  const withScheme = value.includes("://") ? value : `http://${value}`;
  try {
    const url = new URL(withScheme);
    if (url.protocol !== "http:" && url.protocol !== "https:") return null;
    if (!url.hostname) return null;
    return url.toString();
  } catch {
    return null;
  }
}

export function isValidProbeTarget(raw: string): boolean {
  return normalizeProbeTarget(raw) !== null;
}

/** 后端 diagDomainPattern:字母/数字/连字符标签以点连接,允许末尾根点,总长 ≤253。 */
const DOMAIN_PATTERN =
  /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*\.?$/;

export function isValidDomain(raw: string): boolean {
  const value = String(raw ?? "").trim();
  return value !== "" && value.length <= 253 && DOMAIN_PATTERN.test(value);
}

/** 镜像后端 normalizeDiagDNSServer:空 = 系统解析器;host 或 host:port(端口 1-65535)。 */
export function isValidDnsServer(raw: string): boolean {
  const value = String(raw ?? "").trim();
  if (value === "") return true;
  let host = value;
  let port: string | null = null;
  const colonCount = value.split(":").length - 1;
  if (colonCount === 1) {
    const index = value.indexOf(":");
    host = value.slice(0, index);
    port = value.slice(index + 1);
  }
  if (host === "") return false;
  if (port !== null) {
    if (!/^\d+$/.test(port)) return false;
    const portNumber = Number(port);
    if (portNumber < 1 || portNumber > 65535) return false;
  }
  // host:IPv4/IPv6 字面量(宽松)或域名
  return /^[\d.]+$/.test(host) || /^[0-9a-fA-F:]+$/.test(host) || DOMAIN_PATTERN.test(host);
}

/**
 * 探测结果条目:运行时失败(200 + ok:false + error)也是一条正常结果,
 * 展示为失败条目而非页面级错误(错误契约见设计方案 §1.5)。
 */
export interface ProbeOutcome {
  target: string;
  state: "running" | "done";
  ok?: boolean;
  statusCode?: number;
  latencyMs?: number;
  error?: string;
}

/** 延迟横条宽度百分比(相对本轮最大延迟;非零值至少 2% 保证可见,封顶 100%)。 */
export function latencyBarPercent(latencyMs: number | undefined, maxLatencyMs: number): number {
  if (typeof latencyMs !== "number" || !Number.isFinite(latencyMs) || maxLatencyMs <= 0) return 0;
  if (latencyMs <= 0) return 0;
  return Math.max(2, Math.min(100, Math.round((latencyMs / maxLatencyMs) * 100)));
}
