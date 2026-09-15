// 网络设置页纯函数:IPv4/IPv6/MAC 校验(对齐旧版 www/js/pages/netconfig.js 与后端
// normalizeMACAddress/cleanIP 契约)、IP 透传/USB 网卡模式参数组装、上游 DNS 行校验(行号语义,
// 词条走 common:dnsLineInvalid 插值)、LAN IP 表单校验(对齐旧版 setLANIP 规则)、
// status 响应"全默认值不可信"判定(对齐旧版 fetchStatus untrusted 逻辑)。
import type { MacBinding, NetworkConfigResponse } from "@/lib/api";
import { REBOOT_DEFAULT_SECONDS } from "@/stores/reboot";

/** 开机保护期/AT 读取失败/全默认值:queryFn 抛此错误进入 pending 语义(保留旧值 + 退避重试)。 */
export class NetconfigPendingError extends Error {
  constructor() {
    super("network config status pending");
    this.name = "NetconfigPendingError";
  }
}

/** 后端 currentUsbNetMode 的"未知"为 Go 侧固定中文字面量(page_network_config.go)。 */
export const UNKNOWN_USB_MODE_RAW = "未知";

export const USB_NET_MODES = ["RMNET", "ECM", "MBIM", "RNDIS"] as const;
export const IP_PASSTHROUGH_MODES = ["ETH", "USB"] as const;
export const MAC_BIND_LIMIT = 10;
export const DNS_UPSTREAM_LIMIT = 4;

export function validIPv4(ip: string): boolean {
  const parts = String(ip ?? "")
    .trim()
    .split(".");
  if (parts.length !== 4) return false;
  return parts.every((part) => {
    if (!/^\d+$/.test(part)) return false;
    const num = parseInt(part, 10);
    return num >= 0 && num <= 255;
  });
}

/** 对齐旧版 validIPv6:仅十六进制与冒号、至多一个 "::"、组数/组长按压缩规则校验。 */
export function validIPv6(ip: string): boolean {
  const value = String(ip ?? "").trim();
  if (!value.includes(":")) return false;
  if (!/^[0-9A-Fa-f:]+$/.test(value)) return false;
  if (value.includes(":::")) return false;
  const compressedCount = (value.match(/::/g) || []).length;
  if (compressedCount > 1) return false;
  const groups = value.split(":").filter((group) => group !== "");
  if (groups.length < 1 || groups.length > 8) return false;
  if (compressedCount === 0 && groups.length !== 8) return false;
  if (compressedCount === 1 && groups.length > 7) return false;
  return groups.every((group) => group.length <= 4);
}

/**
 * MAC 归一化(与后端 normalizeMACAddress 一致):接受连字符分隔与 12 位紧凑式,
 * 统一为冒号分隔大写;非法返回 null。
 */
export function normalizeMac(mac: string): string | null {
  let value = String(mac ?? "")
    .trim()
    .replace(/-/g, ":");
  if (/^[0-9A-Fa-f]{12}$/.test(value)) {
    const groups = value.match(/.{2}/g);
    if (groups) value = groups.join(":");
  }
  return /^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$/.test(value) ? value.toUpperCase() : null;
}

export function validMAC(mac: string): boolean {
  return normalizeMac(mac) !== null;
}

/** IP 透传启用参数组装:模式必须是 ETH/USB,否则 null(调用方提示"未指定")。 */
export function ipPassthroughEnableParams(mode: string): { enabled: "1"; mode: string } | null {
  const value = String(mode ?? "")
    .trim()
    .toUpperCase();
  if (!(IP_PASSTHROUGH_MODES as readonly string[]).includes(value)) return null;
  return { enabled: "1", mode: value };
}

/** IP 透传禁用参数(后端 enabled=0 走后台重启流程,无需 mode)。 */
export function ipPassthroughDisableParams(): { enabled: "0" } {
  return { enabled: "0" };
}

/** USB 网卡模式参数组装:非法模式返回 null。 */
export function usbNetParams(mode: string): { mode: string } | null {
  const value = String(mode ?? "")
    .trim()
    .toUpperCase();
  if (!(USB_NET_MODES as readonly string[]).includes(value)) return null;
  return { mode: value };
}

/** USB 模式展示文本:后端"未知"/空 → i18n 占位,其余原样(RMNET/ECM/MBIM/RNDIS 为专名)。 */
export function usbNetModeText(mode: string | undefined, unknownText: string): string {
  const value = String(mode ?? "").trim();
  if (value === "" || value === UNKNOWN_USB_MODE_RAW) return unknownText;
  return value;
}

export interface DnsLinesResult {
  /** 去空、按出现顺序去重(大小写不敏感)后的服务器列表 */
  servers: string[];
  /** 第一个非法行的行号(1 起,按非空行计数);全部合法为 null */
  invalidLine: number | null;
}

/**
 * 上游 DNS 行校验矩阵:每行 IPv4/IPv6(后端 validateDNSServerList 用 net.ParseIP,
 * hostname 会被 400 拒绝,前端同口径拦截);行号对齐旧版"第 N 行 DNS 服务器格式无效"。
 */
export function parseDnsLines(lines: readonly string[]): DnsLinesResult {
  const seen = new Set<string>();
  const servers: string[] = [];
  let lineNumber = 0;
  for (const raw of lines) {
    const value = String(raw ?? "").trim();
    if (!value) continue;
    lineNumber += 1;
    if (!validIPv4(value) && !validIPv6(value)) return { servers, invalidLine: lineNumber };
    const dedupeKey = value.toLowerCase();
    if (seen.has(dedupeKey)) continue;
    seen.add(dedupeKey);
    servers.push(value);
  }
  return { servers, invalidLine: null };
}

