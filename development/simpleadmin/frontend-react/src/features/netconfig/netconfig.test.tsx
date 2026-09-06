// 网络设置页单测:纯函数矩阵(IP 透传/USB 参数组装、DNS 行校验含行号、LAN IP 校验、MAC 校验、
// status 可信度、重启契约解析)+ 组件测(状态行渲染、禁用透传 → reboot store 倒计时 + WS 事件收尾
// toast + 卸载退订、开关切换 mutation 参数、LAN IP/MAC 绑定/DNS 上游保存参数与前端拦错)。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { toast } from "sonner";

import "@/lib/i18n";
import { networkConfigData } from "@/lib/api";
import type { NetworkConfigResponse } from "@/lib/api";
import { useConfirmStore } from "@/stores/confirm";

import { isNetconfigPendingError } from "./hooks";
import {
  DNS_UPSTREAM_LIMIT,
  MAC_BIND_LIMIT,
  NetconfigPendingError,
  ensureOk,
  ipPassthroughDisableParams,
  ipPassthroughEnableParams,
  isUntrustedStatus,
  normalizeMac,
  parseDnsLines,
  readRebootNotice,
  usbNetModeText,
  usbNetParams,
  validIPv4,
  validIPv6,
  validMAC,
  validateLanIpForm,
  validateMacBind,
} from "./lib";
import NetconfigPage from "./page";

const mocks = vi.hoisted(() => ({
  startCountdown: vi.fn(),
  closeCountdown: vi.fn(),
  onServerEvent: vi.fn(),
  unsubscribe: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  networkConfigData: vi.fn(),
  gateway: { onServerEvent: mocks.onServerEvent },
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock("@/stores/reboot", async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  const state = {
    countdownActive: false,
    endsAt: 0,
    total: actual.REBOOT_DEFAULT_SECONDS,
    label: null,
    startCountdown: mocks.startCountdown,
    closeCountdown: mocks.closeCountdown,
  };
  return {
    ...actual,
    useRebootStore: (selector: (value: typeof state) => unknown) => selector(state),
  };
});

const networkConfigDataMock = vi.mocked(networkConfigData);

// Radix 在 jsdom 缺失的指针/滚动 API 桩(与 common.test.tsx 相同)
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

const STATUS_DATA: NetworkConfigResponse = {
  ipPassStatus: true,
  DNSV4ProxyStatus: false,
  DNSV6ProxyStatus: false,
  dnsV4QueryFailed: true,
  currentUsbNetMode: "ECM",
  dmzMode: "0",
  dmzIP: "",
  lanIpStart: "100",
  lanIpEnd: "200",
  lanGwIp: "192.168.5.1",
};

const MAC_BIND_DATA = {
  ok: true,
  atReady: true,
  bindings: [{ index: 1, mac: "AA:BB:CC:DD:EE:FF", ip: "192.168.5.20" }],
};

const DNS_UPSTREAM_DATA = { ok: true, enabled: false, servers: [] as string[] };

const REBOOT_NOTICE: NetworkConfigResponse = {
  ok: true,
  response: "后台将开始禁用 IP 透传并重启设备。",
  reboot: true,
  rebooting: true,
  rebootAfterSeconds: 1,
  rebootCountdownSeconds: 40,
  message: "设备即将重启，请等待前端倒计时。",
};

type ServerEventHandler = (message: { data: Record<string, string> }) => void;

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity, staleTime: 0 } },
  });
}

interface ApiMockOptions {
  status?: NetworkConfigResponse;
  macBindList?: NetworkConfigResponse;
  dnsUpstream?: NetworkConfigResponse;
  ipPassthroughDisable?: NetworkConfigResponse;
}

function installApiMock(options: ApiMockOptions = {}) {
  networkConfigDataMock.mockImplementation(
    async (action: string, params?: Record<string, unknown>) => {
      switch (action) {
        case "status":
          return options.status ?? STATUS_DATA;
        case "mac_bind_list":
          return options.macBindList ?? MAC_BIND_DATA;
        case "dns_upstream":
          return options.dnsUpstream ?? DNS_UPSTREAM_DATA;
        case "ip_passthrough":
          return params?.enabled === "0"
            ? (options.ipPassthroughDisable ?? REBOOT_NOTICE)
            : { ok: true };
        default:
          return { ok: true };
      }
    },
  );
}

