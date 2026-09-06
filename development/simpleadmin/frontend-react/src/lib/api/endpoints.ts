import { fetchModuleModel } from "./auth";
import type { ModuleModel } from "./auth";
import { FORM_CONTENT_TYPE, gateway } from "./gateway";
import type { GatewayMethod, GatewayResponse } from "./gateway";

export type ApiParamValue = string | number | boolean | undefined | null;
export type ApiParams = Record<string, ApiParamValue>;

export class ApiError extends Error {
  readonly status: number;
  readonly body: string;

  constructor(status: number, body: string) {
    super(`API request failed (HTTP ${status})`);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

export function buildQuery(params?: ApiParams): string {
  if (!params) return "";
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null) continue;
    query.append(key, String(value));
  }
  return query.toString();
}

export function withQuery(path: string, params?: ApiParams): string {
  const query = buildQuery(params);
  return query ? `${path}?${query}` : path;
}

function ensureOk(response: GatewayResponse): GatewayResponse {
  if (response.status < 200 || response.status >= 300) {
    throw new ApiError(response.status, response.body);
  }
  return response;
}

function parseBody<T>(response: GatewayResponse): T {
  const text = response.body.trim();
  return (text ? JSON.parse(text) : null) as T;
}

async function requestJSON<T>(method: GatewayMethod, path: string): Promise<T> {
  return parseBody<T>(ensureOk(await gateway.request(method, path)));
}

export async function getJSON<T>(path: string, params?: ApiParams): Promise<T> {
  return parseBody<T>(ensureOk(await gateway.request("GET", withQuery(path, params))));
}

export async function postForm<T>(path: string, params: ApiParams): Promise<T> {
  const response = ensureOk(
    await gateway.request("POST", path, buildQuery(params), {
      headers: { "Content-Type": FORM_CONTENT_TYPE },
    }),
  );
  return parseBody<T>(response);
}

export async function getText(path: string, params?: ApiParams): Promise<string> {
  return ensureOk(await gateway.request("GET", withQuery(path, params))).body;
}

// ---------------------------------------------------------------------------
// 响应接口(字段对照 Go handler 与 www/js/pages/*.js 实际用法,均如实标注可选性)
// ---------------------------------------------------------------------------

export interface OkResponse {
  ok: boolean;
  response?: string;
  error?: string;
}

export interface UptimeParts {
  days?: number;
  hours?: number;
  minutes?: number;
}

export interface DashboardData {
  sim?: string;
  temperature?: string;
  active_sim?: string;
  network_provider?: string;
  mccmnc?: string;
  apn?: string;
  network_mode?: string;
  ipv4?: string;
  ipv6?: string;
  bands?: string;
  bandwidth?: string;
  csq?: string;
  rssi?: string;
  cellID?: string;
  eNBID?: string;
  tac?: string;
  rsrqLTE?: string;
  rsrqNR?: string;
  rsrpLTE?: string;
  rsrpNR?: string;
  sinrLTE?: string;
  sinrNR?: string;
  prxqrsrp?: string;
  drxqrsrp?: string;
  rx2qrsrp?: string;
  rx3qrsrp?: string;
  earfcns?: string;
  pcc_pci?: string;
  scc_pci?: string;
  signalAssessment?: string;
  nr_rx_bytes?: number;
  nr_tx_bytes?: number;
  nr_rx_human?: string;
  nr_tx_human?: string;
  nr_dl_speed?: string;
  nr_ul_speed?: string;
  signalPercentage?: number;
  rsrqLTEPercentage?: number;
  rsrqNRPercentage?: number;
  rsrpLTEPercentage?: number;
  rsrpNRPercentage?: number;
  sinrLTEPercentage?: number;
  sinrNRPercentage?: number;
  internetConnection?: string;
  uptimeParts?: UptimeParts;
  cpuUsagePercent?: number;
  ramUsagePercent?: number;
  ramUsedHuman?: string;
  ramTotalHuman?: string;
  lastUpdate?: string;
  pending?: boolean;
  error?: string;
  raw?: string;
}

export interface HistoryPoint {
  t: number;
  cpu?: number;
  ram?: number;
  sig?: number;
  rsrp?: number;
  rx?: number;
  tx?: number;
  rxr?: number;
  txr?: number;
}

export interface HistoryData {
  intervalSeconds?: number;
  points?: HistoryPoint[];
}

export interface SignalAntenna {
  id?: string;
  label?: string;
  rsrp?: string;
  percent?: number;
}

