// 短信收件箱渲染 mock 短信。mock 数据已核实(go-build/.../mock_at_responses.go
// mockSMSList():经 AT+CMGL PDU 构造 2 条短信,WS 网关实测 /api/sms_data action=list):
//   1) 10001        「尊敬的用户:您本月的流量已使用80%,详情可登录中国电信APP查询。」
//   2) +10000000000 「SimpleAdmin Windows mock SMS」
import { test, expect } from "@playwright/test";

test("收件箱渲染 mock 短信", async ({ page }) => {
  await page.goto("/#/sms");
  // 初次加载即拉正文(smsData list force=1),mock AT 通道为串行队列,放宽超时
  await expect(page.getByText("10001", { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("+10000000000", { exact: true })).toBeVisible();
  await expect(page.getByText(/流量已使用80%/)).toBeVisible();
  await expect(page.getByText("SimpleAdmin Windows mock SMS")).toBeVisible();
});