function renderPage() {
  const client = makeClient();
  const utils = render(
    <QueryClientProvider client={client}>
      <NetconfigPage />
    </QueryClientProvider>,
  );
  return { ...utils, client };
}

async function settleConfirm(confirmed: boolean) {
  await waitFor(() => expect(useConfirmStore.getState().options).not.toBeNull());
  await act(async () => {
    useConfirmStore.getState().settle(confirmed);
  });
}

/** 按面板标题取 Panel 根节点(消歧同名按钮,如 DNS 行"添加"与静态绑定"添加")。 */
function panelOf(title: string): HTMLElement {
  const heading = screen.getByRole("heading", { name: title });
  const panel = heading.closest("section");
  expect(panel).not.toBeNull();
  return panel as HTMLElement;
}

function capturedEventHandler(): ServerEventHandler {
  const call = mocks.onServerEvent.mock.calls.find((entry) => entry[0] === "ip_passthrough_result");
  expect(call).toBeTruthy();
  return call?.[1] as ServerEventHandler;
}

function statusCalls(): number {
  return networkConfigDataMock.mock.calls.filter((call) => call[0] === "status").length;
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  useConfirmStore.setState({ options: null, resolve: null });
  mocks.onServerEvent.mockReturnValue(mocks.unsubscribe);
  installApiMock();
});