export interface SignalCarrier {
  role?: string;
  band?: string;
  arfcn?: string;
  bandwidth?: string;
  pci?: string;
}

export interface SignalCell {
  network_mode?: string;
  pcc_pci?: string;
  scc_pci?: string;
  tac?: string;
  earfcns?: string;
  cellID?: string;
  eNBID?: string;
  rsrpLTE?: string;
  rsrqLTE?: string;
  sinrLTE?: string;
  rsrpNR?: string;
  rsrqNR?: string;
  sinrNR?: string;
  rssi?: string;
}

export interface SignalData {
  ok?: boolean;
  pending?: boolean;
  rat?: string;
  antennas?: SignalAntenna[];
  carriers?: SignalCarrier[];
  cell?: SignalCell;
}

export interface DeviceInfoData {
  manufacturer?: string;
  modelName?: string;
  firmwareVersion?: string;
  simStatus?: string;
  simInserted?: boolean;
  imsi?: string;
  iccid?: string;
  imei?: string;
  lanIp?: string;
  wwanIpv4?: string;
  wwanIpv6?: string;
  phoneNumber?: string;
  pending?: boolean;
  error?: string;
  ok?: boolean;
  response?: string;
}

export interface NeighbourCell {
  type?: string;
  provider?: string;
  band?: string;
  freq?: string;
  pci?: string;
  rsrp?: string;
  rsrq?: string;
}

export interface NetworkDataResponse {
  // 通用
  ok?: boolean;
  error?: string;
  pending?: boolean;
  response?: string;
  // action=settings
  sim?: string;
  apn?: string;
  cellLockStatus?: string;
  prefNetwork?: string;
  nrModeControl?: string;
  nrModeControlNum?: string;
  bands?: string;
  pdpType?: string;
  // action=model
  model?: string;
  // action=bands
  locked_lte_bands?: string;
  locked_nsa_bands?: string;
  locked_sa_bands?: string;
  // action=scan
  nr5g_cells_parsed?: NeighbourCell[];
  lte_cells_parsed?: NeighbourCell[];
  // action=lock_serving_cell
  locked?: string;
  scs?: string;
  pci?: string;
  freq?: string;
  band?: string;
  // action=earfcn_lock_status
  nr5g_arfcns?: string[];
  lte_arfcns?: string[];
  [key: string]: unknown;
}

export interface MacBinding {
  index?: number;
  mac?: string;
  ip?: string;
}

export interface NetworkConfigResponse {
  // 通用
  ok?: boolean;
  error?: string;
  pending?: boolean;
  response?: string;
  // action=status
  ipPassStatus?: boolean;
  DNSV4ProxyStatus?: boolean;
  DNSV6ProxyStatus?: boolean;
  dnsV4QueryFailed?: boolean;
  currentUsbNetMode?: string;
  dmzMode?: string;
  dmzIP?: string;
  lanIpStart?: string;
  lanIpEnd?: string;
  lanGwIp?: string;
  // action=mac_bind_list
  bindings?: MacBinding[];
  atReady?: boolean;
  // action=dns_upstream
  enabled?: boolean;
  servers?: string[];
  // 重启提示(ip_passthrough 禁用等)
  reboot?: boolean;
  rebooting?: boolean;
  rebootAfterSeconds?: number;
  rebootCountdownSeconds?: number;
  message?: string;
  [key: string]: unknown;
}

export interface NetworkInterfaceStat {
  name?: string;
  mac?: string;
  state?: string;
  mtu?: number;
  rxBytes?: number;
  txBytes?: number;
  rxRate?: number;
  txRate?: number;
}

export interface LanClient {
  mac?: string;
  ip?: string;
  hostname?: string;
  leaseSeconds?: number;
}

export interface NetworkDetail {
  pending?: boolean;
  wanIPv4?: string;
  wanIPv6?: string;
  lanGateway?: string;
  interfaces?: NetworkInterfaceStat[];
  clients?: LanClient[];
}

export interface FirewallRule {
  port?: string;
  action?: string;
}

export interface FirewallRuleEntry {
  num?: number;
  pkts?: number;
  bytes?: number;
  target?: string;
  proto?: string;
  in?: string;
  out?: string;
  source?: string;
  destination?: string;
  extra?: string;
}

export interface FirewallChainInfo {
  name?: string;
  policy?: string;
  pkts?: number;
  bytes?: number;
  rules?: FirewallRuleEntry[];
}

export interface FirewallStatus {
  rules?: FirewallRule[];
  ruleCount?: number;
  jumpInstalled?: boolean;
  chains?: FirewallChainInfo[];
  ok?: boolean;
  error?: string;
}

