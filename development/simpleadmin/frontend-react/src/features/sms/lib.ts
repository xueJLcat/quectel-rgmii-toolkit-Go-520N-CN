// sms 页纯函数库(无 React/IO 依赖,全部可单测):
// - 时间解析/格式化:移植 www/js/simpleadmin-time.js(parseCustomDate YY/MM/DD 年在先 + 时区剥离);
// - 正文规范化/摘要/行拆分:移植 www/js/pages/sms.js(normalizeMessageText 等);
// - list_meta 轻量轮询 diff 状态机:移植 sms.js handlePolledSMSMeta/fetchStableSMSList
//   (签名一致 → 不拉正文;pending 签名稳定 2 轮或等待超上限才拉;concat 未齐先等待);
// - 号码归一化预览:移植 Go 后端 sms_number.go normalizeSMSNumber(前端仅预览,发送仍由后端归一);
// - UTF-16 码元计数与分段(≤70/段,口径对齐后端 sms_pdu.go splitSMSVendorSegments);
// - AT+CPMS? / AT+CSCA? 应答解析(存储可视化条数据源,经 at_data manual_at 低频拉取);
// - CMS 错误码释义字典(设计方案 §6.9 新增点,值 = sms ns i18n 键)。
import type { SmsDataResponse, SmsMessage } from "@/lib/api";

// ---------------------------------------------------------------------------
// 时间(移植 simpleadmin-time.js)
// ---------------------------------------------------------------------------

/**
 * 解析后端下发的 "YY/MM/DD,HH:MM:SS(+TZ)" 时间串。
 * 时区既可能是两位刻度(+32)也可能是 时:分 形式(+08:00),都要剥离;
 * 日期按年在先解构(与后端下发顺序一致),按 UTC 构造避免本地时区漂移。
 */
export function parseCustomDate(value: string | null | undefined): Date | null {
  const dateStr = String(value ?? "").replace(/[+-]\d{2}(:\d{2})?$/, "");
  const parts = dateStr.split(",");
  if (parts.length !== 2 || !parts[0] || !parts[1]) return null;
  const dateParts = parts[0].split("/").map(Number);
  const timeParts = parts[1].split(":").map(Number);
  if (dateParts.length !== 3 || timeParts.length !== 3) return null;
  if (dateParts.concat(timeParts).some((part) => !Number.isFinite(part))) return null;
  const [year, month, day] = dateParts;
  const [hour, minute, second] = timeParts;
  const date = new Date(Date.UTC(2000 + year, month - 1, day, hour, minute, second));
  return Number.isNaN(date.getTime()) ? null : date;
}

const pad2 = (value: number): string => value.toString().padStart(2, "0");

/** 与后端一致的 YY/MM/DD,HH:MM:SS(年在先),保证解析↔显示精确往返。 */
export function formatSmsDate(date: Date): string {
  const year = pad2(date.getUTCFullYear() - 2000);
  return `${year}/${pad2(date.getUTCMonth() + 1)}/${pad2(date.getUTCDate())},${pad2(
    date.getUTCHours(),
  )}:${pad2(date.getUTCMinutes())}:${pad2(date.getUTCSeconds())}`;
}

/** 完整展示格式(详情对话框):YYYY/MM/DD HH:MM:SS。 */
export function formatDateTime(date: Date): string {
  return `${date.getUTCFullYear()}/${pad2(date.getUTCMonth() + 1)}/${pad2(
    date.getUTCDate(),
  )} ${pad2(date.getUTCHours())}:${pad2(date.getUTCMinutes())}:${pad2(date.getUTCSeconds())}`;
}

// ---------------------------------------------------------------------------
// 正文规范化 / 摘要(移植 pages/sms.js)
// ---------------------------------------------------------------------------

/** CRLF/CR/垂直制表等一律归一 \n,并展开字面 "\r\n"/"\n"/"\r" 转义(PDU 解码残留)。 */
export function normalizeMessageText(text: string | null | undefined): string {
  return String(text ?? "")
    .replace(/\r\n/g, "\n")
    .replace(/\r/g, "\n")
    .replace(/[\v\f\u0085\u2028\u2029]/g, "\n")
    .replace(/\\r\\n/g, "\n")
    .replace(/\\n/g, "\n")
    .replace(/\\r/g, "\n");
}

