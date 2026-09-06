import { POST_LOGIN_HASH_KEY } from "@/lib/api/gateway";

/**
 * 校验登录跳转目标,与后端 sanitizeLoginNext(server_auth.go)语义一致:
 * 只接受单个斜杠开头的同站路径(拒绝 // 协议相对跳转,防开放重定向)与
 * # 开头的同页锚点(#x 归一为 /#x);绝对 URL、反斜杠、控制字符、空串
 * 一律回退 "/"。
 */
export function sanitizeNext(value: string): string {
  const next = value.trim();
  if (next === "") return "/";
  for (const ch of next) {
    const code = ch.codePointAt(0) ?? 0;
    if (code <= 0x20 || code === 0x7f || ch === "\\") return "/";
  }
  if (next.startsWith("#")) return `/${next}`;
  if (/^\/(?![/\\])/.test(next)) return next;
  return "/";
}

/** 读取并删除 gateway 401 跳转时暂存的 hash(形如 "#dashboard"),不可用时静默返回空串。 */
function consumePostLoginHash(): string {
  try {
    const hash = sessionStorage.getItem(POST_LOGIN_HASH_KEY) ?? "";
    sessionStorage.removeItem(POST_LOGIN_HASH_KEY);
    return hash;
  } catch {
    // sessionStorage 不可用(隐私模式等)时跳过恢复
    return "";
  }
}

/**
 * 登录成功后的跳转目标:优先 ?next=(经 sanitizeNext 校验),其次
 * sessionStorage 暂存 hash(读后即删,拼到 "/" 的 hash),最后回退 "/"。
 */
export function resolvePostLoginTarget(search: string = window.location.search): string {
  const rawNext = new URLSearchParams(search).get("next");
  if (rawNext !== null) {
    const target = sanitizeNext(rawNext);
    if (target !== "/") return target;
  }
  const hash = consumePostLoginHash();
  if (hash.startsWith("#") && hash.length > 1) return `/${hash}`;
  return "/";
}