describe("netconfig/lib 纯函数", () => {
  test("validIPv4 矩阵", () => {
    expect(validIPv4("192.168.5.1")).toBe(true);
    expect(validIPv4("0.0.0.0")).toBe(true);
    expect(validIPv4("255.255.255.255")).toBe(true);
    expect(validIPv4("256.1.1.1")).toBe(false);
    expect(validIPv4("192.168.5")).toBe(false);
    expect(validIPv4("192.168.5.1.2")).toBe(false);
    expect(validIPv4("a.b.c.d")).toBe(false);
    expect(validIPv4("")).toBe(false);
  });

  test("validIPv6 矩阵(对齐旧版:hostname/IPv4 拒绝,压缩规则)", () => {
    expect(validIPv6("2400:3200::1")).toBe(true);
    expect(validIPv6("::1")).toBe(true);
    expect(validIPv6("2001:db8:0:0:0:0:0:1")).toBe(true);
    expect(validIPv6("fe80::1::2")).toBe(false); // 两个 "::"
    expect(validIPv6("1:2:3:4:5:6:7")).toBe(false); // 无压缩必须 8 组
    expect(validIPv6("1:2:3:4:5:6:7:8:9")).toBe(false);
    expect(validIPv6("12345::1")).toBe(false); // 组超长
    expect(validIPv6(":::")).toBe(false);
    expect(validIPv6("dns.example.com")).toBe(false);
    expect(validIPv6("192.168.5.1")).toBe(false);
  });

  test("normalizeMac/validMAC:冒号/连字符/紧凑式归一为大写冒号,非法 → null", () => {
    expect(normalizeMac("aa:bb:cc:dd:ee:ff")).toBe("AA:BB:CC:DD:EE:FF");
    expect(normalizeMac("AA-BB-CC-DD-EE-FF")).toBe("AA:BB:CC:DD:EE:FF");
    expect(normalizeMac("aabbccddeeff")).toBe("AA:BB:CC:DD:EE:FF");
    expect(normalizeMac(" aabbccddeeff ")).toBe("AA:BB:CC:DD:EE:FF");
    expect(normalizeMac("aabb.ccdd.eeff")).toBeNull();
    expect(normalizeMac("GG:BB:CC:DD:EE:FF")).toBeNull();
    expect(normalizeMac("AA:BB:CC:DD:EE")).toBeNull();
    expect(normalizeMac("")).toBeNull();
    expect(validMAC("aabbccddeeff")).toBe(true);
    expect(validMAC("nope")).toBe(false);
  });

  test("IP 透传参数组装:ETH/USB 大写归一,非法/未指定 → null;禁用无 mode", () => {
    expect(ipPassthroughEnableParams("ETH")).toEqual({ enabled: "1", mode: "ETH" });
    expect(ipPassthroughEnableParams("eth")).toEqual({ enabled: "1", mode: "ETH" });
    expect(ipPassthroughEnableParams("usb")).toEqual({ enabled: "1", mode: "USB" });
    expect(ipPassthroughEnableParams("")).toBeNull();
    expect(ipPassthroughEnableParams("WAN")).toBeNull();
    expect(ipPassthroughDisableParams()).toEqual({ enabled: "0" });
  });

  test("usbnet 参数组装与展示文本", () => {
    expect(usbNetParams("rmnet")).toEqual({ mode: "RMNET" });
    expect(usbNetParams("ECM")).toEqual({ mode: "ECM" });
    expect(usbNetParams("MBIM")).toEqual({ mode: "MBIM" });
    expect(usbNetParams("RNDIS")).toEqual({ mode: "RNDIS" });
    expect(usbNetParams("XXX")).toBeNull();
    expect(usbNetParams("")).toBeNull();
    expect(usbNetModeText("ECM", "未知")).toBe("ECM");
    expect(usbNetModeText("未知", "Unknown")).toBe("Unknown");
    expect(usbNetModeText(undefined, "Unknown")).toBe("Unknown");
  });

  test("parseDnsLines 矩阵:合法 IPv4/IPv6、hostname 非法、非法行号(非空行计数)、去空去重", () => {
    expect(parseDnsLines(["223.5.5.5", "1.1.1.1"])).toEqual({
      servers: ["223.5.5.5", "1.1.1.1"],
      invalidLine: null,
    });
    expect(parseDnsLines(["2400:3200::1", "::1"])).toEqual({
      servers: ["2400:3200::1", "::1"],
      invalidLine: null,
    });
    // hostname 后端 net.ParseIP 会 400 拒绝,前端同口径拦截(对齐旧版)
    expect(parseDnsLines(["dns.example.com"])).toEqual({ servers: [], invalidLine: 1 });
    expect(parseDnsLines(["223.5.5.5", "bad-server", "1.1.1.1"])).toEqual({
      servers: ["223.5.5.5"],
      invalidLine: 2,
    });
    // 空行不计入行号
    expect(parseDnsLines(["", "  223.5.5.5  ", "", "oops"])).toEqual({
      servers: ["223.5.5.5"],
      invalidLine: 2,
    });
    // 大小写不敏感去重(兼容 IPv6 混写)
    expect(parseDnsLines(["FE80::1", "fe80::1", "223.5.5.5", "223.5.5.5"])).toEqual({
      servers: ["FE80::1", "223.5.5.5"],
      invalidLine: null,
    });
    expect(parseDnsLines([])).toEqual({ servers: [], invalidLine: null });
    expect(DNS_UPSTREAM_LIMIT).toBe(4);
  });

  test("validateLanIpForm 矩阵:合法拼接/缺字段/非法网关/起止范围/顺序/网关末段/池含网关", () => {
    expect(validateLanIpForm({ gateway: "192.168.5.1", start: "100", end: "200" })).toEqual({
      valid: true,
      startIp: "192.168.5.100",
      endIp: "192.168.5.200",
      gateway: "192.168.5.1",
    });
    // " 100"/"1e2" 等输入用校验后的数值拼接(对齐旧版注释语义)
    expect(validateLanIpForm({ gateway: " 10.0.0.1 ", start: " 100 ", end: "1e2" })).toEqual({
      valid: true,
      startIp: "10.0.0.100",
      endIp: "10.0.0.100",
      gateway: "10.0.0.1",
    });
    const missing = validateLanIpForm({ gateway: "", start: "100", end: "200" });
    expect(missing.valid).toBe(false);
    expect(!missing.valid && missing.errorKey).toBe(
      "enterAValidGatewayIpAddressStartAddressAndEndAddress",
    );
    const invalidGw = validateLanIpForm({ gateway: "192.168.5", start: "100", end: "200" });
    expect(invalidGw.valid).toBe(false);
    expect(!invalidGw.valid && invalidGw.errorKey).toBe("invalidGatewayIpAddressFormat");
    const outOfRange = validateLanIpForm({ gateway: "192.168.5.1", start: "255", end: "200" });
    expect(!outOfRange.valid && outOfRange.errorKey).toBe(
      "theStartEndAddressMustBeAnIntegerBetween1And254",
    );
    const notInteger = validateLanIpForm({ gateway: "192.168.5.1", start: "1.5", end: "200" });
    expect(!notInteger.valid && notInteger.errorKey).toBe(
      "theStartEndAddressMustBeAnIntegerBetween1And254",
    );
    const order = validateLanIpForm({ gateway: "192.168.5.1", start: "200", end: "100" });
    expect(!order.valid && order.errorKey).toBe("startAddressCannotBeGreaterThanEndAddress");
    const gwZero = validateLanIpForm({ gateway: "192.168.5.0", start: "100", end: "200" });
    expect(!gwZero.valid && gwZero.errorKey).toBe("lastOctetOfGatewayIpMustBe1254");
    const gwInPool = validateLanIpForm({ gateway: "192.168.5.150", start: "100", end: "200" });
    expect(!gwInPool.valid && gwInPool.errorKey).toBe("dhcpPoolMustNotIncludeGateway");
    // 边界:网关恰在池外紧邻 → 合法
    expect(validateLanIpForm({ gateway: "192.168.5.99", start: "100", end: "200" }).valid).toBe(
      true,
    );
  });

  test("validateMacBind:格式/查重(MAC 大小写不敏感、IP 精确)/上限 10", () => {
    const existing = [{ mac: "AA:BB:CC:DD:EE:FF", ip: "192.168.5.20" }];
    const badMac = validateMacBind("zzz", "192.168.5.21", existing);
    expect(!badMac.valid && badMac.errorKey).toBe("invalidMacAddressFormat");
    const badIp = validateMacBind("11:22:33:44:55:66", "192.168.5", existing);
    expect(!badIp.valid && badIp.errorKey).toBe("invalidIpAddressFormat");
    const dupMac = validateMacBind("aa-bb-cc-dd-ee-ff", "192.168.5.21", existing);
    expect(!dupMac.valid && dupMac.errorKey).toBe("thisMacAddressIsAlreadyBound");
    const dupIp = validateMacBind("11:22:33:44:55:66", "192.168.5.20", existing);
    expect(!dupIp.valid && dupIp.errorKey).toBe("thisIpAddressIsAlreadyBound");
    const full = Array.from({ length: MAC_BIND_LIMIT }, (_, i) => ({
      mac: `AA:BB:CC:DD:EE:${String(i).padStart(2, "0")}`,
      ip: `192.168.5.${100 + i}`,
    }));
    const overflow = validateMacBind("11:22:33:44:55:66", "192.168.5.21", full);
    expect(!overflow.valid && overflow.errorKey).toBe("youCanAddUpTo10StaticBindings");
    const ok = validateMacBind(" 11:22:33:44:55:66 ", " 192.168.5.21 ", existing);
    expect(ok).toEqual({ valid: true, mac: "11:22:33:44:55:66", ip: "192.168.5.21" });
  });

  test("isUntrustedStatus:全默认值不可信;任一字段有实值即信任;dnsV4QueryFailed 时 V4=false 是预期", () => {
    expect(
      isUntrustedStatus({
        ipPassStatus: false,
        DNSV4ProxyStatus: false,
        DNSV6ProxyStatus: false,
        dnsV4QueryFailed: true,
        currentUsbNetMode: "未知",
        dmzIP: "",
        lanGwIp: "",
      }),
    ).toBe(true);
    expect(isUntrustedStatus(STATUS_DATA)).toBe(false); // ipPassStatus true
    expect(
      isUntrustedStatus({
        ipPassStatus: false,
        DNSV4ProxyStatus: false,
        DNSV6ProxyStatus: false,
        dnsV4QueryFailed: true,
        currentUsbNetMode: "ECM",
        dmzIP: "",
        lanGwIp: "",
      }),
    ).toBe(false); // usbnet 有实值
    expect(
      isUntrustedStatus({
        ipPassStatus: false,
        DNSV4ProxyStatus: true,
        DNSV6ProxyStatus: false,
        dnsV4QueryFailed: false,
        currentUsbNetMode: "未知",
        dmzIP: "",
        lanGwIp: "",
      }),
    ).toBe(false); // V4 可查询且已启用
  });

  test("readRebootNotice:reboot/rebooting 契约解析,秒数非法回退 40", () => {
    expect(readRebootNotice(undefined)).toBeNull();
    expect(readRebootNotice({ ok: true })).toBeNull();
    expect(readRebootNotice(REBOOT_NOTICE)).toEqual({ countdownSeconds: 40 });
    expect(readRebootNotice({ reboot: true, rebootCountdownSeconds: 30 })).toEqual({
      countdownSeconds: 30,
    });
    expect(readRebootNotice({ rebooting: true })).toEqual({ countdownSeconds: 40 });
    expect(readRebootNotice({ reboot: true, rebootCountdownSeconds: 0 })).toEqual({
      countdownSeconds: 40,
    });
  });

  test("ensureOk:200+{ok:false,error} → 抛 error;缺响应 → 兜底文案", () => {
    expect(ensureOk({ ok: true }, "fb")).toEqual({ ok: true });
    expect(() => ensureOk({ ok: false, error: "boom" }, "fb")).toThrow("boom");
    expect(() => ensureOk({ ok: false }, "fb")).toThrow("fb");
    expect(() => ensureOk(undefined, "fb")).toThrow("fb");
  });

  test("NetconfigPendingError 判别", () => {
    expect(isNetconfigPendingError(new NetconfigPendingError())).toBe(true);
    expect(isNetconfigPendingError(new Error("x"))).toBe(false);
  });
});