export function normalizeMessageLines(text: string, textLines: readonly string[] = []): string[] {
  if (textLines.length > 0) {
    const lines: string[] = [];
    for (const line of textLines) {
      for (const part of normalizeMessageText(line).split("\n")) lines.push(part);
    }
    return lines;
  }
  return normalizeMessageText(text).split("\n");
}

/** 单行摘要:换行/连续空白折叠为一个空格;空内容回退占位符(由调用方 i18n)。 */
export function messagePreview(row: { lines: readonly string[]; text: string }): string {
  const text = row.lines.length > 0 ? row.lines.join(" ") : row.text;
  return String(text || "")
    .replace(/\s+/g, " ")
    .trim();
}

// ---------------------------------------------------------------------------
// 收件箱行模型
// ---------------------------------------------------------------------------

export type SmsStorage = "ME" | "SM";

export interface SmsRow {
  sender: string;
  date: Date | null;
  /** 列表显示时间(YY/MM/DD,HH:MM:SS);解析失败为 "-"(对齐旧版) */
  dateText: string;
  text: string;
  lines: string[];
  indices: number[];
  storage: SmsStorage;
  concatRef?: string;
  concatTotal?: number;
  concatSeq?: number;
}

export function toSmsRows(messages: readonly SmsMessage[] = []): SmsRow[] {
  return messages.map((msg) => {
    const text = normalizeMessageText(msg.text);
    const date = msg.date ? parseCustomDate(msg.date) : null;
    return {
      sender: msg.sender ?? "",
      date,
      dateText: date ? formatSmsDate(date) : "-",
      text,
      lines: normalizeMessageLines(text, Array.isArray(msg.textLines) ? msg.textLines : []),
      indices: Array.isArray(msg.indices) ? msg.indices : [],
      storage: msg.storage === "SM" ? "SM" : "ME",
      concatRef: msg.concatRef,
      concatTotal: msg.concatTotal,
      concatSeq: msg.concatSeq,
    };
  });
}

/** 行身份键(刷新后按 key 恢复选中/详情,移植 sms.js messageKey)。 */
export function rowKey(row: SmsRow): string {
  return [row.indices.join(","), row.sender, row.dateText, row.text].join("|");
}

// ---------------------------------------------------------------------------
// list_meta 轮询 diff 状态机(移植 sms.js handlePolledSMSMeta / fetchStableSMSList)
// ---------------------------------------------------------------------------

export const SMS_POLL_INTERVAL_MS = 5000;
/** pending 签名需连续稳定的轮询次数(对齐旧版 smsStablePollsRequired) */
export const SMS_STABLE_POLLS_REQUIRED = 2;
/** 签名不稳定/concat 未齐时的最长等待,超时强制拉正文(对齐 smsPendingMaxWaitMs) */
export const SMS_PENDING_MAX_WAIT_MS = 15_000;

export interface SmsSyncState {
  /** 已应用正文的索引签名 */
  indexSignature: string;
  /** 已应用正文的元信息签名 */
  metaSignature: string;
  /** 待确认变更的索引签名 */
  pendingIndexSignature: string;
  /** 待确认变更的元信息签名 */
  pendingMetaSignature: string;
  pendingStableCount: number;
  pendingFirstSeenAt: number;
}

export function createSmsSyncState(): SmsSyncState {
  return {
    indexSignature: "",
    metaSignature: "",
    pendingIndexSignature: "",
    pendingMetaSignature: "",
    pendingStableCount: 0,
    pendingFirstSeenAt: 0,
  };
}

function resetPending(state: SmsSyncState): SmsSyncState {
  return {
    ...state,
    pendingIndexSignature: "",
    pendingMetaSignature: "",
    pendingStableCount: 0,
    pendingFirstSeenAt: 0,
  };
}

/** 索引签名:"ME:2,SM:1" 排序拼接(对齐 makeSMSIndexSignature)。 */
export function makeSmsIndexSignature(messages: readonly SmsMessage[] = []): string {
  const indices: string[] = [];
  for (const msg of messages) {
    if (!Array.isArray(msg.indices)) continue;
    const storage = msg.storage === "SM" ? "SM" : "ME";
    for (const index of msg.indices) {
      const n = Number(index);
      if (Number.isFinite(n)) indices.push(`${storage}:${n}`);
    }
  }
  indices.sort();
  return indices.join(",");
}

