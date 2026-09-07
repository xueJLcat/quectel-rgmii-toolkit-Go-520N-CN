import type { MockInstance } from "vitest";
import { gateway } from "./gateway";
import type { GatewayResponse } from "./gateway";
import {
  ApiError,
  dashboardData,
  getJSON,
  getText,
  getUptime,
  historyData,
  languageSet,
  moduleModel,
  postForm,
  setPassword,
  smsData,
  smsForwardTest,
  smsServerChanSet,
  smsWebhookSet,
  systemMonitor,
  watchdogSet,
} from "./endpoints";
import { fetchModuleModel, login, logout } from "./auth";

type RequestSpy = MockInstance<typeof gateway.request>;

let requestSpy: RequestSpy;

beforeEach(() => {
  requestSpy = vi.spyOn(gateway, "request");
});

afterEach(() => {
  vi.restoreAllMocks();
});

function jsonResponse(status: number, body: string): GatewayResponse {
  return { status, headers: {}, body };
}

const FORM_HEADERS = {
  headers: { "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8" },
};

function mockResponse(init: {
  ok?: boolean;
  status?: number;
  body?: unknown;
  text?: string;
  headers?: Record<string, string>;
  jsonError?: boolean;
}): Response {
  return {
    ok: init.ok ?? true,
    status: init.status ?? 200,
    headers: new Headers(init.headers ?? {}),
    json: () =>
      init.jsonError ? Promise.reject(new Error("invalid json")) : Promise.resolve(init.body ?? {}),
    text: () => Promise.resolve(init.text ?? JSON.stringify(init.body ?? {})),
  } as unknown as Response;
}

describe("endpoints base helpers", () => {
  it("getJSON 拼接 query 参数并跳过 undefined/null", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"a":1}'));
    await expect(
      getJSON<{ a: number }>("/api/x", { a: "1", b: undefined, c: 2, d: null, e: true }),
    ).resolves.toEqual({ a: 1 });
    expect(requestSpy).toHaveBeenCalledWith("GET", "/api/x?a=1&c=2&e=true");
  });

  it("getJSON 空 body 返回 null", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, ""));
    await expect(getJSON("/api/x")).resolves.toBeNull();
  });

  it("getJSON 非 2xx 抛 ApiError{status,body}", async () => {
    requestSpy.mockResolvedValue(jsonResponse(500, "boom"));
    const error = await getJSON("/api/x").then(
      () => null,
      (caught: unknown) => caught,
    );
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(500);
    expect((error as ApiError).body).toBe("boom");
  });

  it("postForm 以 x-www-form-urlencoded 序列化 body", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"ok":true}'));
    await expect(postForm("/api/y", { x: "a b", y: "中文", z: undefined, n: 3 })).resolves.toEqual({
      ok: true,
    });
    expect(requestSpy).toHaveBeenCalledWith(
      "POST",
      "/api/y",
      "x=a+b&y=%E4%B8%AD%E6%96%87&n=3",
      FORM_HEADERS,
    );
  });

  it("getText 返回 text/plain 端点原始文本", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, "up 1 day\n"));
    await expect(getText("/api/get_uptime")).resolves.toBe("up 1 day\n");
    expect(requestSpy).toHaveBeenCalledWith("GET", "/api/get_uptime");
  });
});

