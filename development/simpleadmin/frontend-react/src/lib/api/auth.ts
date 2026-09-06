import { FORM_CONTENT_TYPE } from "./gateway";

export interface LoginResult {
  ok: boolean;
  redirect?: string;
  error?: string;
  retry_after?: number;
}

export interface LogoutResult {
  ok: boolean;
  redirect?: string;
  error?: string;
}

export interface ModuleModel {
  model?: string;
  pending?: boolean;
}

const defaultFetchImpl: typeof fetch = (input, init) => fetch(input, init);

async function readJSON(response: Response): Promise<Record<string, unknown>> {
  try {
    const data: unknown = await response.json();
    return data && typeof data === "object" ? (data as Record<string, unknown>) : {};
  } catch {
    // JSON 解析失败按空对象容错,成败仍由 HTTP 状态与 ok 字段决定
    return {};
  }
}

function toFiniteNumber(value: unknown): number | undefined {
  if (value === undefined || value === null || value === "") return undefined;
  const num = Number(value);
  return Number.isFinite(num) ? num : undefined;
}

function retryAfterHeaderValue(response: Response): string | null {
  return typeof response.headers?.get === "function" ? response.headers.get("Retry-After") : null;
}

export async function login(
  username: string,
  password: string,
  fetchImpl: typeof fetch = defaultFetchImpl,
): Promise<LoginResult> {
  const response = await fetchImpl("/api/login", {
    method: "POST",
    cache: "no-store",
    credentials: "same-origin",
    headers: { "Content-Type": FORM_CONTENT_TYPE, Accept: "application/json" },
    body: new URLSearchParams({ username, password }).toString(),
  });
  const data = await readJSON(response);
  const result: LoginResult = { ok: response.ok && data.ok === true };
  if (typeof data.redirect === "string") result.redirect = data.redirect;
  if (!result.ok) {
    result.error =
      typeof data.error === "string" && data.error !== ""
        ? data.error
        : `login failed (HTTP ${response.status})`;
  }
  const retryAfter =
    toFiniteNumber(data.retry_after) ??
    (response.status === 429 ? toFiniteNumber(retryAfterHeaderValue(response)) : undefined);
  if (retryAfter !== undefined) result.retry_after = retryAfter;
  return result;
}

export async function logout(fetchImpl: typeof fetch = defaultFetchImpl): Promise<LogoutResult> {
  const response = await fetchImpl("/api/logout", {
    method: "POST",
    cache: "no-store",
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  });
  const data = await readJSON(response);
  const result: LogoutResult = { ok: response.ok && data.ok === true };
  if (typeof data.redirect === "string") result.redirect = data.redirect;
  if (!result.ok && typeof data.error === "string" && data.error !== "") {
    result.error = data.error;
  }
  return result;
}

export async function fetchModuleModel(
  fetchImpl: typeof fetch = defaultFetchImpl,
): Promise<ModuleModel> {
  const response = await fetchImpl("/api/module_model", {
    cache: "no-store",
    credentials: "same-origin",
  });
  const text = await response.text();
  if (!response.ok) {
    throw new Error(`module_model failed (HTTP ${response.status})`);
  }
  return (text.trim() ? JSON.parse(text) : {}) as ModuleModel;
}
