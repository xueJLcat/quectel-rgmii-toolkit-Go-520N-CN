// AT 命令页:发送 ATI,终端输出区([data-slot="at-terminal"])出现 mock 响应。
// mock 响应已核实(go-build/.../mock_at_responses.go:101):
//   "ATI\r\nQuectel\r\nRG520N-CN\r\nRevision: RG520NCNAAR02A02M4G_XM\r\nOK\r\n"
import { test, expect } from "@playwright/test";

test("发送 ATI 出现 mock 模块标识响应", async ({ page }) => {
  await page.goto("/#/atcommands");
  const input = page.getByLabel("AT 命令输入");
  await expect(input).toBeVisible();
  await input.fill("ATI");
  await page.getByRole("button", { name: "发送" }).click();

  const terminal = page.locator('[data-slot="at-terminal"]');
  await expect(terminal).toContainText("Quectel", { timeout: 30_000 });
  await expect(terminal).toContainText("RG520N-CN");
  await expect(terminal).toContainText("Revision: RG520NCNAAR02A02M4G_XM");
  await expect(terminal).toContainText("OK");
});
