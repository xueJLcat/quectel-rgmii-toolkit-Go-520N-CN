// 鉴权 setup:API 直登一次(admin:admin,凭据来自 windows-test/data/simpleadmin.auth),
// 会话 Cookie 持久化为 storageState 供所有已登录用例复用(chromium project 依赖本 setup)。
// 走 API 而非 UI:更快、零 UI 依赖,且不产生任何失败登录计数(限速红线:失败 ≤1 次)。
import { test as setup, expect } from "@playwright/test";

import { AUTH_STATE_PATH } from "./helpers";

setup("authenticate as admin", async ({ request }) => {
  const login = await request.post("/api/login", {
    form: { username: "admin", password: "admin" },
  });
  expect(login.ok()).toBeTruthy();
  expect(await login.json()).toMatchObject({ ok: true });

  // 带会话访问受保护根路径:应直接返回 SPA 壳而非 303 到登录页
  const probe = await request.get("/");
  expect(probe.status()).toBe(200);
  expect(await probe.text()).toContain('id="root"');

  await request.storageState({ path: AUTH_STATE_PATH });
});
