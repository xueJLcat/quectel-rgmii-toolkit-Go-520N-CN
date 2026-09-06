// 登录流:未认证重定向、登录页挂载(非白屏)、1 次错误密码提示、正确凭据进入 /#/dashboard。
// 安全红线:失败登录尝试全程仅此 1 次(后端 5 次即 429 锁定,会污染后续用例);
// 错误尝试与成功登录放在同一用例内顺序执行,保证次序与计数确定。
import { test, expect } from "@playwright/test";

// 未认证流:清空 storageState,不复用 auth.setup 的会话 Cookie
test.use({ storageState: { cookies: [], origins: [] } });

test("未认证访问 / 落在 /login.html", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login\.html$/);
});

test("登录表单挂载成功(非白屏)", async ({ page }) => {
  await page.goto("/login.html");
  // React root 挂载:#root 非空且表单控件可见
  await expect(page.locator("#root")).not.toBeEmpty();
  await expect(page.locator("#login-username")).toBeVisible();
  await expect(page.locator("#login-password")).toBeVisible();
  await expect(page.getByRole("button", { name: "登录" })).toBeVisible();
});

test("1 次错误密码出现错误提示,正确凭据进入 /#/dashboard", async ({ page }) => {
  await page.goto("/login.html");
  await page.locator("#login-username").fill("admin");
  await page.locator("#login-password").fill("wrong-password-only-once");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.locator('[role="alert"]')).toContainText("用户名或密码错误");

  // 同上下文立即用正确凭据登录(整套件失败尝试计数到此为止:1 次)
  await page.locator("#login-password").fill("admin");
  await page.getByRole("button", { name: "登录" }).click();
  await page.waitForURL(/\/#\/dashboard$/, { timeout: 20_000 });
  await expect(page.locator('[data-slot="connection-hero"]')).toBeVisible({ timeout: 20_000 });
});
