// 短信转发页纯函数:通道参数校验(Webhook URL/请求方式/超时/请求头/JSON 模板,
// Server酱 SendKey)。校验口径与后端 writeSMSWebhookConfig /
// serverChanSendURL 一致,前端拦截只为即时反馈,最终以后端为准。

export const WEBHOOK_METHODS = ["POST", "GET", "PUT"] as const;
export type WebhookMethod = (typeof WEBHOOK_METHODS)[number];

export const WEBHOOK_TIMEOUT_RANGE = { min: 1, max: 120 } as const;

/** 模板占位符(单花括号,与后端 smsWebhookTemplateKeys 一致)。 */
export const WEBHOOK_TEMPLATE_PLACEHOLDERS = [
  "{sender}",
  "{date}",
  "{text}",
  "{index}",
  "{storage}",
] as const;

/** Server酱 官网(注册获取 SendKey / 切换推送通道)。 */
export const SERVERCHAN_SITE_URL = "https://sct.ftqq.com";

/** URL 校验:仅接受 http/https 回调地址(旧版 placeholder 口径)。 */
export function isWebhookUrlValid(url: string): boolean {
  return /^https?:\/\/\S+$/i.test(url.trim());
}

export function normalizeWebhookMethod(value?: string): WebhookMethod {
  const upper = (value ?? "").trim().toUpperCase();
  return upper === "GET" || upper === "PUT" ? upper : "POST";
}

export function isWebhookTimeoutValid(value: string): boolean {
  const n = Number(value);
  return (
    Number.isInteger(n) &&
    n >= WEBHOOK_TIMEOUT_RANGE.min &&
    n <= WEBHOOK_TIMEOUT_RANGE.max
  );
}

/**
 * 请求头校验:每行 "Name: Value"(空行忽略),返回首个非法行的行号(1 起);
 * 全部合法返回 null。名字为空或含空白即非法,与后端 parseSMSWebhookHeaders 同口径。
 */
export function findInvalidHeaderLine(headers: string): number | null {
  const lines = headers.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (line === "") continue;
    const colon = line.indexOf(":");
    const name = colon >= 0 ? line.slice(0, colon).trim() : "";
    if (colon < 0 || name === "" || /\s/.test(name)) return i + 1;
  }
  return null;
}

/** 模板校验占位符的样例取值(与后端 smsWebhookTemplateSample 同构,含引号/换行)。 */
const TEMPLATE_SAMPLE: Record<string, string> = {
  "{sender}": "+8613800000000",
  "{date}": "26/01/02,03:04:05+32",
  "{text}": '样例 "引号" \\反斜杠\\ \n换行',
  "{index}": "0",
  "{storage}": "ME",
};

/**
 * JSON 模板校验:占位符替换为样例值后必须可被 JSON.parse。
 * 字符串占位符按 JSON 转义替换(引号/换行不破坏结构),与后端渲染口径一致。
 */
export function isWebhookTemplateValid(template: string): boolean {
  if (template.trim() === "") return true;
  let rendered = template;
  for (const [placeholder, sample] of Object.entries(TEMPLATE_SAMPLE)) {
    const replacement =
      placeholder === "{index}" ? sample : JSON.stringify(sample).slice(1, -1);
    rendered = rendered.replaceAll(placeholder, replacement);
  }
  try {
    JSON.parse(rendered);
    return true;
  } catch {
    return false;
  }
}

/**
 * Server酱 SendKey 校验:非空、不含空白;
 * sctp 开头(Server酱³)须为 sctp{数字}t… 形态(端点推导需要 uid),
 * 其余(Turbo,SCT 开头)不强制前缀——与官方 SDK 的宽松识别一致,
 * 但拒绝 URL 保留字符防止拼进端点路径时注入。
 */
export function isServerChanKeyValid(key: string): boolean {
  const trimmed = key.trim();
  if (trimmed === "" || /\s/.test(trimmed)) return false;
  if (/[/?#%]/.test(trimmed)) return false;
  if (trimmed.startsWith("sctp")) return /^sctp\d+t\S+$/.test(trimmed);
  return true;
}
