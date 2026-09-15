// atcommands 页纯函数库(无 React/IO 依赖):
// - transcript 追加与 200 行截断(对齐旧版 pages/atcommands.js appendOutput:
//   截断点对齐 "> " 命令边界,不切断某条命令的应答中间);
// - 命令行分类(渲染着色:"> 命令" cyan / ERROR rose / OK emerald);
// - 命令历史:localStorage "simpleadmin.atHistory" 兼容旧格式(JSON 字符串数组),
//   ≤50 去重(push 时移除旧位置再 unshift);
// - ↑/↓ 历史召回游标(含草稿保存/恢复,对齐 historyPrev/historyNext)。

/** transcript 最大行数,超出从头部截断(对齐命令边界)。 */
export const TRANSCRIPT_MAX_LINES = 200;

/**
 * 追加一条 "> 命令 + 应答" 并把总行数截到 200 行以内:
 * 从溢出点向后找到第一个以 "> " 开头的行作为截断起点(命令边界),
 * 找不到边界时退回按行数硬截断(与旧版一致)。
 */
export function appendTranscript(current: string, command: string, response: string): string {
  const entry = `> ${command}\n${response ?? ""}\n`;
  let output = current ? current + entry : entry;
  const lines = output.split("\n");
  if (lines.length > TRANSCRIPT_MAX_LINES) {
    let cut = lines.length - TRANSCRIPT_MAX_LINES;
    while (cut < lines.length && !lines[cut].startsWith("> ")) cut += 1;
    if (cut >= lines.length) cut = lines.length - TRANSCRIPT_MAX_LINES;
    output = lines.slice(cut).join("\n");
  }
  return output;
}

export type TranscriptLineKind = "command" | "error" | "ok" | "plain";

/** 行分类:"> " 前缀 = 命令回显;含 ERROR = 错误;单独 OK = 成功终结符。 */
export function classifyTranscriptLine(line: string): TranscriptLineKind {
  if (line.startsWith("> ")) return "command";
  const upper = line.toUpperCase();
  if (upper.includes("ERROR")) return "error";
  if (line.trim() === "OK") return "ok";
  return "plain";
}

// ---------------------------------------------------------------------------
// 命令历史(localStorage 兼容旧格式)
// ---------------------------------------------------------------------------

export const AT_HISTORY_KEY = "simpleadmin.atHistory";
export const AT_HISTORY_LIMIT = 50;

/** 解析旧格式(JSON 字符串数组):过滤非字符串/空白项,截前 50;损坏数据回退空表。 */
export function parseAtHistory(raw: string | null): string[] {
  try {
    const parsed = raw ? (JSON.parse(raw) as unknown) : [];
    return Array.isArray(parsed)
      ? parsed
          .filter((item): item is string => typeof item === "string" && item.trim() !== "")
          .slice(0, AT_HISTORY_LIMIT)
      : [];
  } catch {
    return [];
  }
}

/** 入栈:去重(移除旧位置)→ unshift → 截前 50;空命令忽略。 */
export function pushAtHistory(history: readonly string[], command: string): string[] {
  const cmd = String(command ?? "").trim();
  if (!cmd) return [...history];
  const next = history.filter((item) => item !== cmd);
  next.unshift(cmd);
  return next.slice(0, AT_HISTORY_LIMIT);
}

/** 单条删除。 */
export function removeAtHistoryEntry(history: readonly string[], command: string): string[] {
  return history.filter((item) => item !== command);
}

// ---------------------------------------------------------------------------
// ↑/↓ 历史召回游标(index=-1 未激活;首次上翻保存草稿,翻回底部恢复)
// ---------------------------------------------------------------------------

export interface HistoryCursor {
  index: number;
  draft: string;
}

export const INITIAL_HISTORY_CURSOR: HistoryCursor = { index: -1, draft: "" };

export interface HistoryRecallStep {
  cursor: HistoryCursor;
  input: string;
}

/** ↑:激活时先存草稿,向更旧历史移动;已到最旧项则不动。 */
export function historyPrev(
  cursor: HistoryCursor,
  history: readonly string[],
  input: string,
): HistoryRecallStep {
  if (history.length === 0) return { cursor, input };
  const draft = cursor.index < 0 ? input : cursor.draft;
  if (cursor.index < history.length - 1) {
    const index = cursor.index + 1;
    return { cursor: { index, draft }, input: history[index] };
  }
  return { cursor: { ...cursor, draft }, input };
}

/** ↓:向更新历史移动;越过最新项恢复草稿并复位游标。 */
export function historyNext(
  cursor: HistoryCursor,
  history: readonly string[],
  input: string,
): HistoryRecallStep {
  if (cursor.index < 0) return { cursor, input };
  const index = cursor.index - 1;
  if (index < 0) {
    return { cursor: INITIAL_HISTORY_CURSOR, input: cursor.draft };
  }
  return { cursor: { index, draft: cursor.draft }, input: history[index] };
}

// ---------------------------------------------------------------------------
// 快捷命令预设(7 条,label 为 atcommands ns i18n 键)
// ---------------------------------------------------------------------------

export interface QuickCommand {
  labelKey: string;
  cmd: string;
}

export const QUICK_COMMANDS: QuickCommand[] = [
  { labelKey: "moduleInfo", cmd: "ATI" },
  { labelKey: "signalStrength", cmd: "AT+CSQ" },
  { labelKey: "servingCell", cmd: 'AT+QENG="servingcell"' },
  { labelKey: "signalDetails", cmd: "AT+QRSRP" },
  { labelKey: "firmwareVersion", cmd: "AT+QGMR" },
  { labelKey: "imsi", cmd: "AT+CIMI" },
  { labelKey: "lanConfig", cmd: 'AT+QMAP="LANIP"' },
];
