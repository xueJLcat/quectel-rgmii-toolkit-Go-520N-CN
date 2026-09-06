// 网络诊断页。安全红线:探测目标只用本机 mock 服务器自身(http://127.0.0.1:18080),
// DNS 只查 localhost;绝不点击内置目标按钮(223.5.5.5/1.1.1.1/8.8.8.8 为外网地址)
// 与「一键全测」。mock 应答已核实(WS 网关实测 /api/diag_data):
//   http_probe → {ok:true,statusCode:200,latencyMs:N};dns_query localhost → ["127.0.0.1","::1"]
import { test, expect } from "@playwright/test";

test("HTTP 探测本机 mock 服务器出现成功条目(延迟 ms)", async ({ page }) => {
  await page.goto("/#/diag");
  await page.locator("#diag-custom-target").fill("http://127.0.0.1:18080/login.html");
  // exact 只命中自定义目标的「探测」按钮(内置目标按钮可及名包含目标文本,不会误触)
  await page.getByRole("button", { name: "探测", exact: true }).click();

  const timeline = page.locator('[data-slot="probe-timeline"]');
  await expect(timeline).toContainText("127.0.0.1:18080/login.html", { timeout: 30_000 });
  await expect(timeline).toContainText("可达");
  await expect(timeline).toContainText("HTTP 200");
  await expect(timeline.getByText(/\d+ ms/).first()).toBeVisible();
});

test("DNS 查询 localhost 出现地址列表", async ({ page }) => {
  await page.goto("/#/diag");
  await page.locator("#diag-domain").fill("localhost");
  await page.getByRole("button", { name: "查询", exact: true }).click();

  const result = page.locator('[data-slot="dns-result"]');
  await expect(result).toContainText("127.0.0.1", { timeout: 30_000 });
  await expect(result).toContainText("::1");
  await expect(result.getByText(/解析耗时 \d+ ms/)).toBeVisible();
});
