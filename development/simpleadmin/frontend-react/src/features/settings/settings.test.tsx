// settings 测试:纯函数(IMEI 15 位/TTL 严格 0-255/密码一致性矩阵/后端错误映射/关机探测序列)
// + 组件行为(重启触发 reboot store startCountdown、ok:false 取消倒计时、关机 requireWord、
// IMEI confirm+写入、TTL 前端拦截、密码成功 → 重登 confirm)。vi.mock @/lib/api 隔离网关。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import "@/lib/i18n";
import { ConfirmDialog } from "@/components/common";
import SettingsPage from "@/features/settings/page";
import {
  getTTLStatus,
  languageSet,
  logout,
  setPassword,
  setTTL,
  systemData,
  themeGet,
  themeSet,
} from "@/lib/api";
import { useConfirmStore } from "@/stores/confirm";
import { useRebootStore } from "@/stores/reboot";

import {
  POWER_OFF_PROBE,
  confirmPoweroffByProbe,
  isDisplayableImei,
  isValidImeiInput,
  passwordErrorInfo,
  validatePasswordForm,
  validateTtlInput,
  waitForDevicePoweredOff,
} from "./lib";

vi.mock("@/lib/api", () => ({
  systemData: vi.fn(),
  getTTLStatus: vi.fn(),
  setTTL: vi.fn(),
  setPassword: vi.fn(),
  logout: vi.fn(),
  themeGet: vi.fn(),
  themeSet: vi.fn(),
  languageSet: vi.fn(),
}));

const systemDataMock = vi.mocked(systemData);

beforeAll(() => {
  class ResizeObserverStub {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => {};
  Element.prototype.releasePointerCapture = () => {};
  Element.prototype.scrollIntoView = () => {};
});

const originalStartCountdown = useRebootStore.getState().startCountdown;
const originalCloseCountdown = useRebootStore.getState().closeCountdown;
let startCountdownSpy: ReturnType<typeof vi.fn>;
let closeCountdownSpy: ReturnType<typeof vi.fn>;

function renderPage(): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <SettingsPage />
      <ConfirmDialog />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  useConfirmStore.setState({ options: null, resolve: null });
  startCountdownSpy = vi.fn();
  closeCountdownSpy = vi.fn();
  useRebootStore.setState({
    countdownActive: false,
    startCountdown: startCountdownSpy,
    closeCountdown: closeCountdownSpy,
  });
  systemDataMock.mockImplementation((action: string) => {
    if (action === "status") return Promise.resolve({ imei: "860112345678901" });
    return Promise.resolve({ ok: true, response: "OK" });
  });
  vi.mocked(getTTLStatus).mockResolvedValue({ isEnabled: true, ttl: 64 });
  vi.mocked(setTTL).mockResolvedValue({ ok: true });
  vi.mocked(setPassword).mockResolvedValue({ ok: true });
  vi.mocked(logout).mockResolvedValue({ ok: true });
  vi.mocked(themeGet).mockResolvedValue({ theme: "dark" });
  vi.mocked(themeSet).mockResolvedValue({ theme: "dark" });
  vi.mocked(languageSet).mockResolvedValue({ language: "zh-CN" });
  // 关机探测:设备仍可达(fetch 成功);组件卸载时 cancelled 终止循环
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve({ ok: true })),
  );
});

afterEach(() => {
  useRebootStore.setState({
    startCountdown: originalStartCountdown,
    closeCountdown: originalCloseCountdown,
  });
  vi.unstubAllGlobals();
});