/** 元信息签名:sender/date/indices/concat 三元组(对齐 makeSMSMetaSignature)。 */
export function makeSmsMetaSignature(messages: readonly SmsMessage[] = []): string {
  return JSON.stringify(
    messages.map((msg) => [
      msg.sender ?? "",
      msg.date ?? "",
      Array.isArray(msg.indices) ? msg.indices.join(",") : "",
      msg.concatTotal ?? "",
      msg.concatRef ?? "",
      msg.concatSeq ?? "",
    ]),
  );
}

/** concat 合并未齐(某组分段数 < concatTotal):新短信可能仍在逐段到达。 */
export function hasIncompleteMultipart(messages: readonly SmsMessage[] = []): boolean {
  return messages.some((msg) => {
    const total = Number(msg.concatTotal ?? 0);
    if (!Number.isFinite(total) || total <= 1) return false;
    const indices = Array.isArray(msg.indices) ? msg.indices : [];
    return indices.length < total;
  });
}

export interface SmsMetaPollStep {
  state: SmsSyncState;
  /** 非 null 表示应拉正文(list force=1),expected 用于结果一致性校验 */
  fetch: { expectedIndexSignature: string } | null;
}

/**
 * 一轮 list_meta 轮询的决策(纯函数):
 * - pending/error 应答不参与 diff(空列表签名会误判变更),原状态跳过;
 * - 双签名与已应用一致 → 清空 pending 追踪,不拉正文;
 * - 有变更 → 追踪 pending 签名:连续稳定 SMS_STABLE_POLLS_REQUIRED 轮,
 *   或等待超过 SMS_PENDING_MAX_WAIT_MS(concat 未齐时同样以超时兜底)才拉正文。
 */
export function evaluateMetaPoll(
  state: SmsSyncState,
  data: SmsDataResponse,
  now: number,
): SmsMetaPollStep {
  if (data.pending === true || data.error) return { state, fetch: null };
  const messages = data.messages ?? [];
  const indexSignature = makeSmsIndexSignature(messages);
  const metaSignature = makeSmsMetaSignature(messages);
  if (indexSignature === state.indexSignature && metaSignature === state.metaSignature) {
    return { state: resetPending(state), fetch: null };
  }

  let next: SmsSyncState;
  if (
    indexSignature === state.pendingIndexSignature &&
    metaSignature === state.pendingMetaSignature
  ) {
    next = { ...state, pendingStableCount: state.pendingStableCount + 1 };
  } else {
    next = {
      ...state,
      pendingIndexSignature: indexSignature,
      pendingMetaSignature: metaSignature,
      pendingStableCount: 1,
      pendingFirstSeenAt: now,
    };
  }

  const waited = now - next.pendingFirstSeenAt;
  if (hasIncompleteMultipart(messages) && waited < SMS_PENDING_MAX_WAIT_MS) {
    return { state: next, fetch: null };
  }
  if (next.pendingStableCount < SMS_STABLE_POLLS_REQUIRED && waited < SMS_PENDING_MAX_WAIT_MS) {
    return { state: next, fetch: null };
  }
  return { state: next, fetch: { expectedIndexSignature: next.pendingIndexSignature } };
}

export type SmsListResultStep = { kind: "apply" } | { kind: "unchanged" } | { kind: "retrack" };

export interface SmsListEvaluation {
  state: SmsSyncState;
  decision: SmsListResultStep;
}

/**
 * 正文(list force=1)结果的决策(纯函数,移植 fetchStableSMSList):
 * - 与已应用签名一致 → unchanged(清空 pending 追踪);
 * - 与发起时的 expected 索引签名不一致 → 拉取期间又有新变化,retrack 等待下一轮 diff;
 * - 否则 apply(调用方保留详情/选中,签名同步为本次结果)。
 */
export function evaluateListResult(
  state: SmsSyncState,
  data: SmsDataResponse,
  expectedIndexSignature: string,
  now: number,
): SmsListEvaluation {
  const messages = data.messages ?? [];
  const indexSignature = makeSmsIndexSignature(messages);
  const metaSignature = makeSmsMetaSignature(messages);
  if (indexSignature === state.indexSignature && metaSignature === state.metaSignature) {
    return { state: resetPending(state), decision: { kind: "unchanged" } };
  }
  if (expectedIndexSignature !== "" && indexSignature !== expectedIndexSignature) {
    return {
      state: {
        ...state,
        pendingIndexSignature: indexSignature,
        pendingMetaSignature: metaSignature,
        pendingStableCount: 1,
        pendingFirstSeenAt: now,
      },
      decision: { kind: "retrack" },
    };
  }
  return {
    state: {
      indexSignature,
      metaSignature,
      pendingIndexSignature: "",
      pendingMetaSignature: "",
      pendingStableCount: 0,
      pendingFirstSeenAt: 0,
    },
    decision: { kind: "apply" },
  };
}