describe("NetconfigPage 状态行渲染", () => {
  test("mock 数据上屏:六枚状态 chip + 静态绑定表 + DNS V4 系统托管只读说明", async () => {
    renderPage();
    expect(await screen.findByText("IP 透传: 已启用")).toBeInTheDocument();
    expect(screen.getByText("USB 协议: ECM")).toBeInTheDocument();
    expect(screen.getByText("DNS V4: 系统托管")).toBeInTheDocument();
    expect(screen.getByText("DNS V6: 未启用")).toBeInTheDocument();
    expect(screen.getByText("DMZ: 未启用")).toBeInTheDocument();
    expect(screen.getByText("LAN IP 段: 192.168.5.1 (100 - 200)")).toBeInTheDocument();
    // DNS V4 系统托管 → 只读 chip + 说明脚注,无 V4 开关;V6 开关存在
    expect(
      screen.getByText("DNS V4 由系统内部管理，始终生效，不支持查询或修改。"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("switch", { name: "DNS V4 代理开关" })).not.toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "DNS V6 代理开关" })).toBeInTheDocument();
    // 静态绑定表 + 上游 DNS 编辑器 + LAN IP 表单填充服务端值
    expect(screen.getByText("AA:BB:CC:DD:EE:FF")).toBeInTheDocument();
    expect(screen.getByLabelText("上游 DNS 服务器 1")).toHaveValue("");
    expect(screen.getByLabelText("网关 IP 地址")).toHaveValue("192.168.5.1");
    expect(screen.getByLabelText("起始地址")).toHaveValue("100");
  });
});

