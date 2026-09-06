// 设备信息页测试:mock @/lib/api 注入固定数据(先例 src/app/shell.test.tsx),覆盖:
// 定义列表渲染(mono+复制钮)、电话号码缺失词条、SIM 状态 chip、版本号读 meta[name="sa-version"]
// (含 __SA_VERSION__ 占位回退空)、pending 保留旧值 + chip、error → ErrorRetry、
// 以及 lib 纯函数(readSaVersion/simStatusView/isPlaceholderValue)。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen } from "@testing-library/react";

import "@/lib/i18n";
import DeviceinfoPage from "@/features/deviceinfo/page";
import { isPlaceholderValue, readSaVersion, simStatusView } from "@/features/deviceinfo/lib";
import { deviceInfoData } from "@/lib/api";

vi.mock("@/lib/api", () => ({
  deviceInfoData: vi.fn(),
}));

const GOOD = {
  manufacturer: "Quectel",
  modelName: "RG520N-CN",
  firmwareVersion: "RG520NCNAAR01A03M4GA_30.006.02",
  simStatus: "已插卡",
  simInserted: true,
  imsi: "460001234567890",
  iccid: "89860012345678901234",
  imei: "860000000000001",
  lanIp: "192.168.1.1",
  wwanIpv4: "10.0.0.2",
  wwanIpv6: "fe80::2",
  phoneNumber: "-",
};

function createClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderPage(client = createClient()) {
  const utils = render(
    <QueryClientProvider client={client}>
      <DeviceinfoPage />
    </QueryClientProvider>,
  );
  return { client, ...utils };
}

function appendVersionMeta(content: string): HTMLMetaElement {
  const meta = document.createElement("meta");
  meta.setAttribute("name", "sa-version");
  meta.content = content;
  document.head.append(meta);
  return meta;
}

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

beforeEach(() => {
  vi.mocked(deviceInfoData).mockResolvedValue(GOOD);
});

afterEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  document.querySelector('meta[name="sa-version"]')?.remove();
});

describe("deviceinfo 渲染", () => {
  test("定义列表上屏:标识字段 mono + 复制钮,SIM 状态 chip,调用口径 get+force:false", async () => {
    const { container } = renderPage();
    expect(await screen.findByText("Quectel")).toBeInTheDocument();
    expect(screen.getByText("RG520N-CN")).toBeInTheDocument();
    expect(screen.getByText("RG520NCNAAR01A03M4GA_30.006.02")).toBeInTheDocument();
    const imei = screen.getByText("860000000000001");
    expect(imei).toHaveClass("font-mono");
    expect(screen.getByText("460001234567890")).toBeInTheDocument();
    expect(screen.getByText("89860012345678901234")).toBeInTheDocument();
    expect(screen.getByText("192.168.1.1")).toBeInTheDocument();
    expect(screen.getByText("10.0.0.2")).toBeInTheDocument();
    expect(screen.getByText("fe80::2")).toBeInTheDocument();
    // SIM 状态 → StatusChip(success tone)
    expect(screen.getByText("已插卡")).toBeInTheDocument();
    // 电话号码缺失 → 无本机号码词条
    expect(screen.getByText("无本机号码")).toBeInTheDocument();
    // 可复制字段(型号/固件/IMEI/IMSI/ICCID/LAN/WANv4/WANv6)均带复制钮
    expect(screen.getAllByRole("button", { name: "复制" }).length).toBeGreaterThanOrEqual(8);
    expect(container.querySelector('[data-slot="deviceinfo-footer"]')).not.toBeNull();
    expect(vi.mocked(deviceInfoData)).toHaveBeenCalledWith("get", { force: false });
  });

  test("电话号码存在时直接显示,不出现无本机号码词条", async () => {
    vi.mocked(deviceInfoData).mockResolvedValue({ ...GOOD, phoneNumber: "106491234567" });
    renderPage();
    expect(await screen.findByText("106491234567")).toBeInTheDocument();
    expect(screen.queryByText("无本机号码")).toBeNull();
  });

  test("版本号读 meta[name=sa-version];无 meta 回退 -", async () => {
    appendVersionMeta("SimpleAdmin-Go-snjzb-1.23");
    renderPage();
    expect(await screen.findByText("SimpleAdmin-Go-snjzb-1.23")).toBeInTheDocument();
    expect(screen.getByText("代码库")).toBeInTheDocument();
  });

  test("无 meta 时页脚版本显示占位 -", async () => {
    const { container } = renderPage();
    expect(await screen.findByText("Quectel")).toBeInTheDocument();
    expect(container.querySelector('[data-slot="sa-version"]')?.textContent).toBe("-");
  });

  test("pending:true 保留旧值 + StatusChip(模块就绪中…)", async () => {
    const client = createClient();
    renderPage(client);
    expect(await screen.findByText("Quectel")).toBeInTheDocument();

    vi.mocked(deviceInfoData).mockResolvedValue({
      ...GOOD,
      pending: true,
      manufacturer: "-",
      modelName: "-",
      imei: "-",
    });
    await act(async () => {
      await client.invalidateQueries({ queryKey: ["deviceInfo"] });
    });

    expect(await screen.findByText("模块就绪中…")).toBeInTheDocument();
    // 旧值保留,pending 载荷的 "-" 不上屏
    expect(screen.getByText("Quectel")).toBeInTheDocument();
    expect(screen.getByText("860000000000001")).toBeInTheDocument();
  });

  test("data.error → ErrorRetry 失败横幅(retry 退避后落定)", async () => {
    vi.mocked(deviceInfoData).mockResolvedValue({ error: "AT 数据读取失败，请检查模块或稍后重试" });
    renderPage();
    // useDeviceInfoStream 显式 retry:2(≈旧版连续 3 次失败才亮横幅),退避约 3s
    expect(
      await screen.findByText("设备信息读取失败", undefined, { timeout: 8000 }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重试" })).toBeInTheDocument();
  });
});

describe("deviceinfo lib 纯函数", () => {
  test("readSaVersion:无 meta 回退空;__SA_VERSION__ 占位视同未注入", () => {
    expect(readSaVersion()).toBe("");
    const placeholder = appendVersionMeta("__SA_VERSION__");
    expect(readSaVersion()).toBe("");
    placeholder.content = "SimpleAdmin-Go-1.00";
    expect(readSaVersion()).toBe("SimpleAdmin-Go-1.00");
  });

  test("simStatusView:服务端固定中文枚举 → tone + 词条键", () => {
    expect(simStatusView("已插卡")).toEqual({ tone: "success", labelKey: "simInserted" });
    expect(simStatusView("未插卡")).toEqual({ tone: "danger", labelKey: "noSim" });
    expect(simStatusView("未知")).toEqual({ tone: "muted", labelKey: "simUnknown" });
    expect(simStatusView(undefined)).toEqual({ tone: "muted", labelKey: "simUnknown" });
  });

  test("isPlaceholderValue:空串/-/空白为占位", () => {
    expect(isPlaceholderValue("-")).toBe(true);
    expect(isPlaceholderValue("")).toBe(true);
    expect(isPlaceholderValue("  ")).toBe(true);
    expect(isPlaceholderValue(undefined)).toBe(true);
    expect(isPlaceholderValue("192.168.1.1")).toBe(false);
  });
});