/** 正文应用后的签名状态(初次加载/强制刷新用)。 */
export function appliedSyncState(data: SmsDataResponse): SmsSyncState {
  const messages = data.messages ?? [];
  return {
    indexSignature: makeSmsIndexSignature(messages),
    metaSignature: makeSmsMetaSignature(messages),
    pendingIndexSignature: "",
    pendingMetaSignature: "",
    pendingStableCount: 0,
    pendingFirstSeenAt: 0,
  };
}

// ---------------------------------------------------------------------------
// 多选删除索引串(对齐旧版 "ME:2,SM:1" 格式)
// ---------------------------------------------------------------------------

export function buildDeleteIndices(rows: readonly SmsRow[]): string {
  const tokens: string[] = [];
  for (const row of rows) {
    for (const index of row.indices) tokens.push(`${row.storage}:${index}`);
  }
  return tokens.join(",");
}

// ---------------------------------------------------------------------------
// 号码归一化预览(移植 Go sms_number.go normalizeSMSNumber + mccToCallingCode)
// ---------------------------------------------------------------------------

/** MCC → 国家/地区呼叫代码(与后端 sms_number.go mccToCallingCode 全量对齐)。 */
export const MCC_CALLING_CODES: Record<string, string> = {
  "202": "30",
  "204": "31",
  "206": "32",
  "208": "33",
  "212": "377",
  "213": "376",
  "214": "34",
  "216": "36",
  "218": "387",
  "219": "385",
  "220": "381",
  "222": "39",
  "225": "39",
  "226": "40",
  "228": "41",
  "230": "420",
  "231": "421",
  "232": "43",
  "234": "44",
  "235": "44",
  "238": "45",
  "240": "46",
  "242": "47",
  "244": "358",
  "246": "370",
  "247": "371",
  "248": "372",
  "250": "7",
  "255": "380",
  "257": "375",
  "259": "373",
  "260": "48",
  "262": "49",
  "266": "350",
  "268": "351",
  "270": "352",
  "272": "353",
  "274": "354",
  "276": "355",
  "278": "356",
  "280": "357",
  "282": "995",
  "283": "374",
  "284": "359",
  "286": "90",
  "288": "298",
  "290": "299",
  "292": "378",
  "293": "386",
  "294": "389",
  "295": "423",
  "297": "382",
  "302": "1",
  "310": "1",
  "311": "1",
  "312": "1",
  "313": "1",
  "314": "1",
  "315": "1",
  "316": "1",
  "330": "1",
  "332": "1",
  "334": "52",
  "338": "1",
  "340": "590",
  "342": "1",
  "344": "1",
  "346": "1",
  "348": "1",
  "350": "1",
  "352": "1",
  "354": "1",
  "356": "1",
  "358": "1",
  "360": "1",
  "363": "297",
  "364": "1",
  "365": "1",
  "366": "1",
  "368": "53",
  "370": "1",
  "372": "509",
  "374": "1",
  "376": "1",
  "400": "994",
  "401": "7",
  "402": "975",
  "404": "91",
  "405": "91",
  "406": "91",
  "410": "92",
  "412": "93",
  "413": "94",
  "414": "95",
  "415": "961",
  "416": "962",
  "417": "963",
  "418": "964",
  "419": "965",
  "420": "966",
  "421": "967",
  "422": "968",
  "424": "971",
  "425": "972",
  "426": "973",
  "427": "974",
  "428": "976",
  "429": "977",
  "430": "971",
  "431": "971",
  "432": "98",
  "434": "998",
  "436": "992",
  "437": "996",
  "438": "993",
  "440": "81",
  "441": "81",
  "450": "82",
  "452": "84",
  "454": "852",
  "455": "853",
  "456": "855",
  "457": "856",
  "460": "86",
  "461": "86",
  "466": "886",
  "467": "850",
  "470": "880",
  "472": "960",
  "502": "60",
  "505": "61",
  "510": "62",
  "514": "670",
  "515": "63",
  "520": "66",
  "525": "65",
  "528": "673",
  "530": "64",
  "536": "674",
  "537": "675",
  "539": "676",
  "540": "677",
  "541": "678",
  "542": "679",
  "545": "686",
  "546": "687",
  "547": "689",
  "548": "682",
  "549": "685",
  "550": "691",
  "551": "692",
  "552": "680",
  "602": "20",
  "603": "213",
  "604": "212",
  "605": "216",
  "606": "218",
  "607": "220",
  "608": "221",
  "609": "222",
  "610": "223",
  "611": "224",
  "612": "225",
  "613": "226",
  "614": "227",
  "615": "228",
  "616": "229",
  "617": "230",
  "618": "231",
  "619": "232",
  "620": "233",
  "621": "234",
  "622": "235",
  "623": "236",
  "624": "237",
  "625": "238",
  "626": "239",
  "627": "240",
  "628": "241",
  "629": "242",
  "630": "243",
  "631": "244",
  "632": "245",
  "633": "248",
  "634": "249",
  "635": "250",
  "636": "251",
  "637": "252",
  "638": "253",
  "639": "254",
  "640": "255",
  "641": "256",
  "642": "257",
  "643": "258",
  "645": "260",
  "646": "261",
  "647": "262",
  "648": "263",
  "649": "264",
  "650": "265",
  "651": "266",
  "652": "267",
  "653": "268",
  "654": "269",
  "655": "27",
  "657": "291",
  "659": "211",
  "702": "501",
  "704": "502",
  "706": "503",
  "708": "504",
  "710": "505",
  "712": "506",
  "714": "507",
  "716": "51",
  "722": "54",
  "724": "55",
  "730": "56",
  "732": "57",
  "734": "58",
  "736": "591",
  "738": "592",
  "740": "593",
  "744": "595",
  "746": "597",
  "748": "598",
};