export interface SmsMessage {
  sender?: string;
  date?: string;
  text?: string;
  textLines?: string[];
  indices?: number[];
  storage?: string;
  concatRef?: string;
  concatTotal?: number;
  concatSeq?: number;
}

export interface SmsList {
  messages?: SmsMessage[];
  serviceCenters?: string[];
  pending?: boolean;
  error?: string;
}

export interface SmsDataResponse {
  // action=list / list_meta
  messages?: SmsMessage[];
  serviceCenters?: string[];
  pending?: boolean;
  // action=sim_status
  inserted?: boolean;
  // action=delete_all
  me?: boolean;
  sm?: boolean;
  // action=delete_indices
  deleted?: number;
  total?: number;
  // action=send
  segments?: number;
  number?: string;
  // 通用
  ok?: boolean;
  error?: string;
  response?: string;
  [key: string]: unknown;
}

export interface SystemDataResponse {
  imei?: string;
  pending?: boolean;
  error?: string;
  ok?: boolean;
  response?: string;
  mock?: boolean;
  [key: string]: unknown;
}

export interface DiagResponse {
  ok?: boolean;
  error?: string;
  target?: string;
  statusCode?: number;
  latencyMs?: number;
  domain?: string;
  server?: string;
  addresses?: string[];
  [key: string]: unknown;
}

export interface ProcessInfo {
  pid?: number;
  user?: string;
  name?: string;
  state?: string;
  cpuPercent?: number;
  memPercent?: number;
  rssHuman?: string;
}

export interface SystemMonitor {
  cpuUsagePercent?: number;
  ramUsagePercent?: number;
  ramUsedHuman?: string;
  ramTotalHuman?: string;
  loadAverage?: string;
  uptime?: string;
  processCount?: number;
  processes?: ProcessInfo[];
}

export interface WatchdogConfig {
  enabled?: boolean;
  failThreshold?: number;
  cooldownMinutes?: number;
  checkIntervalMinutes?: number;
  targets?: string[];
  defaultTargets?: string[];
  actionPolicy?: string;
  consecutiveFailures?: number;
  lastActionTime?: string;
  lastActionType?: string;
  pollerRunning?: boolean;
}

export interface SchedulerConfig {
  rebootEnabled?: boolean;
  rebootTime?: string;
  lastRebootDate?: string;
}

export interface TimesyncConfig {
  enabled?: boolean;
  intervalMinutes?: number;
  server?: string;
  defaultServer?: string;
  pollerRunning?: boolean;
  syncing?: boolean;
  lastSyncTime?: string;
  lastSyncOK?: boolean;
  lastError?: string;
  lastOffsetMs?: number;
  lastServer?: string;
  systemTime?: string;
}

export type ConfigSetResult<T> = OkResponse & { config?: T };

export type TimesyncNowResult = OkResponse & {
  server?: string;
  offsetMs?: number;
  systemTime?: string;
};

export interface SmsWebhookConfig {
  enabled?: boolean;
  url?: string;
  lastNotifiedIndex?: number;
}

export interface LanguageConfig {
  language?: string;
}

export interface ThemeConfig {
  theme?: string;
}

export interface TTLStatus {
  isEnabled?: boolean;
  ttl?: number;
}

export type SetTTLResult = OkResponse & { debug_logs?: string[] };

export interface MockATResponse {
  ok?: boolean;
  error?: string;
  kind?: string;
  status?: Record<string, string>;
  commands?: string[];
  [key: string]: unknown;
}

// ---------------------------------------------------------------------------
// 功能域封装(全部经 WS 网关;module_model 例外,直接 HTTP fetch)
// ---------------------------------------------------------------------------

export function dashboardData(params?: ApiParams): Promise<DashboardData> {
  return postForm<DashboardData>("/api/dashboard_data", { action: "get", ...params });
}

export function historyData(): Promise<HistoryData> {
  return postForm<HistoryData>("/api/history_data", {});
}

export function signalData(params?: ApiParams): Promise<SignalData> {
  return postForm<SignalData>("/api/signal_data", { ...params });
}

export function deviceInfoData(action = "get", params?: ApiParams): Promise<DeviceInfoData> {
  return postForm<DeviceInfoData>("/api/device_info_data", { action, ...params });
}

export function networkData<T = NetworkDataResponse>(
  action: string,
  params?: ApiParams,
): Promise<T> {
  return postForm<T>("/api/network_data", { action, ...params });
}

