// network 页纯函数:锁频配置档 localStorage 读写(兼容旧版 www/js/pages/network.js 的
// simpleadmin.bandProfiles 数组格式:{name, savedAt, data:{bands:{LTE,NSA,SA},
// prefNetworkMode, nrModeControlNew}})与 save_settings 载荷构建(对齐旧版 saveChanges 守卫:
// 空 APN=仅改 PDP 语义、设置未就绪拒绝提交、PDP 空值兜底 IPV4V6)。
import type { ApiParams } from "@/lib/api";
import { BAND_GROUP_PREFIX } from "./lib/bandMap";
import type { BandMode } from "./lib/bandMap";

export const BAND_PROFILES_STORAGE_KEY = "simpleadmin.bandProfiles";

export type SelectedBandsByMode = Record<BandMode, string[]>;

export interface BandProfileData {
  bands?: Partial<SelectedBandsByMode>;
  prefNetworkMode?: string | null;
  nrModeControlNew?: string | null;
}

export interface BandProfile {
  name: string;
  savedAt?: string;
  data?: BandProfileData;
}

/** 读取配置档:容错非法 JSON/非数组;过滤无名条目(对齐旧版 loadBandProfiles)。 */
export function loadBandProfiles(storage: Pick<Storage, "getItem"> = localStorage): BandProfile[] {
  let profiles: BandProfile[] = [];
  try {
    const raw = storage.getItem(BAND_PROFILES_STORAGE_KEY);
    if (raw) {
      const parsed: unknown = JSON.parse(raw);
      if (Array.isArray(parsed)) profiles = parsed as BandProfile[];
    }
  } catch {
    // 数据损坏时按空档处理,与旧版 console.warn 后继续等价
  }
  return profiles.filter((profile) => profile && profile.name);
}

/** 写入配置档:配额/隐私模式失败返回 false(调用方 toast failedToSaveProfile)。 */
export function persistBandProfiles(
  profiles: readonly BandProfile[],
  storage: Pick<Storage, "setItem"> = localStorage,
): boolean {
  try {
    storage.setItem(BAND_PROFILES_STORAGE_KEY, JSON.stringify(profiles));
    return true;
  } catch {
    return false;
  }
}

// ---------------------------------------------------------------------------
// save_settings 载荷(对齐旧版 network.js saveChanges)
// ---------------------------------------------------------------------------

export interface CellularSettingsSnapshot {
  /** 当前生效值;"-" 表示未就绪 */
  apn: string;
  pdpType: string;
  prefNetwork: string;
  /** nrModeControlNum 原始值("0"/"1"/"2");null=未就绪 */
  nrModeControlCurrent: string | null;
}

export interface CellularSettingsDraft {
  /** null=用户未改动 */
  newApn: string | null;
  newPdpType: string | null;
  prefNetworkMode: string | null;
  nrModeControlNew: string | null;
}

export type SaveSettingsDecision =
  { kind: "no-change" } | { kind: "not-ready" } | { kind: "submit"; payload: ApiParams };

/**
 * 计算 save_settings 载荷:
 * - 无任何改动 → no-change(调用方 toast noChangesWereMade);
 * - APN/PDP 有改动但当前 APN 未就绪("-"/空)→ not-ready(拒绝提交,防误改 PDP 默认值);
 * - 仅改 PDP 时一并提交当前 APN(后端 CGDCONT 需要完整上下文);PDP 空值兜底 IPV4V6;
 * - modePref/nrDisableMode 仅在用户改动且不同于当前值时提交。
 */
export function buildSaveSettingsPayload(
  snapshot: CellularSettingsSnapshot,
  draft: CellularSettingsDraft,
): SaveSettingsDecision {
  const currentApn = snapshot.apn === "-" ? "" : String(snapshot.apn);
  const newApn = draft.newApn === null ? currentApn : String(draft.newApn).trim();
  const newPdpType = draft.newPdpType === null ? snapshot.pdpType : draft.newPdpType;

  const apnChanged = draft.newApn !== null && newApn !== currentApn;
  const pdpChanged = draft.newPdpType !== null && draft.newPdpType !== snapshot.pdpType;
  const modePrefChanged =
    draft.prefNetworkMode !== null && draft.prefNetworkMode !== snapshot.prefNetwork;
  const nrDisableModeChanged =
    draft.nrModeControlNew !== null &&
    String(draft.nrModeControlNew) !== String(snapshot.nrModeControlCurrent);

  if (!apnChanged && !pdpChanged && !modePrefChanged && !nrDisableModeChanged) {
    return { kind: "no-change" };
  }

  const payload: ApiParams = {};
  if (apnChanged || pdpChanged) {
    const apnValue = apnChanged ? newApn : snapshot.apn;
    if (!apnValue || apnValue === "-") return { kind: "not-ready" };
    let pdpType = pdpChanged ? newPdpType : snapshot.pdpType;
    if (!pdpType || pdpType === "-") pdpType = "IPV4V6";
    payload.pdpType = pdpType;
    payload.apn = apnValue;
  }
  if (modePrefChanged) payload.modePref = draft.prefNetworkMode;
  if (nrDisableModeChanged) payload.nrDisableMode = draft.nrModeControlNew;
  return { kind: "submit", payload };
}

/** 频段摘要 chips 用:"B1 B3 …" / "N78 …" 前缀拼接(矩阵与配置档卡共用)。 */
export function formatBandSummary(mode: BandMode, names: readonly string[]): string {
  return names.map((name) => `${BAND_GROUP_PREFIX[mode]}${name}`).join(" ");
}
