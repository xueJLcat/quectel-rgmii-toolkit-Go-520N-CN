// 总览页测试:mock @/lib/api 注入固定数据(先例 src/app/shell.test.tsx),覆盖三页渲染契约之 dashboard:
// mock 数据上屏、精简/完整切换(旧版 localStorage 键)、刷新频率变更持久化、pending 保留旧值 + chip、
// at_cache_updated 静默刷新,以及 lib 纯函数矩阵(旧版 index.js 语义等价)。
// jsdom 无 canvas:GaugeChart/TrendChart 走 charts 内建 data-chart-fallback 降级。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import "@/lib/i18n";
import DashboardPage from "@/features/dashboard/page";
import {
  clampRefreshRate,
  describeActiveSim,
  describeAssessment,
  formatUptimeParts,
  historyLatestRxRate,
  historyLatestSignal,
  humanBytes,
  humanBytesPerSec,
  readStoredRefreshRate,
  resolveTraffic,
  visibleSignalRats,
} from "@/features/dashboard/lib";
import { dashboardData, gateway, historyData } from "@/lib/api";
import type { ServerEventHandler } from "@/lib/api";

vi.mock("@/lib/api", () => ({
  gateway: {
    onServerEvent: vi.fn(() => () => {}),
  },
  dashboardData: vi.fn(),
  historyData: vi.fn(),
}));

const GOOD = {
  sim: "已激活",
  active_sim: "卡2",
  network_provider: "中国移动",
  network_mode: "NR5G",
  mccmnc: "46000",
  apn: "cmnet",
  bands: "n78",
  bandwidth: "100MHz",
  pcc_pci: "301",
  scc_pci: "-",
  ipv4: "10.0.0.1",
  ipv6: "fe80::1",
  cellID: "123456",
  eNBID: "654",
  earfcns: "627264",
  tac: "1",
  prxqrsrp: "-85",
  drxqrsrp: "-86",
  rx2qrsrp: "-87",
  rx3qrsrp: "-",
  rsrqLTE: "-11",
  rsrqLTEPercentage: 70,
  rsrqNR: "-12",
  rsrqNRPercentage: 65,
  rsrpLTE: "-95",
  rsrpLTEPercentage: 80,
  rsrpNR: "-100",
  rsrpNRPercentage: 75,
  sinrLTE: "20",
  sinrLTEPercentage: 90,
  sinrNR: "15",
  sinrNRPercentage: 85,
  signalPercentage: 82,
  signalAssessment: "优秀",
  cpuUsagePercent: 23,
  ramUsagePercent: 46,
  ramUsedHuman: "230 MB",
  ramTotalHuman: "512 MB",
  temperature: "43",
  internetConnection: "已连接",
  lastUpdate: "2026-09-01 12:00:00",
  uptimeParts: { days: 3, hours: 4, minutes: 12 },
  nr_rx_bytes: 1536000,
  nr_tx_bytes: 512000,
  nr_rx_human: "1.5 MB",
  nr_tx_human: "500 KB",
  nr_dl_speed: "1.0 MB/s",
  nr_ul_speed: "100 KB/s",
};

const HISTORY = {
  points: [
    { t: 1767225600, sig: 70, rxr: 1024, txr: 512 },
    { t: 1767225660, sig: 82, rxr: 2048, txr: 256 },
    { t: 1767225720, sig: 76, rxr: 4096, txr: 128 },
  ],
};

function createClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderPage(client = createClient()) {
  const utils = render(
    <QueryClientProvider client={client}>
      <DashboardPage />
    </QueryClientProvider>,
  );
  return { client, ...utils };
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
  vi.mocked(dashboardData).mockResolvedValue(GOOD);
  vi.mocked(historyData).mockResolvedValue(HISTORY);
  // 放慢主轮询(60s),避免测试期间额外 tick 干扰调用计数断言
  localStorage.setItem("refreshRate", "60");
});

afterEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
});