describe("IP 透传", () => {
  test("启用未选模式 → toast 提示,不下发", async () => {
    installApiMock({ status: { ...STATUS_DATA, ipPassStatus: false } });
    renderPage();
    // 等 status 就绪(控件解除禁用)后再点击
    await screen.findByText("IP 透传: 未启用");
    fireEvent.click(screen.getByRole("button", { name: "启用" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("未指定 IP 透传模式"));
    expect(networkConfigDataMock.mock.calls.some((call) => call[0] === "ip_passthrough")).toBe(
      false,
    );
  });

  test("禁用:danger 确认 → 立即倒计时 + 响应契约重置 + info toast", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "禁用" }));
    // 确认框挂起:标题为"禁用 IP 透传",danger 变体
    await waitFor(() => {
      const options = useConfirmStore.getState().options;
      expect(options?.title).toBe("禁用 IP 透传");
      expect(options?.danger).toBe(true);
    });
    expect(mocks.startCountdown).not.toHaveBeenCalled();
    await settleConfirm(true);
    // 立即启动默认 40s(不依赖响应到达)+ 响应 reboot 契约重置倒计时
    await waitFor(() => expect(mocks.startCountdown).toHaveBeenCalledTimes(2));
    expect(mocks.startCountdown).toHaveBeenNthCalledWith(1, { seconds: 40 });
    expect(mocks.startCountdown).toHaveBeenNthCalledWith(2, { seconds: 40 });
    expect(toast.info).toHaveBeenCalledWith("正在禁用 IP 透传，网口会重启，请等待倒计时结束。");
    expect(networkConfigDataMock).toHaveBeenCalledWith("ip_passthrough", { enabled: "0" });
  });

  test("禁用:取消确认不下发、不启动倒计时", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "禁用" }));
    await settleConfirm(false);
    expect(mocks.startCountdown).not.toHaveBeenCalled();
    expect(networkConfigDataMock.mock.calls.some((call) => call[0] === "ip_passthrough")).toBe(
      false,
    );
  });

  test("ip_passthrough_result 事件收尾:ok=false → 失败 toast + 取消倒计时;ok=true → 成功 toast;卸载退订", async () => {
    const { unmount } = renderPage();
    await screen.findByText("IP 透传: 已启用");
    expect(mocks.onServerEvent).toHaveBeenCalledWith("ip_passthrough_result", expect.any(Function));
    const handler = capturedEventHandler();

    act(() => {
      handler({ data: { ok: "false" } });
    });
    expect(toast.error).toHaveBeenCalledWith("操作失败，已取消后续步骤");
    expect(mocks.closeCountdown).toHaveBeenCalledTimes(1);

    act(() => {
      handler({ data: { ok: "true" } });
    });
    expect(toast.success).toHaveBeenCalledWith("IP 透传已禁用，设备正在重启");

    unmount();
    expect(mocks.unsubscribe).toHaveBeenCalled();
  });

  test("禁用响应 ok:false → 失败 toast + 取消倒计时(兜底,不等事件)", async () => {
    installApiMock({ ipPassthroughDisable: { ok: false, error: "模块拒绝执行" } });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "禁用" }));
    await settleConfirm(true);
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("模块拒绝执行"));
    expect(mocks.closeCountdown).toHaveBeenCalled();
  });
});

