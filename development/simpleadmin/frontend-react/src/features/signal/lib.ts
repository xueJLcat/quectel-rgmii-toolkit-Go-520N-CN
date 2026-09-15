// 信号详情页纯函数:天线标签映射、载波聚合状态、服务小区行构建、更新时间格式化。
// 行为对齐旧版 www/js/pages/signal.js(computed caStatus/cellRows 与空值行过滤规则)。
import type { SignalAntenna, SignalCarrier, SignalCell } from "@/lib/api";

/** 开机保护期/后台执行中:queryFn 抛此错误进入 pending 语义(保留旧值 + "获取中" chip,绝不假空态)。 */
export class SignalPendingError extends Error {
  constructor() {
    super("signal data pending");
    this.name = "SignalPendingError";
  }
}

/** 后端天线 label 为固定中文(page_signal.go signalAntennaIDs),按 id 映射到 signal ns 词条键。 */
export const ANTENNA_LABEL_KEYS: Record<string, string> = {
  PRX: "antenna1Prx",
  DRX: "antenna2Drx",
  RX2: "antenna3Rx2",
  RX3: "antenna4Rx3",
};

export type Translate = (key: string) => string;

export function antennaLabel(antenna: SignalAntenna, t: Translate): string {
  const key = ANTENNA_LABEL_KEYS[String(antenna.id ?? "")];
  if (key) return t(key);
  return antenna.label || antenna.id || "-";
}

/** RSRP 副标题:"-"/空 → 占位文案("无"),否则 "值 dBm"(EN-DC 双连接时后端给 "LTE值/NR值")。 */
export function antennaRsrpText(antenna: SignalAntenna, noneText: string): string {
  const rsrp = antenna.rsrp;
  if (rsrp === undefined || rsrp === null || rsrp === "" || rsrp === "-") return noneText;
  return `${rsrp} dBm`;
}

export type AggregationState = "none" | "single" | "active";

/** 载波聚合状态(对齐旧版 caStatus):0 → 无;1 → 单载波;>1 → 聚合生效。 */
export function aggregationState(carriers: readonly SignalCarrier[] | undefined): {
  state: AggregationState;
  count: number;
} {
  const count = carriers?.length ?? 0;
  if (count === 0) return { state: "none", count };
  if (count === 1) return { state: "single", count };
  return { state: "active", count };
}

const CELL_ROW_DEFS: readonly { field: keyof SignalCell; labelKey: string; always?: boolean }[] = [
  { field: "network_mode", labelKey: "networkMode", always: true },
  { field: "pcc_pci", labelKey: "physicalCellIdPci" },
  { field: "scc_pci", labelKey: "sccPci" },
  { field: "earfcns", labelKey: "arfcn", always: true },
  { field: "tac", labelKey: "tac" },
  { field: "cellID", labelKey: "cellId" },
  { field: "eNBID", labelKey: "enbId" },
  { field: "rsrpNR", labelKey: "rsrpNr" },
  { field: "rsrqNR", labelKey: "rsrqNr" },
  { field: "sinrNR", labelKey: "sinrNr" },
  { field: "rsrpLTE", labelKey: "rsrpLte" },
  { field: "rsrqLTE", labelKey: "rsrqLte" },
  { field: "sinrLTE", labelKey: "sinrLte" },
  { field: "rssi", labelKey: "rssi" },
];

export interface CellRow {
  key: string;
  labelKey: string;
  value: string;
}

function isEmptyValue(value: unknown): boolean {
  return value === undefined || value === null || value === "-" || String(value).trim() === "";
}

/** 服务小区描述列表行:网络制式/频点恒显(确认驻留状态),其余空值行隐藏(对齐旧版 cellRows)。 */
export function buildCellRows(cell: SignalCell | undefined): CellRow[] {
  const source = cell ?? {};
  const rows: CellRow[] = [];
  for (const def of CELL_ROW_DEFS) {
    const raw = source[def.field];
    if (isEmptyValue(raw) && !def.always) continue;
    rows.push({
      key: def.field,
      labelKey: def.labelKey,
      value: isEmptyValue(raw) ? "-" : String(raw),
    });
  }
  return rows;
}

/** 更新时间 HH:mm:ss(对齐旧版:zh → zh-CN,其余 → en-GB,24 小时制)。 */
export function formatUpdatedAt(timestampMs: number, language: string): string {
  const locale = String(language || "zh-CN")
    .toLowerCase()
    .startsWith("zh")
    ? "zh-CN"
    : "en-GB";
  return new Date(timestampMs).toLocaleTimeString(locale, { hour12: false });
}
