// 主题/语言切换:侧边栏底部按钮驱动。
// 主题 = html[data-bs-theme] + localStorage("theme") 持久化(浏览器侧,刷新保持);
// 语言 = i18next + localStorage("simpleadmin.language"),英文词条已对照
// locales/en/nav.json 核实(overview=Overview、systemSettings=System Settings)。
import { test, expect } from "@playwright/test";

test("切主题:html[data-bs-theme] 翻转且刷新后保持", async ({ page }) => {
  await page.goto("/#/dashboard");
  await expect(page.locator("html")).toHaveAttribute("data-bs-theme", "light");

  const footer = page.locator('[data-slot="sidebar-footer"]');
  await footer.getByRole("button", { name: "暗夜模式" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-bs-theme", "dark");

  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-bs-theme", "dark");
});

test("切语言:界面出现英文词条且刷新后保持", async ({ page }) => {
  await page.goto("/#/dashboard");
  await expect(page.locator('[data-slot="page-header"] h1')).toHaveText("总览");

  const footer = page.locator('[data-slot="sidebar-footer"]');
  // 中文态语言按钮显示 "中"(title=English)
  await footer.getByRole("button", { name: "中", exact: true }).click();
  await expect(page.locator('[data-slot="page-header"] h1')).toHaveText("Overview");
  await expect(page.locator("html")).toHaveAttribute("lang", "en");
  await expect(
    page
      .locator('aside[data-slot="sidebar"]')
      .getByRole("link", { name: "System Settings", exact: true }),
  ).toBeVisible();

  await page.reload();
  await expect(page.locator('[data-slot="page-header"] h1')).toHaveText("Overview");

  // 切回中文:恢复服务端语言配置(set_language 为服务端持久化,避免影响后续观察)
  await footer.getByRole("button", { name: "EN", exact: true }).click();
  await expect(page.locator('[data-slot="page-header"] h1')).toHaveText("总览");
});