describe("DNS 代理开关", () => {
  test("V6 开关切换 → dns_proxy 参数 + 成功 toast + status 回读", async () => {
    renderPage();
    const toggle = await screen.findByRole("switch", { name: "DNS V6 代理开关" });
    // 等 status 就绪:开关解除禁用后才响应切换
    await screen.findByText("DNS V6: 未启用");
    const before = statusCalls();
    fireEvent.click(toggle);
    await waitFor(() =>
      expect(networkConfigDataMock).toHaveBeenCalledWith("dns_proxy", {
        family: "6",
        enabled: "1",
      }),
    );
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("DNS 代理设置已下发"));
    await waitFor(() => expect(statusCalls()).toBeGreaterThan(before));
  });

  test("开关 checked 绑定权威 status 值(下发期间不显示虚假状态)", async () => {
    renderPage();
    const toggle = await screen.findByRole("switch", { name: "DNS V6 代理开关" });
    expect(toggle).toHaveAttribute("data-state", "unchecked");
  });
});

describe("上游 DNS", () => {
  test("非法行 → common:dnsLineInvalid 行号 toast,不弹确认不下发", async () => {
    renderPage();
    const line1 = await screen.findByLabelText("上游 DNS 服务器 1");
    await screen.findByText("跟随运营商"); // dns_upstream 已加载,控件解除禁用
    fireEvent.change(line1, { target: { value: "dns.example.com" } });
    fireEvent.click(screen.getByRole("button", { name: /启用自定义/ }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("第 1 行 DNS 服务器格式无效"));
    expect(useConfirmStore.getState().options).toBeNull();
    expect(networkConfigDataMock.mock.calls.some((call) => call[0] === "dns_upstream_set")).toBe(
      false,
    );
  });

  test("空列表 → 至少填写一个;合法 IPv4+IPv6 → confirm → dns_upstream_set 参数", async () => {
    renderPage();
    const line1 = await screen.findByLabelText("上游 DNS 服务器 1");
    await screen.findByText("跟随运营商");
    fireEvent.click(screen.getByRole("button", { name: /启用自定义/ }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("请至少填写一个 DNS 服务器"));

    fireEvent.change(line1, { target: { value: "223.5.5.5" } });
    fireEvent.click(within(panelOf("上游 DNS")).getByRole("button", { name: "添加" }));
    const line2 = screen.getByLabelText("上游 DNS 服务器 2");
    fireEvent.change(line2, { target: { value: "2400:3200::1" } });
    fireEvent.click(screen.getByRole("button", { name: /启用自定义/ }));
    await settleConfirm(true);
    await waitFor(() =>
      expect(networkConfigDataMock).toHaveBeenCalledWith("dns_upstream_set", {
        enabled: "1",
        servers: "223.5.5.5,2400:3200::1",
      }),
    );
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("自定义上游 DNS 已保存"));
  });
});

describe("LAN IP", () => {
  test("校验失败 → toast 拦错不下发;合法 → lanip 参数(网关前三段拼接)", async () => {
    renderPage();
    const gateway = await screen.findByLabelText("网关 IP 地址");
    await screen.findByText("LAN IP 段: 192.168.5.1 (100 - 200)"); // status 就绪,表单已填充
    fireEvent.change(gateway, { target: { value: "192.168.5.150" } });
    fireEvent.click(screen.getByRole("button", { name: /^保存$/ }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("DHCP 地址池不能包含网关地址"));
    expect(networkConfigDataMock.mock.calls.some((call) => call[0] === "lanip")).toBe(false);

    fireEvent.change(gateway, { target: { value: "192.168.5.1" } });
    fireEvent.click(screen.getByRole("button", { name: /^保存$/ }));
    await waitFor(() =>
      expect(networkConfigDataMock).toHaveBeenCalledWith("lanip", {
        start: "192.168.5.100",
        end: "192.168.5.200",
        gateway: "192.168.5.1",
      }),
    );
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("已保存"));
  });
});

describe("DHCP 静态绑定", () => {
  test("非法 MAC → toast 拦错;合法 → mac_bind_set 参数 + 成功清空输入", async () => {
    renderPage();
    const macInput = await screen.findByLabelText("MAC 地址");
    const ipInput = screen.getByLabelText("IP 地址");
    await screen.findByText("AA:BB:CC:DD:EE:FF"); // 绑定列表已加载
    const bindPanel = panelOf("静态地址绑定");
    fireEvent.change(macInput, { target: { value: "zzz" } });
    fireEvent.change(ipInput, { target: { value: "192.168.5.21" } });
    fireEvent.click(within(bindPanel).getByRole("button", { name: "添加" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("MAC 地址格式无效"));

    fireEvent.change(macInput, { target: { value: "11-22-33-44-55-66" } });
    fireEvent.click(within(bindPanel).getByRole("button", { name: "添加" }));
    await waitFor(() =>
      expect(networkConfigDataMock).toHaveBeenCalledWith("mac_bind_set", {
        mac: "11-22-33-44-55-66",
        ip: "192.168.5.21",
      }),
    );
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("已添加静态绑定"));
    await waitFor(() => expect(macInput).toHaveValue(""));
  });

  test("删除 → danger 确认 → mac_bind_del 参数 + 成功 toast", async () => {
    renderPage();
    await screen.findByText("AA:BB:CC:DD:EE:FF");
    fireEvent.click(within(panelOf("静态地址绑定")).getByRole("button", { name: "删除" }));
    await waitFor(() => {
      const options = useConfirmStore.getState().options;
      expect(options?.title).toBe("删除静态绑定");
      expect(options?.danger).toBe(true);
    });
    await settleConfirm(true);
    await waitFor(() =>
      expect(networkConfigDataMock).toHaveBeenCalledWith("mac_bind_del", {
        mac: "AA:BB:CC:DD:EE:FF",
      }),
    );
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("已删除静态绑定"));
  });

  test("AT 未就绪(ok:false)→ 失败重试入口,不渲染假空列表", async () => {
    installApiMock({
      macBindList: { ok: false, error: "AT 通道忙,请稍后重试", bindings: [], atReady: false },
    });
    renderPage();
    expect(await screen.findByText("AT 通道忙,请稍后重试")).toBeInTheDocument();
    expect(screen.queryByText("未配置静态绑定")).not.toBeInTheDocument();
  });
});

describe("USB 网卡模式", () => {
  test("未选模式点更改 → toast 拦错,不弹确认不下发", async () => {
    renderPage();
    await screen.findByText("USB 协议: ECM"); // status 就绪,控件解除禁用
    fireEvent.click(screen.getByRole("button", { name: "更改" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("未指定 USB 网络模式"));
    expect(useConfirmStore.getState().options).toBeNull();
    expect(networkConfigDataMock.mock.calls.some((call) => call[0] === "usbnet")).toBe(false);
  });
});