export function networkConfigData<T = NetworkConfigResponse>(
  action: string,
  params?: ApiParams,
): Promise<T> {
  return postForm<T>("/api/network_config_data", { action, ...params });
}

export function networkDetail(params?: ApiParams): Promise<NetworkDetail> {
  return postForm<NetworkDetail>("/api/network_detail", { ...params });
}

export function firewallData<T = FirewallStatus>(action: string, params?: ApiParams): Promise<T> {
  return postForm<T>("/api/firewall_data", { action, ...params });
}

export function smsData<T = SmsDataResponse>(action: string, params?: ApiParams): Promise<T> {
  return postForm<T>("/api/sms_data", { action, ...params });
}

export function systemData<T = SystemDataResponse>(action: string, params?: ApiParams): Promise<T> {
  return postForm<T>("/api/system_data", { action, ...params });
}

export function atData<T = OkResponse>(action: string, params?: ApiParams): Promise<T> {
  return postForm<T>("/api/at_data", { action, ...params });
}

export function diagData<T = DiagResponse>(action: string, params?: ApiParams): Promise<T> {
  return postForm<T>("/api/diag_data", { action, ...params });
}

export function systemMonitor(sort?: "cpu" | "mem"): Promise<SystemMonitor> {
  return postForm<SystemMonitor>("/api/system_monitor", sort ? { sort } : {});
}

export function getUptime(): Promise<string> {
  return getText("/api/get_uptime");
}

export function getPing(): Promise<string> {
  return getText("/api/get_ping");
}

export function watchdogGet(): Promise<WatchdogConfig> {
  return postForm<WatchdogConfig>("/api/get_watchdog", {});
}

export function watchdogSet(params: ApiParams): Promise<ConfigSetResult<WatchdogConfig>> {
  return postForm<ConfigSetResult<WatchdogConfig>>("/api/set_watchdog", params);
}

export function schedulerGet(): Promise<SchedulerConfig> {
  return postForm<SchedulerConfig>("/api/get_scheduler", {});
}

export function schedulerSet(params: ApiParams): Promise<ConfigSetResult<SchedulerConfig>> {
  return postForm<ConfigSetResult<SchedulerConfig>>("/api/set_scheduler", params);
}

export function timesyncGet(): Promise<TimesyncConfig> {
  return postForm<TimesyncConfig>("/api/get_timesync", {});
}

export function timesyncSet(params: ApiParams): Promise<ConfigSetResult<TimesyncConfig>> {
  return postForm<ConfigSetResult<TimesyncConfig>>("/api/set_timesync", params);
}

export function timesyncNow(): Promise<TimesyncNowResult> {
  return postForm<TimesyncNowResult>("/api/timesync_now", {});
}

export function smsWebhookGet(): Promise<SmsWebhookConfig> {
  return postForm<SmsWebhookConfig>("/api/get_sms_webhook", {});
}

export function smsWebhookSet(params: ApiParams): Promise<OkResponse> {
  return postForm<OkResponse>("/api/set_sms_webhook", params);
}

export function languageGet(): Promise<LanguageConfig> {
  return getJSON<LanguageConfig>("/api/get_language");
}

export function languageSet(language: string): Promise<LanguageConfig> {
  return requestJSON<LanguageConfig>("POST", withQuery("/api/set_language", { language }));
}

export function themeGet(): Promise<ThemeConfig> {
  return getJSON<ThemeConfig>("/api/get_theme");
}

export function themeSet(theme: string): Promise<ThemeConfig> {
  return requestJSON<ThemeConfig>("POST", withQuery("/api/set_theme", { theme }));
}

export function setPassword(
  currentPassword: string,
  newPassword: string,
  confirmPassword: string,
): Promise<OkResponse> {
  return postForm<OkResponse>("/api/set_password", {
    current_password: currentPassword,
    new_password: newPassword,
    confirm_password: confirmPassword,
  });
}

export function getTTLStatus(): Promise<TTLStatus> {
  return getJSON<TTLStatus>("/api/get_ttl_status");
}

export function setTTL(ttlvalue: number | string): Promise<SetTTLResult> {
  return getJSON<SetTTLResult>("/api/set_ttl", { ttlvalue });
}

// 仅 mock 模式可用(实机返回 403)
export function mockAT(params?: ApiParams): Promise<MockATResponse> {
  return postForm<MockATResponse>("/api/mock_at", { ...params });
}

// module_model 是公开端点,不走 WS 网关,直接 HTTP fetch
export function moduleModel(fetchImpl?: typeof fetch): Promise<ModuleModel> {
  return fetchModuleModel(fetchImpl);
}
