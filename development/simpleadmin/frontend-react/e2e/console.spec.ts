// 控制台页:xterm 经 /api/console/ws 连上后端本机 PTY(Linux 下 native_console.go
// 启动 /bin/sh,非真机、不涉及任何外部目标)。断言终端挂载 + 连接状态「已连接」。
//
// 降级说明(不做键入回显断言):xterm 默认 canvas 渲染且未开 screenReaderMode,
// 回显文本不进入 DOM,无法稳定断言;按任务口径只保留连接断言以避免 flaky。
import { test, expect } from "@playwright/test";

test("xterm 挂载并显示连接状态「已连接」", async ({ page }) => {
  await page.goto("/#/console");
  await expect(page.locator(".xterm")).toBeVisible({ timeout: 30_000 });

  // Topbar VitalChips 也会显示「已连接」(互联网状态),必须限定在终端面板内断言
  const panel = page.locator('[data-slot="panel"]').filter({ has: page.locator(".xterm") });
  await expect(panel.getByText("已连接")).toBeVisible({ timeout: 30_000 });
  // 输入通路就位(xterm 隐藏 textarea 承载键盘输入)
  await expect(page.locator(".xterm-helper-textarea")).toBeAttached();
});

test("终端搜索浮层:Ctrl+F 打开、输入即搜、Esc 关闭", async ({ page }) => {
  await page.goto("/#/console");
  await expect(page.locator(".xterm")).toBeVisible({ timeout: 30_000 });
  const panel = page.locator('[data-slot="panel"]').filter({ has: page.locator(".xterm") });
  await expect(
    panel.getByRole("button", { name: "搜索终端输出" }),
  ).toBeVisible({ timeout: 30_000 });

  // 终端聚焦后 Ctrl+F 唤起(拦截浏览器页内查找),浮层内输入触发真实 SearchAddon
  await panel.locator(".xterm").click();
  await page.keyboard.press("Control+f");
  const search = panel.getByRole("search");
  await expect(search).toBeVisible();
  await search.locator("input").fill("root");
  await page.keyboard.press("Escape");
  await expect(search).toHaveCount(0);

  // 工具栏按钮同样可开关
  await panel.getByRole("button", { name: "搜索终端输出" }).click();
  await expect(search).toBeVisible();
  await panel.getByRole("button", { name: "关闭搜索" }).click();
  await expect(search).toHaveCount(0);
});
