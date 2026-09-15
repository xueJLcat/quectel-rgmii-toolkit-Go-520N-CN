// celllock 页纯函数(行为对齐旧版 www/js/pages/celllock.js):
// 小区唯一标识/选择约束(NR 单选、LTE ≤10)、RSRP 信号格、LTE 手动锁 pairs 拼接与校验、
// 频点锁输入解析(NR5G≤32/LTE≤2)、cellLockStatus 映射、SCS 选项表。
import type { NeighbourCell } from "@/lib/api";

/** 扫描表选中项(对齐旧版 selectedCells 条目 {pci, provider, type, freq})。 */
export interface SelectedCell {
  pci: string;
  provider: string;
  type: string;
  freq: string;
}

export const MAX_LTE_CELLS = 10;

export const EARFCN_LIMITS = { NR5G: 32, LTE: 2 } as const;

export type EarfcnRat = keyof typeof EARFCN_LIMITS;

/** 小区唯一标识:同一 PCI 在不同频点上属于不同小区(对齐旧版 cellKey)。 */
export function cellKey(cell: {
  type?: string | null;
  pci?: string | null;
  provider?: string | null;
  freq?: string | null;
}): string {
  return [cell.type, cell.pci, cell.provider, cell.freq].join("|");
}

export type SelectionToggleResult =
  { next: SelectedCell[]; blocked?: undefined } | { next: SelectedCell[]; blocked: "lte-limit" };

/**
 * 扫描表选择切换(对齐旧版 toggleCellSelection):
 * NR5G 单选——新选 NR 替换已选 NR(保留 LTE);LTE 多选,超过 10 条返回 blocked
 * (调用方 toast 阻止);再次点击已选项 = 取消。
 */
export function toggleCellSelection(
  selected: readonly SelectedCell[],
  cell: SelectedCell,
): SelectionToggleResult {
  const key = cellKey(cell);
  const index = selected.findIndex((item) => cellKey(item) === key);
  if (index !== -1) {
    return { next: selected.filter((_, i) => i !== index) };
  }
  if (cell.type === "NR5G") {
    return { next: [...selected.filter((item) => item.type !== "NR5G"), cell] };
  }
  const lteCount = selected.filter((item) => item.type !== "NR5G").length;
  if (lteCount >= MAX_LTE_CELLS) {
    return { next: [...selected], blocked: "lte-limit" };
  }
  return { next: [...selected, cell] };
}

/** RSRP → 0-5 信号格(阈值对齐旧版 signalBars)。 */
export function signalBars(rsrp: string | number | undefined): number {
  const value = Number(rsrp);
  if (!Number.isFinite(value)) return 0;
  if (value >= -55) return 5;
  if (value >= -85) return 4;
  if (value >= -95) return 3;
  if (value >= -105) return 2;
  if (value >= -115) return 1;
  return 0;
}

/** RSRP → MetricBar 百分比(信号格 / 满格)。 */
export function rsrpPercent(rsrp: string | number | undefined): number {
  return (signalBars(rsrp) / 5) * 100;
}

/**
 * 在扫描结果中定位小区(对齐旧版 getCellDetails 的 matches):
 * pci+provider 必配,freq 仅在给出时参与匹配;命中返回频点/PCI/频段。
 */
export function findScannedCell(
  cells: readonly NeighbourCell[],
  ref: { pci?: string | null; provider?: string | null; freq?: string | null },
): { earfcn: string; pci: string; band: string } | null {
  const cell = cells.find(
    (c) =>
      c.pci === ref.pci &&
      c.provider === ref.provider &&
      (ref.freq === undefined || ref.freq === null || c.freq === ref.freq),
  );
  if (!cell || cell.freq === undefined || cell.pci === undefined) return null;
  return { earfcn: String(cell.freq), pci: String(cell.pci), band: String(cell.band ?? "") };
}

