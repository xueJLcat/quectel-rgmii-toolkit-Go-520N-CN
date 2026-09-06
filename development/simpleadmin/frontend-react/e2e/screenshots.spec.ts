// README 截图重拍:登录态(storageState)、1440×900(全局 viewport)、亮色主题与
// 中文界面(均为服务端/浏览器默认,无 localStorage 覆写),等待 mock 数据渲染稳定后
// 对 8 页整页截图,覆盖写入仓库根 PNG/<同名>.png(替换旧 UI 截图)。
import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import fs from "node:fs";
import { fileURLToPath } from "node:url";

// e2e/ → frontend-react/ → simpleadmin/ → development/ → 仓库根
const PNG_DIR = fileURLToPath(new URL("../../../../PNG", import.meta.url));

const SHOT_PAGES = [
  "dashboard",
  "signal",
  "network",
  "celllock",
  "netdetail",
  "netconfig",
  "firewall",
  "sms",
] as const;

/** 数据渲染稳定锚点:页头出现、骨架屏(aria-busy)清零、页级关键 mock 数据就位。 */
async function waitForStable(page: Page, name: string): Promise<void> {
  await expect(page.locator('[data-slot="page-header"] h1')).toBeVisible({ timeout: 30_000 });
  if (name === "dashboard") {
    await expect(
      page.locator('[data-slot="connection-hero"][data-online="true"]'),
    ).toBeVisible({ timeout: 30_000 });
  }
  if (name === "sms") {
    await expect(page.getByText("10001", { exact: true })).toBeVisible({ timeout: 30_000 });
  }
  if (name === "celllock") {
    await expect(page.locator('[data-slot="lock-status-banner"]')).toBeVisible({ timeout: 30_000 });
  }
  await expect(page.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 30_000 });
  // 字体与入场动画/ECharts 动效落定,避免截到中间帧
  await page.evaluate(() => document.fonts.ready.then(() => undefined));
  await page.waitForTimeout(2_000);
}

for (const name of SHOT_PAGES) {
  test(`截图重拍 ${name}`, async ({ page }) => {
    test.setTimeout(120_000);
    expect(fs.existsSync(PNG_DIR), `PNG 目录不存在: ${PNG_DIR}`).toBe(true);
    await page.goto(`/#/${name}`);
    await waitForStable(page, name);
    await page.screenshot({ path: `${PNG_DIR}/${name}.png`, fullPage: true });
    const stat = fs.statSync(`${PNG_DIR}/${name}.png`);
    expect(stat.size).toBeGreaterThan(10_000);
  });
}