export type NumberPreview =
  | { status: "empty" }
  /** 号码含字母:后端会直接拒绝(vanity 号码静默剔除会变成错误号码) */
  | { status: "letters" }
  /** 已是国际格式/短号,原样(或去杂符后)发送 */
  | { status: "ready"; value: string }
  /** ≤6 位客服/应急短号:不加国家码 */
  | { status: "short"; value: string }
  /** 长号码:发送时由后端按 IMSI MCC 自动补国家码,预览给出预期结果 */
  | { status: "auto"; value: string }
  /** 长号码但 IMSI 未就绪:发送时后端将报"模块未就绪/无有效 IMSI" */
  | { status: "imsi-missing"; value: string }
  /** IMSI 的 MCC 未配置国家码:提示改用国际格式输入 */
  | { status: "mcc-missing"; value: string; mcc: string };

/** 从 AT+CIMI 原始应答/IMSI 串中提取 10–20 位纯数字 IMSI(对齐后端 parseCIMIResponseIMSI)。 */
export function parseImsi(raw: string | null | undefined): string {
  for (const line of String(raw ?? "").split(/\r?\n/)) {
    const digits = line.replace(/\D/g, "");
    if (/^\d{10,20}$/.test(digits)) return digits;
  }
  return "";
}

/**
 * 号码归一化预览(纯前端,口径对齐后端 normalizeSMSNumber;发送时后端仍会再归一):
 * 00/+ 前缀 → 去杂符保留 +;≤6 位短号原样;长号码剥一个前导 0 后按 IMSI MCC 补 +国家码。
 */
export function normalizeNumberPreview(
  input: string,
  imsiRaw: string | null | undefined,
): NumberPreview {
  const trimmed = String(input ?? "").trim();
  if (/[A-Za-z]/.test(trimmed)) return { status: "letters" };
  const clean = trimmed.replace(/[^0-9+]/g, "");
  if (clean.startsWith("00")) {
    const digits = clean.slice(2).replace(/\D/g, "");
    // "00"/"00-" 等退化输入不得拼出裸 "+",按空号码处理。
    return digits === "" ? { status: "empty" } : { status: "ready", value: `+${digits}` };
  }
  if (clean.startsWith("+")) {
    const digits = clean.replace(/\D/g, "");
    return digits === "" ? { status: "empty" } : { status: "ready", value: `+${digits}` };
  }
  let digits = clean.replace(/\D/g, "");
  if (digits === "") return { status: "empty" };
  if (digits.length <= 6) return { status: "short", value: digits };
  // 国内长途/本地拨号的一个前导 0 剥离,避免拼出 +86010... 不可路由号码。
  if (digits.startsWith("0")) digits = digits.slice(1);

  const imsi = parseImsi(imsiRaw);
  const value = digits;
  if (imsi.length < 3) return { status: "imsi-missing", value };
  const mcc = imsi.slice(0, 3);
  const callingCode = MCC_CALLING_CODES[mcc];
  if (!callingCode) return { status: "mcc-missing", value, mcc };
  return { status: "auto", value: `+${callingCode}${digits}` };
}