export interface LanIpForm {
  gateway: string;
  /** DHCP 起始地址末段(1-254) */
  start: string;
  /** DHCP 结束地址末段(1-254) */
  end: string;
}

export type LanIpValidation =
  | { valid: true; startIp: string; endIp: string; gateway: string }
  | { valid: false; errorKey: string };

/**
 * LAN IP 表单校验(对齐旧版 setLANIP):网关合法 IPv4;起止为 1-254 整数且 start ≤ end;
 * 网关末段 1-254 且不得落在 DHCP 池内;起止 IP 用网关前三段拼接(同段),
 * 用校验后的数值拼接避免 " 100"/"1e2" 原样进 IP 被后端拒绝。
 */
export function validateLanIpForm(form: LanIpForm): LanIpValidation {
  const gateway = String(form.gateway ?? "").trim();
  const startRaw = String(form.start ?? "").trim();
  const endRaw = String(form.end ?? "").trim();
  if (!gateway || !startRaw || !endRaw) {
    return {
      valid: false,
      errorKey: "enterAValidGatewayIpAddressStartAddressAndEndAddress",
    };
  }
  if (!validIPv4(gateway)) return { valid: false, errorKey: "invalidGatewayIpAddressFormat" };
  const startNum = Number(startRaw);
  const endNum = Number(endRaw);
  if (
    !Number.isInteger(startNum) ||
    startNum < 1 ||
    startNum > 254 ||
    !Number.isInteger(endNum) ||
    endNum < 1 ||
    endNum > 254
  ) {
    return { valid: false, errorKey: "theStartEndAddressMustBeAnIntegerBetween1And254" };
  }
  if (startNum > endNum) {
    return { valid: false, errorKey: "startAddressCannotBeGreaterThanEndAddress" };
  }
  const gwLastOctet = Number(gateway.split(".")[3]);
  if (gwLastOctet < 1 || gwLastOctet > 254) {
    return { valid: false, errorKey: "lastOctetOfGatewayIpMustBe1254" };
  }
  if (gwLastOctet >= startNum && gwLastOctet <= endNum) {
    return { valid: false, errorKey: "dhcpPoolMustNotIncludeGateway" };
  }
  const prefix = gateway.split(".").slice(0, 3).join(".");
  return { valid: true, startIp: `${prefix}.${startNum}`, endIp: `${prefix}.${endNum}`, gateway };
}

export type MacBindValidation =
  { valid: true; mac: string; ip: string } | { valid: false; errorKey: string };

/**
 * 静态绑定输入校验(对齐旧版 addMacBinding):MAC 格式(宽松,后端归一)、IPv4 格式、
 * 本地查重(MAC 大小写不敏感 / IP 精确)、总数 ≤10。
 */
export function validateMacBind(
  macInput: string,
  ipInput: string,
  existing: readonly MacBinding[],
): MacBindValidation {
  const mac = String(macInput ?? "").trim();
  const ip = String(ipInput ?? "").trim();
  if (!validMAC(mac)) return { valid: false, errorKey: "invalidMacAddressFormat" };
  if (!validIPv4(ip)) return { valid: false, errorKey: "invalidIpAddressFormat" };
  const normalized = normalizeMac(mac);
  const bindings = existing ?? [];
  if (bindings.some((entry) => normalizeMac(String(entry?.mac ?? "")) === normalized)) {
    return { valid: false, errorKey: "thisMacAddressIsAlreadyBound" };
  }
  if (bindings.some((entry) => entry?.ip === ip)) {
    return { valid: false, errorKey: "thisIpAddressIsAlreadyBound" };
  }
  if (bindings.length >= MAC_BIND_LIMIT) {
    return { valid: false, errorKey: "youCanAddUpTo10StaticBindings" };
  }
  return { valid: true, mac, ip };
}

/**
 * status 响应可信度(对齐旧版 untrusted 判定):全部字段为默认值时视为读取失败——
 * 曾加载成功则沿用旧值,否则按 pending 退避重试;dnsV4QueryFailed=true 时
 * V4=false 是预期值,不作为"未加载"证据。
 */
export function isUntrustedStatus(data: NetworkConfigResponse): boolean {
  const dmzIpValue = String(data.dmzIP ?? "");
  const lanGwValue = String(data.lanGwIp ?? "");
  const dnsV4LooksDefault = data.dnsV4QueryFailed === true ? true : data.DNSV4ProxyStatus === false;
  return (
    data.ipPassStatus === false &&
    dnsV4LooksDefault &&
    data.DNSV6ProxyStatus === false &&
    data.currentUsbNetMode === UNKNOWN_USB_MODE_RAW &&
    dmzIpValue === "" &&
    lanGwValue === ""
  );
}

export interface RebootNotice {
  countdownSeconds: number;
}

/**
 * 重启预告解析(settingsRebootNoticeResponse 契约):reboot/rebooting 任一为 true 即生效,
 * 倒计时秒数取 rebootCountdownSeconds,非法回退默认 40s。
 */
export function readRebootNotice(data: NetworkConfigResponse | undefined): RebootNotice | null {
  if (!data || (data.reboot !== true && data.rebooting !== true)) return null;
  const seconds = Number(data.rebootCountdownSeconds);
  return {
    countdownSeconds:
      Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds) : REBOOT_DEFAULT_SECONDS,
  };
}

/** 运行时错误契约:200 + {ok:false,error} → 抛 Error 供 mutation onError toast。 */
export function ensureOk(
  data: NetworkConfigResponse | undefined,
  fallbackMessage: string,
): NetworkConfigResponse {
  if (!data) throw new Error(fallbackMessage);
  if (data.ok === false) throw new Error(data.error || fallbackMessage);
  return data;
}