describe("endpoints domain wrappers", () => {
  it("dashboardData 默认 action=get,pending 原样返回", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"pending":true,"sim":"未激活"}'));
    await expect(dashboardData({ force: 1 })).resolves.toMatchObject({
      pending: true,
      sim: "未激活",
    });
    expect(requestSpy).toHaveBeenCalledWith(
      "POST",
      "/api/dashboard_data",
      "action=get&force=1",
      FORM_HEADERS,
    );
  });

  it("historyData 空表单 POST", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"intervalSeconds":60,"points":[]}'));
    await expect(historyData()).resolves.toEqual({ intervalSeconds: 60, points: [] });
    expect(requestSpy).toHaveBeenCalledWith("POST", "/api/history_data", "", FORM_HEADERS);
  });

  it("systemMonitor 可选 sort 参数", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"processes":[]}'));
    await systemMonitor("mem");
    expect(requestSpy).toHaveBeenLastCalledWith(
      "POST",
      "/api/system_monitor",
      "sort=mem",
      FORM_HEADERS,
    );
    await systemMonitor();
    expect(requestSpy).toHaveBeenLastCalledWith("POST", "/api/system_monitor", "", FORM_HEADERS);
  });

  it("smsData delete_indices 参数编码", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"ok":true,"deleted":2,"total":2}'));
    await expect(smsData("delete_indices", { indices: "ME:1,ME:2" })).resolves.toMatchObject({
      deleted: 2,
    });
    expect(requestSpy).toHaveBeenCalledWith(
      "POST",
      "/api/sms_data",
      "action=delete_indices&indices=ME%3A1%2CME%3A2",
      FORM_HEADERS,
    );
  });

  it("smsWebhookSet 扩展参数字段编码(多行 headers 与模板)", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"ok":true}'));
    await expect(
      smsWebhookSet({
        enabled: "1",
        url: "https://x/h",
        method: "GET",
        headers: "A: B",
        timeoutSec: "30",
        template: `{"m":"{text}"}`,
      }),
    ).resolves.toEqual({ ok: true });
    expect(requestSpy).toHaveBeenCalledWith(
      "POST",
      "/api/set_sms_webhook",
      "enabled=1&url=https%3A%2F%2Fx%2Fh&method=GET&headers=A%3A+B&timeoutSec=30&template=%7B%22m%22%3A%22%7Btext%7D%22%7D",
      FORM_HEADERS,
    );
  });

  it("smsServerChanSet 提交 enabled+sendKey", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"ok":true}'));
    await expect(smsServerChanSet({ enabled: "1", sendKey: "SCT123Tabc" })).resolves.toEqual({
      ok: true,
    });
    expect(requestSpy).toHaveBeenCalledWith(
      "POST",
      "/api/set_sms_serverchan",
      "enabled=1&sendKey=SCT123Tabc",
      FORM_HEADERS,
    );
  });

  it("smsForwardTest 按通道提交 channel 参数", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"ok":true}'));
    await expect(smsForwardTest("serverchan")).resolves.toEqual({ ok: true });
    expect(requestSpy).toHaveBeenCalledWith(
      "POST",
      "/api/test_sms_forward",
      "channel=serverchan",
      FORM_HEADERS,
    );
  });

  it("languageSet 走 POST + query 参数", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"language":"zh-CN"}'));
    await expect(languageSet("zh-CN")).resolves.toEqual({ language: "zh-CN" });
    expect(requestSpy).toHaveBeenCalledWith("POST", "/api/set_language?language=zh-CN");
  });

  it("setPassword 提交三个密码字段", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, '{"ok":true}'));
    await expect(setPassword("old", "new", "new")).resolves.toEqual({ ok: true });
    expect(requestSpy).toHaveBeenCalledWith(
      "POST",
      "/api/set_password",
      "current_password=old&new_password=new&confirm_password=new",
      FORM_HEADERS,
    );
  });

  it("watchdogSet 返回 ok+config", async () => {
    requestSpy.mockResolvedValue(
      jsonResponse(200, '{"ok":true,"config":{"enabled":true,"failThreshold":3}}'),
    );
    await expect(watchdogSet({ enabled: true })).resolves.toMatchObject({
      ok: true,
      config: { enabled: true },
    });
    expect(requestSpy).toHaveBeenCalledWith(
      "POST",
      "/api/set_watchdog",
      "enabled=true",
      FORM_HEADERS,
    );
  });

  it("getUptime 走网关 GET", async () => {
    requestSpy.mockResolvedValue(jsonResponse(200, "up 0 day, 1 hour, 2 min\n"));
    await expect(getUptime()).resolves.toBe("up 0 day, 1 hour, 2 min\n");
    expect(requestSpy).toHaveBeenCalledWith("GET", "/api/get_uptime");
  });
});

describe("moduleModel / auth (直接 HTTP fetch)", () => {
  it("moduleModel 直接 fetch /api/module_model,不走网关", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(mockResponse({ body: { model: "RG520N-CN", pending: false } }));
    await expect(moduleModel(fetchMock)).resolves.toEqual({ model: "RG520N-CN", pending: false });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/module_model",
      expect.objectContaining({ credentials: "same-origin" }),
    );
    expect(requestSpy).not.toHaveBeenCalled();
    await expect(fetchModuleModel(fetchMock)).resolves.toEqual({
      model: "RG520N-CN",
      pending: false,
    });
  });

  it("login 成功返回 ok+redirect,form 编码提交", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(mockResponse({ body: { ok: true, redirect: "/" } }));
    await expect(login("admin", "secret", fetchMock)).resolves.toEqual({ ok: true, redirect: "/" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/login",
      expect.objectContaining({ method: "POST", credentials: "same-origin" }),
    );
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(init.body).toBe("username=admin&password=secret");
    expect((init.headers as Record<string, string>)["Content-Type"]).toBe(
      "application/x-www-form-urlencoded; charset=UTF-8",
    );
  });

  it("login 429 返回 retry_after(body 优先,缺省回退 Retry-After 头)", async () => {
    const withBody = vi.fn<typeof fetch>().mockResolvedValue(
      mockResponse({
        ok: false,
        status: 429,
        body: { ok: false, error: "too many failed attempts", retry_after: 30 },
        headers: { "Retry-After": "45" },
      }),
    );
    await expect(login("admin", "bad", withBody)).resolves.toEqual({
      ok: false,
      error: "too many failed attempts",
      retry_after: 30,
    });

    const headerOnly = vi.fn<typeof fetch>().mockResolvedValue(
      mockResponse({
        ok: false,
        status: 429,
        body: { ok: false },
        headers: { "Retry-After": "45" },
      }),
    );
    await expect(login("admin", "bad", headerOnly)).resolves.toMatchObject({
      ok: false,
      retry_after: 45,
    });
  });

  it("login JSON 解析失败容错", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(mockResponse({ ok: false, status: 401, jsonError: true }));
    await expect(login("admin", "bad", fetchMock)).resolves.toEqual({
      ok: false,
      error: "login failed (HTTP 401)",
    });
  });

  it("logout POST 且 Accept: application/json", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(mockResponse({ body: { ok: true, redirect: "/login.html" } }));
    await expect(logout(fetchMock)).resolves.toEqual({ ok: true, redirect: "/login.html" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/logout",
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
        headers: { Accept: "application/json" },
      }),
    );
  });
});