// ---------------------------------------------------------------------------
// UTF-16 码元计数与分段(对齐后端 sms_pdu.go:每段 70 码元,代理对不拆)
// ---------------------------------------------------------------------------

/** 单个 SMS 段的 UTF-16 码元上限。 */
export const SMS_SEGMENT_UNITS = 70;

/** UTF-16 码元数 = JS string.length(emoji 等增补字符按代理对计 2 码元)。 */
export function utf16Units(text: string): number {
  return String(text ?? "").length;
}

/** 分段数:0 码元 0 段,否则 ceil(units/70)。 */
export function segmentCount(text: string): number {
  const units = utf16Units(text);
  return units === 0 ? 0 : Math.ceil(units / SMS_SEGMENT_UNITS);
}

// ---------------------------------------------------------------------------
// AT+CPMS? / AT+CSCA? 应答解析(存储可视化条)
// ---------------------------------------------------------------------------

/**
 * 拉取存储占用 + SMSC 的组合命令:切 SM 读占用 → 回置 ME(与后端列表读取的
 * 收尾一致)读占用 → 读 SMSC。多命令行分号后的后续命令不得再带 AT 前缀
 * (3GPP TS 27.007),否则真机语法错误/AT 通道超时且 CPMS 滞留 SM。
 */
export const SMS_STORAGE_AT_COMMAND = 'AT+CPMS="SM";+CPMS?;+CPMS="ME";+CPMS?;+CSCA?';

export interface SmsStorageInfo {
  used: number;
  total: number;
}

export interface SmsStorageStatus {
  me: SmsStorageInfo | null;
  sm: SmsStorageInfo | null;
  /** SMSC 号码(AT+CSCA?),未解析到为 null */
  smsc: string | null;
}

/** 容量缺省值:ME 255 / SM 40(应答缺 total 或为 0 时回退,口径同设计方案 §6.9)。 */
export const ME_DEFAULT_TOTAL = 255;
export const SM_DEFAULT_TOTAL = 40;