/** 小区数量输入归一化(对齐旧版 getLteCellCount:空/非法→0,向下取整,夹在 1-10)。 */
export function normalizeLteCellCount(raw: string | number | null | undefined): number {
  if (raw === null || raw === undefined || raw === "") return 0;
  const count = Math.floor(Number(raw));
  if (!Number.isFinite(count) || count < 1) return 0;
  return Math.min(count, MAX_LTE_CELLS);
}

export interface LtePair {
  earfcn: string;
  pci: string;
}

export type LtePairsResult =
  { status: "ok"; pairs: LtePair[] } | { status: "incomplete" } | { status: "non-digit" };

/**
 * 收集可见 LTE 参数组(对齐旧版 getVisibleLteManualPairs):
 * 任一字段为空 → incomplete;非纯数字 → non-digit;全部有效 → pairs。
 */
export function collectLtePairs(rows: readonly LtePair[]): LtePairsResult {
  const pairs: LtePair[] = [];
  for (const row of rows) {
    const earfcn = String(row.earfcn ?? "").trim();
    const pci = String(row.pci ?? "").trim();
    if (!earfcn || !pci) return { status: "incomplete" };
    if (!/^\d+$/.test(earfcn) || !/^\d+$/.test(pci)) return { status: "non-digit" };
    pairs.push({ earfcn, pci });
  }
  return { status: "ok", pairs };
}

/** lock_lte_manual 的 pairs 参数:"earfcn,pci;earfcn,pci"(对齐旧版拼接格式)。 */
export function buildLtePairsParam(pairs: readonly LtePair[]): string {
  return pairs.map((pair) => `${pair.earfcn},${pair.pci}`).join(";");
}

export type EarfcnInputResult =
  | { status: "ok"; arfcns: string[] }
  | { status: "empty" }
  | { status: "non-digit" }
  | { status: "limit" };

/** 频点锁输入解析(对齐旧版 setEarfcnLock 校验链:逗号分隔→去空→纯数字→数量上限)。 */
export function parseEarfcnInput(raw: string, rat: EarfcnRat): EarfcnInputResult {
  const arfcns = String(raw ?? "")
    .split(",")
    .map((value) => value.trim())
    .filter((value) => value !== "");
  if (arfcns.length === 0) return { status: "empty" };
  if (!arfcns.every((value) => /^\d+$/.test(value))) return { status: "non-digit" };
  if (arfcns.length > EARFCN_LIMITS[rat]) return { status: "limit" };
  return { status: "ok", arfcns };
}

// ---------------------------------------------------------------------------
// 锁定状态横幅
// ---------------------------------------------------------------------------

export type CellLockStatusKey = "unlocked" | "locked4g" | "locked5g" | "locked4gAnd5g" | "unknown";

// 后端 parseNetworkSettingsAT 返回固定中文枚举,映射到 i18n 键;未知值原样展示
const CELL_LOCK_STATUS_MAP: Record<string, CellLockStatusKey> = {
  未锁定: "unlocked",
  已锁定4G: "locked4g",
  已锁定5G: "locked5g",
  已锁定4G和5G: "locked4gAnd5g",
};

export const CELL_LOCK_STATUS_KEY_NS: Record<Exclude<CellLockStatusKey, "unknown">, string> = {
  unlocked: "lockStatusUnlocked",
  locked4g: "lockStatus4g",
  locked5g: "lockStatus5g",
  locked4gAnd5g: "lockStatus4gAnd5g",
};

export function cellLockStatusKey(status: string | null | undefined): CellLockStatusKey {
  return CELL_LOCK_STATUS_MAP[String(status ?? "").trim()] ?? "unknown";
}

/** SCS 分段控件选项(auto = 不提交 scs,后端按频段推断/守护探测)。 */
export const SCS_OPTIONS: { value: string; labelKey: string }[] = [
  { value: "auto", labelKey: "scsAuto" },
  { value: "15", labelKey: "scs15" },
  { value: "30", labelKey: "scs30" },
  { value: "60", labelKey: "scs60" },
  { value: "120", labelKey: "scs120" },
  { value: "240", labelKey: "scs240" },
];
