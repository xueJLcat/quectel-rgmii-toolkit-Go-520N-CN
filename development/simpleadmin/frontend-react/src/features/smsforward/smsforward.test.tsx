// smsforward 页测试:纯函数矩阵(Webhook URL/请求头/JSON 模板校验、Server酱
// SendKey 识别)+ 组件测试(Webhook 卡扩展参数校验与保存载荷、Server酱 卡
// 校验与保存、测试推送按钮的脏态禁用与调用)。jsdom 无真实网络:
// vi.mock("@/lib/api") 隔离全部端点。
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import "@/lib/i18n";
import { Providers } from "@/app/providers";
import {
  smsForwardTest,
  smsServerChanGet,
  smsServerChanSet,
  smsWebhookGet,
  smsWebhookSet,
} from "@/lib/api";

import SmsForwardPage from "./page";
import {
  findInvalidHeaderLine,
  isServerChanKeyValid,
  isWebhookTemplateValid,
  isWebhookTimeoutValid,
  isWebhookUrlValid,
  normalizeWebhookMethod,
} from "./lib";

vi.mock("@/lib/api", () => ({
  smsWebhookGet: vi.fn(),
  smsWebhookSet: vi.fn(),
  smsServerChanGet: vi.fn(),
  smsServerChanSet: vi.fn(),
  smsForwardTest: vi.fn(),
}));

const webhookGetMock = vi.mocked(smsWebhookGet);
const webhookSetMock = vi.mocked(smsWebhookSet);
const serverChanGetMock = vi.mocked(smsServerChanGet);
const serverChanSetMock = vi.mocked(smsServerChanSet);
const forwardTestMock = vi.mocked(smsForwardTest);