describe("dashboard 渲染", () => {
  test("mock 数据上屏:英雄条/仪表盘/信号质量/趋势/网络信息", async () => {
    const { container } = renderPage();
    // 英雄条:连接状态 + 运营商(英雄条与网络信息卡各一处)+ 激活 SIM/uptime 格式化
    expect(await screen.findByText("已连接")).toBeInTheDocument();
    expect(screen.getAllByText("中国移动").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText("已激活卡2").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText("3 天 4 小时 12 分钟").length).toBeGreaterThanOrEqual(1);
    // 英雄条右侧:服务端速率 + 累计流量 count-up 终值
    expect(screen.getByText("1.0 MB/s")).toBeInTheDocument();
    expect(screen.getByText("100 KB/s")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("1.5 MB")).toBeInTheDocument());
    // 4 仪表盘 + 趋势图:jsdom 无 canvas → charts 内建 fallback
    await waitFor(() => expect(container.querySelectorAll("[data-chart-fallback]").length).toBe(5));
    // 信号质量卡:network_mode "NR5G" 驻留 → 仅 5G 三条渐变进度条 + 评估徽章
    expect(screen.getByText("-12 / 65%")).toBeInTheDocument();
    expect(screen.getByText("15 / 85%")).toBeInTheDocument();
    expect(screen.getAllByText("优秀").length).toBeGreaterThanOrEqual(2);
    expect(container.querySelectorAll('[data-slot="quality-row"]')).toHaveLength(3);
    // 趋势 meta:最新信号
    expect(screen.getByText("76%")).toBeInTheDocument();
    // 网络信息卡:完整态含 MCCMNC,值 mono + 复制按钮
    expect(screen.getByText("MCCMNC")).toBeInTheDocument();
    expect(screen.getByText("10.0.0.1")).toBeInTheDocument();
    expect(screen.getByText("cmnet")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "复制" }).length).toBeGreaterThan(0);
  });

  test("精简/完整切换:MCCMNC/CELL ID/TAC 行隐藏,localStorage 沿用旧版键", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findAllByText("中国移动");
    expect(screen.getByText("MCCMNC")).toBeInTheDocument();
    expect(screen.getByText("CELL ID")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "精简显示" }));
    expect(screen.queryByText("MCCMNC")).toBeNull();
    expect(screen.queryByText("CELL ID")).toBeNull();
    expect(screen.queryByText("TAC")).toBeNull();
    // APN 等非完整态专属行仍显示
    expect(screen.getByText("cmnet")).toBeInTheDocument();
    expect(localStorage.getItem("simpleadmin.dashboard.compactNetworkInfo")).toBe("1");

    await user.click(screen.getByRole("button", { name: "完整显示" }));
    expect(screen.getByText("MCCMNC")).toBeInTheDocument();
    expect(localStorage.getItem("simpleadmin.dashboard.compactNetworkInfo")).toBe("0");
  });

  test("精简态从 localStorage 恢复", async () => {
    localStorage.setItem("simpleadmin.dashboard.compactNetworkInfo", "1");
    renderPage();
    await screen.findAllByText("中国移动");
    expect(screen.queryByText("MCCMNC")).toBeNull();
    expect(screen.getByRole("button", { name: "完整显示" })).toBeInTheDocument();
  });

  test("刷新频率变更:应用后持久化 refreshRate 键并清空草稿;超范围钳制 2–60", async () => {
    const user = userEvent.setup();
    localStorage.removeItem("refreshRate");
    renderPage();
    await screen.findAllByText("中国移动");
    const input = screen.getByLabelText("刷新频率（最少 2 秒）");
    expect(input).toHaveAttribute("placeholder", "2s");

    await user.type(input, "10");
    await user.click(screen.getByRole("button", { name: "应用" }));
    expect(localStorage.getItem("refreshRate")).toBe("10");
    expect(input).toHaveValue(null);
    expect(input).toHaveAttribute("placeholder", "10s");

    await user.type(input, "999");
    await user.click(screen.getByRole("button", { name: "应用" }));
    expect(localStorage.getItem("refreshRate")).toBe("60");

    await user.type(input, "1");
    await user.click(screen.getByRole("button", { name: "应用" }));
    expect(localStorage.getItem("refreshRate")).toBe("2");
  });

  test("pending:true 保留旧值 + StatusChip(common:getting 插值),绝不假空态", async () => {
    const client = createClient();
    renderPage(client);
    expect(await screen.findByText("已连接")).toBeInTheDocument();

    vi.mocked(dashboardData).mockResolvedValue({
      pending: true,
      internetConnection: "未连接",
      network_provider: "-",
      signalPercentage: 0,
    });
    await act(async () => {
      await client.invalidateQueries({ queryKey: ["dashboardData"] });
    });

    expect(await screen.findByText("获取网络信息中...")).toBeInTheDocument();
    // 旧值保留:英雄条仍是好数据的运营商/连接状态,pending 载荷的 "-" 不上屏
    expect(screen.getAllByText("中国移动").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("已连接")).toBeInTheDocument();
    expect(screen.queryByText("未连接")).toBeNull();
  });

  test("at_cache_updated → 去抖 800ms 后静默刷新(旧值保留)", async () => {
    let handler: ServerEventHandler | null = null;
    vi.mocked(gateway.onServerEvent).mockImplementation((name, eventHandler) => {
      if (name === "at_cache_updated") handler = eventHandler;
      return () => {};
    });
    const client = createClient();
    renderPage(client);
    expect(await screen.findByText("已连接")).toBeInTheDocument();
    expect(handler).not.toBeNull();
    const callsBefore = vi.mocked(dashboardData).mock.calls.length;

    vi.mocked(dashboardData).mockResolvedValue({ ...GOOD, network_provider: "中国电信" });
    act(() => {
      handler?.({ type: "event", event: "at_cache_updated", data: { command: "ATI" } });
    });
    // 等过去抖窗口(800ms,真实时钟,先例:common.test.tsx motion 真实时钟断言终态)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 850));
    });
    await waitFor(() => expect(screen.getAllByText("中国电信").length).toBeGreaterThanOrEqual(1));
    expect(vi.mocked(dashboardData).mock.calls.length).toBeGreaterThan(callsBefore);
  });

  test("历史数据不足 2 点 → EmptyState 说明", async () => {
    vi.mocked(historyData).mockResolvedValue({ points: [{ t: 1767225600, sig: 70 }] });
    renderPage();
    expect(await screen.findByText("已连接")).toBeInTheDocument();
    expect(screen.getByText("暂无历史数据，页面保持打开后将自动积累")).toBeInTheDocument();
  });
});

