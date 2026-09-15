// 导航巡检:侧边栏依次点击 15 个页面链接,断言 hash 变化 + PageHeader 标题出现,
// 并全程监听 pageerror(未捕获异常,必须为零)与 console.error(甄别 mock 数据的
// 可预期告警,崩溃级错误必须为零)。
import { test, expect } from "@playwright/test";

import { NAV_PAGES } from "./helpers";

// mock 环境下可预期的 console.error(甄别白名单):均为数据/网络层告警,非页面崩溃。
const BENIGN_CONSOLE_ERRORS: RegExp[] = [
  // 离开控制台页时前端关闭 PTY WebSocket(/api/console/ws,本机 /bin/sh),
  // 服务端可能在 close 握手期间再发一帧 shell 输出,Chromium 记为 console.error;
  // 属遥测性拆除告警(仅本机 console WS,pageerror 仍为零),非页面崩溃级错误。
  /^WebSocket connection to 'ws:\/\/127\.0\.0\.1:\d+\/api\/console\/ws' failed: Data frame received after close$/,
];

test("侧边栏依次走完 16 页:hash 变化 + PageHeader 标题 + 零未捕获异常", async ({ page }) => {
  test.setTimeout(240_000);
  const pageErrors: string[] = [];
  const consoleErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(String(error)));
  page.on("console", (message) => {
    if (message.type() === "error") consoleErrors.push(message.text());
  });

  await page.goto("/#/dashboard");
  const sidebar = page.locator('aside[data-slot="sidebar"]');
  await expect(sidebar).toBeVisible();

  for (const target of NAV_PAGES) {
    await sidebar.getByRole("link", { name: target.title, exact: true }).click();
    await expect(page, `hash 未切到 ${target.id}`).toHaveURL(new RegExp(`/#/${target.id}$`));
    await expect(
      page.locator('[data-slot="page-header"] h1'),
      `${target.id} 页标题未出现`,
    ).toHaveText(target.title);
    // 懒加载 chunk 就位、数据骨架屏(aria-busy)消失后才进入下一页
    await expect(page.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 30_000 });
  }

  expect(pageErrors, `未捕获异常: ${pageErrors.join(" | ")}`).toEqual([]);
  const unexpected = consoleErrors.filter(
    (text) => !BENIGN_CONSOLE_ERRORS.some((pattern) => pattern.test(text)),
  );
  expect(unexpected, "存在未甄别的 console.error").toEqual([]);
});
