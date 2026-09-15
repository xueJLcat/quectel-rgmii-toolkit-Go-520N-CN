// 总览页 mock 数据上屏(数据源已核实:go-build/.../mock_at_responses.go 与
// /api/dashboard_data mock 应答:internetConnection=已连接、network_provider=中国电信、
// cpu/ram/信号/温度四仪表、历史趋势为进程内环形缓冲)。
import { test, expect } from "@playwright/test";

test("英雄条显示已连接与运营商 mock 数据", async ({ page }) => {
  await page.goto("/#/dashboard");
  const hero = page.locator('[data-slot="connection-hero"]');
  await expect(hero).toBeVisible({ timeout: 20_000 });
  await expect(hero).toHaveAttribute("data-online", "true");
  await expect(hero).toContainText("已连接");
  await expect(hero).toContainText("中国电信");
  await expect(hero).toContainText("NR5G-SA FDD");
});

test("CPU/RAM 等仪表渲染(ECharts canvas)", async ({ page }) => {
  await page.goto("/#/dashboard");
  await expect(page.locator('[data-slot="connection-hero"]')).toBeVisible({ timeout: 20_000 });
  // 4 联仪表卡(CPU/RAM/信号/温度):ECharts 以 canvas 渲染,卡片 caption 在 DOM
  await expect(page.getByText("实时负载")).toBeVisible();
  await expect(page.getByText(/\d+(\.\d+)? GB \/ \d+(\.\d+)? GB/)).toBeVisible();
  await expect(page.locator("canvas").first()).toBeVisible({ timeout: 20_000 });
  expect(await page.locator("canvas").count()).toBeGreaterThanOrEqual(4);
});

test("趋势图容器渲染", async ({ page }) => {
  await page.goto("/#/dashboard");
  const trend = page.locator('[data-slot="panel"]').filter({ hasText: "信号与流量趋势" });
  await expect(trend).toBeVisible({ timeout: 20_000 });
  // history_data 为进程内环形缓冲(≥60s 才积 1 点,重启清零):
  // 点数 <2 时按设计渲染空态提示,≥2 点渲染 TrendChart canvas——两种形态都算容器渲染成功
  const hasChart = await trend.locator("canvas").count();
  const hasEmptyHint = await trend.getByText(/暂无历史数据/).count();
  expect(hasChart + hasEmptyHint).toBeGreaterThanOrEqual(1);
});