describe("校验纯函数", () => {
  test("IMEI:写入必须 15 位纯数字;展示放宽到 14-17 位", () => {
    expect(isValidImeiInput("490154203237518")).toBe(true);
    expect(isValidImeiInput(" 490154203237518 ")).toBe(true);
    expect(isValidImeiInput("49015420323751")).toBe(false);
    expect(isValidImeiInput("4901542032375182")).toBe(false);
    expect(isValidImeiInput("49015420323751a")).toBe(false);
    expect(isValidImeiInput("")).toBe(false);
    expect(isDisplayableImei("860112345678901")).toBe(true);
    expect(isDisplayableImei("86011234567890123")).toBe(true);
    expect(isDisplayableImei("-")).toBe(false);
    expect(isDisplayableImei("12345")).toBe(false);
  });

  test('TTL:严格 0-255,拒绝 parseInt 陷阱("0x10"/"12abc")', () => {
    expect(validateTtlInput("0")).toBe(0);
    expect(validateTtlInput("64")).toBe(64);
    expect(validateTtlInput("255")).toBe(255);
    expect(validateTtlInput(" 64 ")).toBe(64);
    expect(validateTtlInput("256")).toBeNull();
    expect(validateTtlInput("0x10")).toBeNull();
    expect(validateTtlInput("12abc")).toBeNull();
    expect(validateTtlInput("-1")).toBeNull();
    expect(validateTtlInput("")).toBeNull();
    expect(validateTtlInput("1234")).toBeNull();
  });

  test("密码矩阵:非空 → 一致性 → 换行 → 超长 → 强度(≥8)", () => {
    expect(validatePasswordForm({ current: "", next: "abcd1234", confirm: "abcd1234" })).toBe(
      "empty",
    );
    expect(validatePasswordForm({ current: "old", next: "", confirm: "" })).toBe("empty");
    expect(validatePasswordForm({ current: "old", next: "abcd1234", confirm: "abcd1235" })).toBe(
      "mismatch",
    );
    expect(
      validatePasswordForm({ current: "old", next: "abcd\n1234", confirm: "abcd\n1234" }),
    ).toBe("lineBreaks");
    expect(
      validatePasswordForm({ current: "old", next: "a".repeat(129), confirm: "a".repeat(129) }),
    ).toBe("tooLong");
    expect(validatePasswordForm({ current: "old", next: "abc12", confirm: "abc12" })).toBe(
      "tooShort",
    );
    expect(
      validatePasswordForm({ current: "old", next: "abcd1234", confirm: "abcd1234" }),
    ).toBeNull();
  });

  test("passwordErrorInfo:403 与后端 error 文案映射", () => {
    expect(passwordErrorInfo(403, '{"error":"current password incorrect"}').key).toBe(
      "currentPasswordIsIncorrect",
    );
    expect(passwordErrorInfo(400, '{"error":"new password is empty"}').key).toBe(
      "newPasswordCannotBeEmpty",
    );
    expect(
      passwordErrorInfo(400, '{"error":"new password must not contain line breaks"}').key,
    ).toBe("passwordNoLineBreaks");
    expect(passwordErrorInfo(400, '{"error":"new password is too long"}').key).toBe(
      "passwordTooLong",
    );
    expect(passwordErrorInfo(400, '{"error":"password confirmation mismatch"}').key).toBe(
      "passwordConfirmationMismatch",
    );
    expect(passwordErrorInfo(400, '{"error":"weird"}')).toEqual({ key: null, raw: "weird" });
    expect(passwordErrorInfo(500, undefined)).toEqual({ key: null, raw: "" });
    expect(passwordErrorInfo(400, "not-json")).toEqual({ key: null, raw: "" });
  });

  test("waitForDevicePoweredOff:轮询到不可达即 off;超时也 off;取消即 cancelled", async () => {
    const sleepFn = vi.fn(() => Promise.resolve());
    const probe = vi
      .fn<() => Promise<boolean>>()
      .mockResolvedValueOnce(true)
      .mockResolvedValueOnce(false);
    await expect(waitForDevicePoweredOff({ probe, sleepFn })).resolves.toBe("off");
    expect(probe).toHaveBeenCalledTimes(2);
    // 初始延迟 + 第一次轮询后的间隔(第二次探测发现不可达即返回,不再等待)
    expect(sleepFn).toHaveBeenCalledTimes(2);

    const deadProbe = vi.fn<() => Promise<boolean>>().mockResolvedValue(false);
    const deadSleep = vi.fn(() => Promise.resolve());
    await expect(waitForDevicePoweredOff({ probe: deadProbe, sleepFn: deadSleep })).resolves.toBe(
      "off",
    );
    expect(deadProbe).toHaveBeenCalledTimes(1);

    const aliveProbe = vi.fn<() => Promise<boolean>>().mockResolvedValue(true);
    await expect(
      waitForDevicePoweredOff({ probe: aliveProbe, sleepFn, maxAttempts: 3, intervalMs: 1 }),
    ).resolves.toBe("off");
    expect(aliveProbe).toHaveBeenCalledTimes(3);

    const cancelledProbe = vi.fn<() => Promise<boolean>>();
    await expect(
      waitForDevicePoweredOff({ probe: cancelledProbe, sleepFn, isCancelled: () => true }),
    ).resolves.toBe("cancelled");
    expect(cancelledProbe).not.toHaveBeenCalled();
  });

  test("confirmPoweroffByProbe:延时单探,仍可达=alive(未送达),不可达=off", async () => {
    const sleepFn = vi.fn(() => Promise.resolve());
    await expect(
      confirmPoweroffByProbe({ probe: () => Promise.resolve(true), sleepFn }),
    ).resolves.toBe("alive");
    expect(sleepFn).toHaveBeenCalledWith(POWER_OFF_PROBE.fallbackDelayMs);
    await expect(
      confirmPoweroffByProbe({ probe: () => Promise.resolve(false), sleepFn }),
    ).resolves.toBe("off");
  });
});