describe("smsforward 纯函数", () => {
  test("isWebhookUrlValid:仅 http/https", () => {
    expect(isWebhookUrlValid("https://example.com/hook")).toBe(true);
    expect(isWebhookUrlValid("http://192.168.1.2:8080/hook")).toBe(true);
    expect(isWebhookUrlValid("ftp://bad")).toBe(false);
    expect(isWebhookUrlValid("example.com/hook")).toBe(false);
    expect(isWebhookUrlValid("  ")).toBe(false);
  });

  test("normalizeWebhookMethod:未知/空缺省 POST", () => {
    expect(normalizeWebhookMethod("get")).toBe("GET");
    expect(normalizeWebhookMethod("PUT")).toBe("PUT");
    expect(normalizeWebhookMethod("delete")).toBe("POST");
    expect(normalizeWebhookMethod(undefined)).toBe("POST");
  });

  test("isWebhookTimeoutValid:1-120 整数", () => {
    expect(isWebhookTimeoutValid("1")).toBe(true);
    expect(isWebhookTimeoutValid("120")).toBe(true);
    expect(isWebhookTimeoutValid("0")).toBe(false);
    expect(isWebhookTimeoutValid("121")).toBe(false);
    expect(isWebhookTimeoutValid("abc")).toBe(false);
    expect(isWebhookTimeoutValid("1.5")).toBe(false);
  });

  test("findInvalidHeaderLine:每行 Name: Value,空行忽略", () => {
    expect(findInvalidHeaderLine("")).toBeNull();
    expect(findInvalidHeaderLine("Authorization: Bearer tok\n\nX-Tag: cpe\n")).toBeNull();
    expect(findInvalidHeaderLine("badline")).toBe(1);
    expect(findInvalidHeaderLine("A: ok\nbad")).toBe(2);
    expect(findInvalidHeaderLine(": value")).toBe(1);
    expect(findInvalidHeaderLine("Bad Name: value")).toBe(1);
  });

  test("isWebhookTemplateValid:占位符样例替换后须为合法 JSON", () => {
    expect(isWebhookTemplateValid("")).toBe(true);
    expect(isWebhookTemplateValid("   ")).toBe(true);
    expect(isWebhookTemplateValid('{"m":"{text}","s":"{sender}"}')).toBe(true);
    expect(isWebhookTemplateValid('{"i":{index}}')).toBe(true);
    expect(isWebhookTemplateValid("{bad")).toBe(false);
    expect(isWebhookTemplateValid('{"a":"{text}"')).toBe(false);
  });

  test("isServerChanKeyValid:SCT/sctp 识别与非法字符拦截", () => {
    expect(isServerChanKeyValid("SCT123456Tabc")).toBe(true);
    expect(isServerChanKeyValid("  SCT123456Tabc  ")).toBe(true);
    expect(isServerChanKeyValid("sctp123tXXXX")).toBe(true);
    expect(isServerChanKeyValid("sctpABtXXXX")).toBe(false);
    expect(isServerChanKeyValid("sctp123")).toBe(false);
    expect(isServerChanKeyValid("")).toBe(false);
    expect(isServerChanKeyValid("has space")).toBe(false);
    expect(isServerChanKeyValid("SCT/evil")).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// 组件测试
// ---------------------------------------------------------------------------

function mockApis(): void {
  webhookGetMock.mockResolvedValue({
    enabled: false,
    url: "",
    method: "POST",
    headers: "",
    timeoutSec: 10,
    template: "",
    lastNotifiedIndex: -1,
  });
  webhookSetMock.mockResolvedValue({ ok: true });
  serverChanGetMock.mockResolvedValue({ enabled: false, sendKey: "" });
  serverChanSetMock.mockResolvedValue({ ok: true });
  forwardTestMock.mockResolvedValue({ ok: true });
}

function renderPage(): void {
  render(
    <Providers>
      <SmsForwardPage />
    </Providers>,
  );
}

/** 按卡片标题定位 Panel 根节点(标题文本与卡内 Label 重名,须走 heading)。 */
function panelByTitle(title: string): HTMLElement {
  const heading = screen.getByRole("heading", { name: title });
  const section = heading.closest("section");
  if (!section) throw new Error(`未找到卡片: ${title}`);
  return section;
}

beforeEach(() => {
  vi.clearAllMocks();
  mockApis();
});

describe("SmsForwardPage 组件", () => {
  test("Webhook 卡:URL 校验拦截非法地址,合法地址按扩展参数保存", async () => {
    renderPage();
    const panel = panelByTitle("新短信转发到 Webhook");
    const urlInput = await within(panel).findByPlaceholderText("https:// 开头的回调地址");
    // 配置回读前表单禁用:等待 loaded 后再交互
    await waitFor(() => expect(urlInput).toBeEnabled());
    fireEvent.change(urlInput, { target: { value: "ftp://bad" } });
    expect(
      within(panel).getByText("请输入 http:// 或 https:// 开头的地址"),
    ).toBeInTheDocument();
    const saveButton = within(panel).getByRole("button", { name: "保存" });
    expect(saveButton).toBeDisabled();
    fireEvent.change(urlInput, { target: { value: "https://example.com/hook" } });
    await waitFor(() => expect(saveButton).toBeEnabled());
    fireEvent.click(saveButton);
    await waitFor(() =>
      expect(webhookSetMock).toHaveBeenCalledWith({
        enabled: "0",
        url: "https://example.com/hook",
        method: "POST",
        headers: "",
        timeoutSec: "10",
        template: "",
      }),
    );
  });

  test("Webhook 卡:非法请求头/模板/超时显示行级错误并拦截保存", async () => {
    renderPage();
    const panel = panelByTitle("新短信转发到 Webhook");
    const headersInput = await within(panel).findByPlaceholderText(
      "每行一条，格式：Name: Value（可选）",
    );
    await waitFor(() => expect(headersInput).toBeEnabled());
    fireEvent.change(headersInput, { target: { value: "A: ok\nbadline" } });
    expect(
      within(panel).getByText("请求头第 2 行格式不正确，应为 Name: Value"),
    ).toBeInTheDocument();

    const templateInput = within(panel).getByPlaceholderText(
      /留空使用内置载荷/,
    );
    fireEvent.change(templateInput, { target: { value: "{bad" } });
    expect(
      within(panel).getByText("模板替换占位符后不是合法 JSON"),
    ).toBeInTheDocument();

    const timeoutInput = within(panel).getByLabelText("超时（秒）");
    fireEvent.change(timeoutInput, { target: { value: "999" } });
    expect(within(panel).getByText("超时须为 1-120 的整数")).toBeInTheDocument();

    expect(within(panel).getByRole("button", { name: "保存" })).toBeDisabled();
  });

  test("Webhook 卡:测试推送使用已保存配置,表单脏态时禁用", async () => {
    webhookGetMock.mockResolvedValue({
      enabled: true,
      url: "https://example.com/hook",
      method: "POST",
      headers: "",
      timeoutSec: 10,
      template: "",
      lastNotifiedIndex: 3,
    });
    renderPage();
    const panel = panelByTitle("新短信转发到 Webhook");
    await within(panel).findByText("最近已转发至索引 3");
    const testButton = within(panel).getByRole("button", { name: /发送测试/ });
    expect(testButton).toBeEnabled();
    fireEvent.click(testButton);
    await waitFor(() => expect(forwardTestMock).toHaveBeenCalledWith("webhook"));

    // 修改地址未保存 → 测试按钮禁用(避免测试口径与保存配置错位)
    const urlInput = within(panel).getByPlaceholderText("https:// 开头的回调地址");
    fireEvent.change(urlInput, { target: { value: "https://example.com/other" } });
    await waitFor(() => expect(testButton).toBeDisabled());
  });

  test("Server酱 卡:SendKey 校验与保存,启用未填 Key 拦截", async () => {
    renderPage();
    const panel = panelByTitle("新短信推送到 Server酱");
    const keyInput = await within(panel).findByPlaceholderText(
      "SCT…（Turbo）或 sctp…（Server酱³）开头",
    );
    await waitFor(() => expect(keyInput).toBeEnabled());
    fireEvent.change(keyInput, { target: { value: "bad key" } });
    expect(
      within(panel).getByText(/SendKey 不能包含空白字符/),
    ).toBeInTheDocument();
    const saveButton = within(panel).getByRole("button", { name: "保存" });
    expect(saveButton).toBeDisabled();

    fireEvent.change(keyInput, { target: { value: "sctp123tXXXX" } });
    await waitFor(() => expect(saveButton).toBeEnabled());
    fireEvent.click(saveButton);
    await waitFor(() =>
      expect(serverChanSetMock).toHaveBeenCalledWith({
        enabled: "0",
        sendKey: "sctp123tXXXX",
      }),
    );
  });

  test("Server酱 卡:启用开关在 SendKey 为空时拦截保存,测试按钮随保存态启用", async () => {
    renderPage();
    const panel = panelByTitle("新短信推送到 Server酱");
    await within(panel).findByPlaceholderText("SCT…（Turbo）或 sctp…（Server酱³）开头");
    const toggle = within(panel).getByRole("switch");
    await waitFor(() => expect(toggle).toBeEnabled());
    fireEvent.click(toggle);
    expect(
      within(panel).getByText("启用推送时必须填写 SendKey"),
    ).toBeInTheDocument();
    expect(within(panel).getByRole("button", { name: "保存" })).toBeDisabled();
    // 未保存 SendKey 时测试按钮禁用
    expect(within(panel).getByRole("button", { name: /发送测试/ })).toBeDisabled();
  });

  test("Server酱 卡:已保存合法 Key 后测试推送走 serverchan 通道", async () => {
    serverChanGetMock.mockResolvedValue({ enabled: true, sendKey: "SCT123Tabc" });
    renderPage();
    const panel = panelByTitle("新短信推送到 Server酱");
    await within(panel).findByDisplayValue("SCT123Tabc");
    const testButton = within(panel).getByRole("button", { name: /发送测试/ });
    expect(testButton).toBeEnabled();
    fireEvent.click(testButton);
    await waitFor(() => expect(forwardTestMock).toHaveBeenCalledWith("serverchan"));
  });
});