describe("dashboard lib 纯函数(旧版 index.js 语义)", () => {
  test.each([
    [0, "0 B"],
    [1023, "1023 B"],
    [1024, "1.0 KB"],
    [1536, "1.5 KB"],
    [1536000, "1.5 MB"],
    [512000, "500 KB"],
    [5 * 1024 ** 3, "5.0 GB"],
    [-1, "-"],
    [Number.NaN, "-"],
  ])("humanBytes(%p) === %p", (input, expected) => {
    expect(humanBytes(input)).toBe(expected);
  });

  test("humanBytesPerSec 追加 /s,非法值保持 -", () => {
    expect(humanBytesPerSec(2048)).toBe("2.0 KB/s");
    expect(humanBytesPerSec(-1)).toBe("-");
  });

  test("clampRefreshRate/readStoredRefreshRate:钳制 2–60,非法存储回默认 2", () => {
    expect(clampRefreshRate(1)).toBe(2);
    expect(clampRefreshRate(999)).toBe(60);
    expect(clampRefreshRate(Number.NaN)).toBe(2);
    localStorage.setItem("refreshRate", "10");
    expect(readStoredRefreshRate()).toBe(10);
    localStorage.setItem("refreshRate", "1");
    expect(readStoredRefreshRate()).toBe(2);
    localStorage.setItem("refreshRate", "abc");
    expect(readStoredRefreshRate()).toBe(2);
    localStorage.setItem("refreshRate", "120");
    expect(readStoredRefreshRate()).toBe(60);
  });

  test("describeActiveSim 矩阵(旧版 formatActiveSimStatus)", () => {
    expect(describeActiveSim("已激活", "卡2")).toEqual({ kind: "activeSlot", index: "2" });
    expect(describeActiveSim("已激活", "2")).toEqual({ kind: "activeSlot", index: "2" });
    expect(describeActiveSim("已激活", "-")).toEqual({ kind: "active" });
    expect(describeActiveSim("已激活", undefined)).toEqual({ kind: "active" });
    expect(describeActiveSim("未激活", "卡1")).toEqual({ kind: "inactiveSlot", index: "1" });
    expect(describeActiveSim("未激活", "")).toEqual({ kind: "inactive" });
    expect(describeActiveSim("未知", "卡3")).toEqual({ kind: "plain", text: "未知", index: "3" });
    expect(describeActiveSim(undefined, undefined)).toEqual({ kind: "plain", text: "-" });
    expect(describeActiveSim("-", "-")).toEqual({ kind: "plain", text: "-" });
  });

  test("formatUptimeParts:天/小时为 0 省略、分钟恒显、en 单数去 s、null 回退", () => {
    const zh = { days: "天", hours: "小时", minutes: "分钟" };
    const en = { days: "days", hours: "hours", minutes: "minutes" };
    expect(formatUptimeParts({ days: 3, hours: 4, minutes: 12 }, zh, "未知时间", false)).toBe(
      "3 天 4 小时 12 分钟",
    );
    expect(formatUptimeParts({ days: 0, hours: 0, minutes: 5 }, zh, "未知时间", false)).toBe(
      "5 分钟",
    );
    expect(formatUptimeParts({ days: 0, hours: 0, minutes: 0 }, zh, "未知时间", false)).toBe(
      "0 分钟",
    );
    expect(formatUptimeParts(undefined, zh, "未知时间", false)).toBe("未知时间");
    expect(formatUptimeParts({ days: 1, hours: 1, minutes: 1 }, en, "Unknown Time", true)).toBe(
      "1 day 1 hour 1 minute",
    );
    expect(formatUptimeParts({ days: 2, hours: 0, minutes: 30 }, en, "Unknown Time", true)).toBe(
      "2 days 30 minutes",
    );
  });

  test("describeAssessment:服务端中文枚举 → 词条键 + tone,未知/占位归 muted", () => {
    expect(describeAssessment("优秀")).toEqual({ key: "excellent", tone: "success" });
    expect(describeAssessment("良好")).toEqual({ key: "good", tone: "info" });
    expect(describeAssessment("一般")).toEqual({ key: "fair", tone: "warning" });
    expect(describeAssessment("差")).toEqual({ key: "poor", tone: "danger" });
    expect(describeAssessment("无信号")).toEqual({ key: "noSignal", tone: "muted" });
    expect(describeAssessment("-")).toEqual({ tone: "muted", text: undefined });
    expect(describeAssessment(undefined)).toEqual({ tone: "muted", text: undefined });
  });

  test("visibleSignalRats:按当前驻留制式过滤,未就绪/未知保留双制式", () => {
    expect(visibleSignalRats("NR5G-SA")).toEqual(["nr"]);
    expect(visibleSignalRats("NR5G-NSA")).toEqual(["nr"]);
    expect(visibleSignalRats("LTE")).toEqual(["lte"]);
    expect(visibleSignalRats("-")).toEqual(["lte", "nr"]);
    expect(visibleSignalRats(undefined)).toEqual(["lte", "nr"]);
    expect(visibleSignalRats("未插卡")).toEqual(["lte", "nr"]);
  });

  test("resolveTraffic:服务端速率优先;缺失时按字节增量折算;计数器回绕按 0", () => {
    const now = 1_000_000;
    const withServer = resolveTraffic(
      { nr_rx_bytes: 1000, nr_tx_bytes: 500, nr_dl_speed: "9 MB/s", nr_ul_speed: "1 MB/s" },
      null,
      now,
    );
    expect(withServer.view.dl).toBe("9 MB/s");
    expect(withServer.view.ul).toBe("1 MB/s");
    expect(withServer.view.dlRate).toBeNull();
    expect(withServer.next).toEqual({ rx: 1000, tx: 500, t: now });

    const first = resolveTraffic({ nr_rx_bytes: 1000, nr_tx_bytes: 500 }, null, now);
    expect(first.view.dl).toBe("-");
    expect(first.view.rxTotal).toBe("1000 B");

    const second = resolveTraffic(
      { nr_rx_bytes: 1000 + 2048, nr_tx_bytes: 500 + 1024 },
      first.next,
      now + 1000,
    );
    expect(second.view.dl).toBe("2.0 KB/s");
    expect(second.view.ul).toBe("1.0 KB/s");

    const wrapped = resolveTraffic({ nr_rx_bytes: 10, nr_tx_bytes: 5 }, second.next, now + 2000);
    expect(wrapped.view.dl).toBe("0 B/s");
  });

  test("history 取值纯函数", () => {
    expect(historyLatestSignal([])).toBe("-");
    expect(historyLatestSignal(HISTORY.points)).toBe("76%");
    expect(historyLatestRxRate(HISTORY.points)).toBe("4.0 KB/s");
  });
});