describe("SettingsPage 组件", () => {
  test("五卡渲染:当前 IMEI 与 TTL 徽章就位", async () => {
    renderPage();
    expect(await screen.findByText("设备操作")).toBeInTheDocument();
    expect(screen.getByText("IMEI 设置")).toBeInTheDocument();
    expect(screen.getByText("界面偏好")).toBeInTheDocument();
    expect(screen.getByText("账户安全")).toBeInTheDocument();
    expect(screen.getByText("TTL 设置")).toBeInTheDocument();
    expect(await screen.findByText("860112345678901")).toBeInTheDocument();
    expect(screen.getByText("TTL 已激活")).toBeInTheDocument();
    expect(screen.getByText("TTL 值: 64")).toBeInTheDocument();
    // 设备默认主题读取 themeGet(与顶栏本机切换区分)
    await waitFor(() => expect(themeGet).toHaveBeenCalled());
  });

  test("重启模块:confirm → startCountdown(40s) → systemData(reboot)", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "AT命令重启" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("AT命令重启");
    await user.click(within(dialog).getByRole("button", { name: "重启" }));
    await waitFor(() =>
      expect(startCountdownSpy).toHaveBeenCalledWith(
        expect.objectContaining({ seconds: 40, label: "重启中…请稍候,请勿关闭页面" }),
      ),
    );
    await waitFor(() => expect(systemDataMock).toHaveBeenCalledWith("reboot", undefined));
    expect(closeCountdownSpy).not.toHaveBeenCalled();
  });

  test("重启模块被拒绝(ok:false)→ 取消倒计时", async () => {
    const user = userEvent.setup();
    systemDataMock.mockImplementation((action: string) => {
      if (action === "status") return Promise.resolve({ imei: "860112345678901" });
      return Promise.resolve({ ok: false, response: "ERROR" });
    });
    renderPage();
    await user.click(await screen.findByRole("button", { name: "AT命令重启" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "重启" }));
    await waitFor(() => expect(closeCountdownSpy).toHaveBeenCalled());
  });

  test("重启设备:乐观倒计时 60s + 设备重启文案", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "设备重启" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("这将重启整台设备");
    await user.click(within(dialog).getByRole("button", { name: "重启设备" }));
    await waitFor(() =>
      expect(startCountdownSpy).toHaveBeenCalledWith(
        expect.objectContaining({ seconds: 60, label: "设备重启中…请稍候,请勿关闭页面" }),
      ),
    );
    await waitFor(() => expect(systemDataMock).toHaveBeenCalledWith("reboot_device", undefined));
  });

  test("关机:confirm requireWord「关机」,输入前禁用,确认后提交 poweroff_device", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "关机" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("这将使设备断电关机");
    const confirmButton = within(dialog).getByRole("button", { name: "关机" });
    expect(confirmButton).toBeDisabled();
    await user.type(within(dialog).getByPlaceholderText("关机"), "关机");
    expect(confirmButton).toBeEnabled();
    await user.click(confirmButton);
    await waitFor(() => expect(systemDataMock).toHaveBeenCalledWith("poweroff_device", undefined));
    // 关机同样接入 reboot store 遮罩(探测预算 20s)
    expect(startCountdownSpy).toHaveBeenCalledWith(
      expect.objectContaining({ seconds: POWER_OFF_PROBE.overlaySeconds }),
    );
  });

  test("IMEI:与当前相同被拦截;合法新值 → confirm → 倒计时 40s + set_imei", async () => {
    const user = userEvent.setup();
    renderPage();
    const input = await screen.findByLabelText("请输入新的 IMEI");
    // 页面上 IMEI 卡与 TTL 卡各有一个「更新」按钮,用所属 Panel 作用域区分
    const imeiPanel = input.closest('[data-slot="panel"]') as HTMLElement;
    const updateButton = within(imeiPanel).getByRole("button", { name: "更新" });
    // 预填当前 IMEI → 相同值拦截,不弹 confirm
    await waitFor(() => expect(input).toHaveValue("860112345678901"));
    await user.click(updateButton);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(systemDataMock).not.toHaveBeenCalledWith("set_imei", expect.anything());

    await user.clear(input);
    await user.type(input, "49015420323751");
    await user.click(updateButton);
    // 14 位非法 → 仍不弹 confirm
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    await user.clear(input);
    await user.type(input, "490154203237518");
    await user.click(updateButton);
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("这将修改 IMEI 并重启调制解调器。");
    await user.click(within(dialog).getByRole("button", { name: "确认并重启" }));
    await waitFor(() => expect(startCountdownSpy).toHaveBeenCalledWith({ seconds: 40 }));
    await waitFor(() =>
      expect(systemDataMock).toHaveBeenCalledWith("set_imei", { imei: "490154203237518" }),
    );
  });

  test('TTL:"0x10" 被前端拦截;合法值提交并回读', async () => {
    const user = userEvent.setup();
    renderPage();
    const input = await screen.findByLabelText("TTL 值");
    const ttlPanel = input.closest('[data-slot="panel"]') as HTMLElement;
    const updateButton = within(ttlPanel).getByRole("button", { name: "更新" });
    await user.type(input, "0x10");
    await user.click(updateButton);
    expect(setTTL).not.toHaveBeenCalled();

    await user.clear(input);
    await user.type(input, "65");
    await user.click(updateButton);
    await waitFor(() => expect(setTTL).toHaveBeenCalledWith(65));
    // 保存后回读 TTL 状态
    await waitFor(() => expect(getTTLStatus).toHaveBeenCalledTimes(2));
  });

  test("密码:强度/一致性前端拦截;成功后 confirm 提示重登,取消不退出", async () => {
    const user = userEvent.setup();
    renderPage();
    const current = await screen.findByPlaceholderText("请输入当前登录密码");
    const next = screen.getByPlaceholderText("请输入新的登录密码");
    const confirmInput = screen.getByPlaceholderText("请再次输入新的登录密码");

    await user.type(current, "oldpass");
    await user.type(next, "short1");
    await user.type(confirmInput, "short1");
    await user.click(screen.getByRole("button", { name: "修改密码" }));
    expect(setPassword).not.toHaveBeenCalled();

    await user.clear(next);
    await user.type(next, "abcd1234");
    await user.click(screen.getByRole("button", { name: "修改密码" }));
    // 两次输入不一致 → 拦截
    expect(setPassword).not.toHaveBeenCalled();

    await user.clear(confirmInput);
    await user.type(confirmInput, "abcd1234");
    await user.click(screen.getByRole("button", { name: "修改密码" }));
    await waitFor(() =>
      expect(setPassword).toHaveBeenCalledWith("oldpass", "abcd1234", "abcd1234"),
    );

    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("密码已保存，请使用新密码重新登录。");
    expect(within(dialog).getByRole("button", { name: "退出并重新登录" })).toBeInTheDocument();
    // 取消 → 不触发退出登录
    await user.click(within(dialog).getByRole("button", { name: "取消" }));
    await waitFor(() => expect(useConfirmStore.getState().options).toBeNull());
    expect(logout).not.toHaveBeenCalled();
  });
});
