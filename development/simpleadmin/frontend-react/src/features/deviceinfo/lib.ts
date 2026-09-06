// deviceinfo 纯函数/常量集:版本号读 <meta name="sa-version">(回退空;未替换的 __SA_VERSION__
// 占位视同未注入)、代码库链接、
// SIM 状态(服务端固定中文枚举)→ StatusChip tone/词条键映射、"-" 占位判定。
import type { StatusTone } from "@/components/common";

export const DEVICE_INFO_POLL_MS = 5_000;
export const SA_VERSION_META_SELECTOR = 'meta[name="sa-version"]';
export const SA_VERSION_PLACEHOLDER = "__SA_VERSION__";
export const REPO_URL = "https://github.com/xueJLcat/quectel-rgmii-toolkit-Go-520N-CN";

export function readSaVersion(doc?: Document | null): string {
  const root = doc ?? (typeof document === "undefined" ? null : document);
  if (!root) return "";
  const content = root.querySelector<HTMLMetaElement>(SA_VERSION_META_SELECTOR)?.content ?? "";
  const trimmed = content.trim();
  return trimmed === SA_VERSION_PLACEHOLDER ? "" : trimmed;
}

export type SimLabelKey = "simInserted" | "noSim" | "simUnknown";

export interface SimStatusView {
  tone: StatusTone;
  labelKey: SimLabelKey;
}

// page_deviceinfo.go simStatus 固定枚举:已插卡/未插卡/未知
export function simStatusView(status: string | undefined): SimStatusView {
  const value = String(status ?? "").trim();
  if (value === "已插卡") return { tone: "success", labelKey: "simInserted" };
  if (value === "未插卡") return { tone: "danger", labelKey: "noSim" };
  return { tone: "muted", labelKey: "simUnknown" };
}

export function isPlaceholderValue(value: string | undefined): boolean {
  const text = String(value ?? "").trim();
  return text === "" || text === "-";
}

/** document.hidden 时返回 false 暂停轮询(refetchInterval 回调形态)。 */
export function visiblePollInterval(ms: number): number | false {
  return typeof document !== "undefined" && document.hidden ? false : ms;
}