function splitCsvLine(value: string): string[] {
  return (value.match(/"[^"]*"|[^,]+/g) ?? []).map((item) => item.trim().replace(/^"|"$/g, ""));
}

/**
 * UCS2 十六进制串按需解码(口径对齐后端 at_parse_util.go decodeMaybeUCS2):
 * 去空白后 长度%4==0 且纯 hex 且至少含一个 A-Fa-f 字母才按 4 位一组解码;
 * 纯数字串同为合法 hex,须防误判,原样返回。
 */
export function decodeMaybeUcs2(value: string): string {
  const clean = String(value ?? "").trim().replace(/ /g, "");
  if (clean === "" || clean.length % 4 !== 0 || !/^[0-9A-Fa-f]+$/.test(clean)) return value;
  if (!/[A-Fa-f]/.test(clean)) return value;
  let decoded = "";
  for (let i = 0; i < clean.length; i += 4) {
    decoded += String.fromCharCode(parseInt(clean.slice(i, i + 4), 16));
  }
  return decoded;
}

/**
 * 解析 AT+CPMS? / AT+CSCA? 组合应答:
 * +CPMS: <mem1>,<used1>,<total1>,<mem2>,... 每三元组按存储名归档(ME/SM 首个命中生效);
 * +CSCA: "+86138..." 取首个号码字段。任一缺失返回 null 分量(界面显示占位)。
 */
export function parseSmsStorageResponse(raw: string | null | undefined): SmsStorageStatus {
  const status: SmsStorageStatus = { me: null, sm: null, smsc: null };
  for (const rawLine of String(raw ?? "").split(/\r?\n/)) {
    const line = rawLine.trim();
    if (line.startsWith("+CPMS:")) {
      const fields = splitCsvLine(line.slice("+CPMS:".length));
      for (let i = 0; i + 2 < fields.length; i += 3) {
        const name = fields[i].toUpperCase();
        if (name !== "ME" && name !== "SM") continue;
        const used = Number(fields[i + 1]);
        const total = Number(fields[i + 2]);
        if (!Number.isFinite(used)) continue;
        const fallbackTotal = name === "ME" ? ME_DEFAULT_TOTAL : SM_DEFAULT_TOTAL;
        const info: SmsStorageInfo = {
          used: Math.max(0, used),
          total: Number.isFinite(total) && total > 0 ? total : fallbackTotal,
        };
        if (name === "ME" && status.me === null) status.me = info;
        if (name === "SM" && status.sm === null) status.sm = info;
      }
    } else if (line.startsWith("+CSCA:") && status.smsc === null) {
      const fields = splitCsvLine(line.slice("+CSCA:".length));
      const value = (fields[0] ?? "").trim();
      if (value !== "") status.smsc = decodeMaybeUcs2(value);
    }
  }
  return status;
}

export type StorageTone = "domain" | "warning" | "danger";

/** 占用预警色阶:>90% rose / >80% amber / 其余域色渐变(设计方案 §6.9)。 */
export function storageTone(used: number, total: number): StorageTone {
  if (!(total > 0)) return "domain";
  const percent = (used / total) * 100;
  if (percent > 90) return "danger";
  if (percent > 80) return "warning";
  return "domain";
}

export function storagePercent(info: SmsStorageInfo | null): number {
  if (!info || !(info.total > 0)) return 0;
  return Math.max(0, Math.min(100, (info.used / info.total) * 100));
}

// ---------------------------------------------------------------------------
// CMS 错误码释义字典(设计方案 §6.9 新增点)
// ---------------------------------------------------------------------------

/**
 * CMS 错误码 → sms ns i18n 键(释义口径对齐后端 smsSendErrorHint,
 * 350 为本固件最常见:运营商拒绝发送,物联卡常见)。字典化可扩展。
 */
export const CMS_ERROR_HINT_KEYS: Record<string, string> = {
  "304": "cmsError304",
  "305": "cmsError305",
  "310": "cmsError310",
  "330": "cmsError330",
  "331": "cmsError331",
  "332": "cmsError332",
  "341": "cmsError341",
  "342": "cmsError342",
  "350": "cmsError350",
};

/** 未知 CMS 码的通用回退释义键。 */
export const CMS_ERROR_GENERIC_KEY = "cmsErrorGeneric";

export interface CmsErrorInfo {
  family: "CMS" | "CME";
  code: string;
  /** 展示标签,如 "+CMS ERROR 350" */
  label: string;
  /** sms ns 释义键;未收录的码回退通用释义 */
  hintKey: string;
}

/**
 * 从发送失败文本中提取 CMS/CME 错误码并给出释义键。
 * 兼容三种来源:+CMS ERROR: 350(原始 AT 应答)、CMS 350(后端 smsSendErrorKind
 * 归一格式)、CMS 350(中文提示)(归一格式 + 后端提示)。无错误码返回 null。
 */
export function explainCmsError(text: string | null | undefined): CmsErrorInfo | null {
  const upper = String(text ?? "").toUpperCase();
  const matched =
    /\+?\b(CMS|CME)\s+ERROR:?\s*(\d+)/.exec(upper) ?? /\b(CMS|CME)\s+(\d{3,})\b/.exec(upper);
  if (!matched) return null;
  const family = matched[1] as "CMS" | "CME";
  const code = matched[2];
  return {
    family,
    code,
    label: `+${family} ERROR ${code}`,
    hintKey: (family === "CMS" && CMS_ERROR_HINT_KEYS[code]) || CMS_ERROR_GENERIC_KEY,
  };
}

// ---------------------------------------------------------------------------
// 发件人头像色相(名字哈希取域内色相:comm 域 violet→fuchsia 区间 250–310)
// ---------------------------------------------------------------------------

export function avatarHue(name: string): number {
  const text = String(name ?? "");
  let hash = 0;
  for (let i = 0; i < text.length; i += 1) {
    hash = (hash * 31 + text.charCodeAt(i)) >>> 0;
  }
  return 250 + (hash % 61);
}

/** 头像显示字符:首字符大写;空号码/空名回退 "#"。 */
export function avatarInitial(name: string): string {
  const text = String(name ?? "").trim();
  if (text === "") return "#";
  return Array.from(text)[0].toUpperCase();
}
