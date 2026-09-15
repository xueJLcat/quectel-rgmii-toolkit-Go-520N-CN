// 频段能力表(移植自 www/js/band_map.js + populate-checkbox.js):
// 仅适配 RG520N-CN,频段为已确认的硬件能力值:
//   LTE  B1/3/5/8/34/38/39/40/41(9 个)
//   NSA  n1/8/28/41/78
//   SA   n1/8/28/41/78
// 注意:本机固件 AT+QNWPREFCFG="ue_capability_band" 返回的是受限子集
// (LTE 仅 1:3:5:8、NSA 仅 78),不代表硬件能力,不得作为频段表依据。
// 设备型号恒定,不保留任何型号识别/匹配逻辑(getBandsForModel 忽略入参)。

export const TARGET_MODEL = "RG520N-CN";

export interface BandDefaults {
  lte: string;
  nsa: string;
  sa: string;
}

export const BAND_DEFAULTS: BandDefaults = {
  lte: "1:3:5:8:34:38:39:40:41",
  nsa: "1:8:28:41:78",
  sa: "1:8:28:41:78",
};

/** 兼容旧签名的固定频段集:无条件返回新副本,防止调用方修改污染共享对象。 */
export function getBandsForModel(_model?: string): BandDefaults {
  return { ...BAND_DEFAULTS };
}

export type BandMode = "LTE" | "NSA" | "SA";

export const BAND_MODES: readonly BandMode[] = ["LTE", "NSA", "SA"];

export const BAND_GROUP_TITLES: Record<BandMode, string> = {
  LTE: "LTE",
  NSA: "NR5G-NSA",
  SA: "NR5G-SA",
};

/** 频段名前缀:LTE 为 B(band),NSA/SA 为 N(nR band)。 */
export const BAND_GROUP_PREFIX: Record<BandMode, string> = {
  LTE: "B",
  NSA: "N",
  SA: "N",
};

/** 冒号分隔频段串 → 去空白去空项列表(对齐旧版 populate-checkbox.js bandList)。 */
export function parseBandList(value: string | null | undefined): string[] {
  if (value === null || value === undefined) return [];
  return String(value)
    .split(":")
    .map((band) => band.trim())
    .filter((band) => band !== "");
}

export interface BandEntry {
  name: string;
  /** 初始勾选态 = 当前已锁定频段(高亮);用户交互后由页面选择态接管 */
  checked: boolean;
}

export interface BandGroup {
  mode: BandMode;
  title: string;
  prefix: string;
  bands: BandEntry[];
}

export type LockedBandsByMode = Partial<Record<BandMode, string | null | undefined>>;

const MODE_DEFAULT_KEY: Record<BandMode, keyof BandDefaults> = {
  LTE: "lte",
  NSA: "nsa",
  SA: "sa",
};

/**
 * 构建三列频段组(对齐旧版 SimpleAdmin.BandCheckboxes.build):
 * 全集来自 BAND_DEFAULTS,checked 来自 locked_<mode>_bands(bands action 响应)。
 * locked 为 null/undefined(尚未加载)时 checked 全 false。
 */
export function buildBandGroups(locked?: LockedBandsByMode | null): BandGroup[] {
  return BAND_MODES.map((mode) => {
    const lockedList = parseBandList(locked?.[mode]);
    return {
      mode,
      title: BAND_GROUP_TITLES[mode],
      prefix: BAND_GROUP_PREFIX[mode],
      bands: parseBandList(BAND_DEFAULTS[MODE_DEFAULT_KEY[mode]]).map((name) => ({
        name,
        checked: lockedList.includes(name),
      })),
    };
  });
}

/** 频段名列表 → lock_bands 的 values 参数(冒号分隔,对齐旧版 join(":"))。 */
export function joinBandValues(names: readonly string[]): string {
  return names.join(":");
}
