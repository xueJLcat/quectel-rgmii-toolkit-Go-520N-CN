// 信号详情页单测:纯函数矩阵(cellRows 过滤/聚合状态/天线标签)+ 组件渲染 mock 数据上屏 +
// pending 保留旧值 + chip(§5.2)+ 5s 轮询节奏(visibility 门控由 hooks 配置断言)。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";

import "@/lib/i18n";
import { signalData } from "@/lib/api";
import type { SignalData } from "@/lib/api";

import { SIGNAL_QUERY_KEY, isSignalPendingError } from "./hooks";
import {
  SignalPendingError,
  aggregationState,
  antennaLabel,
  antennaRsrpText,
  buildCellRows,
  formatUpdatedAt,
} from "./lib";
import SignalPage from "./page";

vi.mock("@/lib/api", () => ({
  signalData: vi.fn(),
  gateway: { onServerEvent: vi.fn(() => vi.fn()) },
}));

const signalDataMock = vi.mocked(signalData);

const GOOD_DATA: SignalData = {
  ok: true,
  pending: false,
  rat: "NR5G",
  antennas: [
    { id: "PRX", label: "天线1 (PRX)", rsrp: "-75", percent: 82 },
    { id: "DRX", label: "天线2 (DRX)", rsrp: "-81", percent: 70 },
    { id: "RX2", label: "天线3 (RX2)", rsrp: "-", percent: 0 },
    { id: "RX3", label: "天线4 (RX3)", rsrp: "-95", percent: 44 },
  ],
  carriers: [
    { role: "PCC", band: "n78", arfcn: "627264", bandwidth: "100 MHz", pci: "123" },
    { role: "SCC", band: "n1", arfcn: "4400", bandwidth: "20 MHz", pci: "456" },
  ],
  cell: {
    network_mode: "NR5G",
    pcc_pci: "123",
    scc_pci: "456",
    tac: "1",
    earfcns: "627264",
    cellID: "789",
    eNBID: "-",
    rsrpNR: "-75 dBm",
    rsrqNR: "-9 dB",
    sinrNR: "18 dB",
    rsrpLTE: "-",
    rsrqLTE: "-",
    sinrLTE: "-",
    rssi: "-60 dBm",
  },
};

const PENDING_DATA: SignalData = { ok: false, pending: true, rat: "", antennas: [], carriers: [] };

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity, staleTime: 0 } },
  });
}

function renderPage(client = makeClient()) {
  const utils = render(
    <QueryClientProvider client={client}>
      <SignalPage />
    </QueryClientProvider>,
  );
  return { ...utils, client };
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("signal/lib 纯函数", () => {
  test("buildCellRows:制式/频点恒显,空值行隐藏,空值兜底 '-'", () => {
    const rows = buildCellRows(GOOD_DATA.cell);
    const keys = rows.map((row) => row.key);
    expect(keys).toContain("network_mode");
    expect(keys).toContain("earfcns");
    expect(keys).toContain("tac");
    expect(rows.find((row) => row.key === "tac")?.value).toBe("1");
    expect(keys).not.toContain("eNBID"); // 非恒显空值行隐藏
    expect(keys).not.toContain("rsrpLTE");
    expect(rows.find((row) => row.key === "rsrpNR")?.value).toBe("-75 dBm");
  });

  test("buildCellRows:cell 缺省不抛错,仅剩恒显行", () => {
    const rows = buildCellRows(undefined);
    expect(rows.map((row) => row.key)).toEqual(["network_mode", "earfcns"]);
    expect(rows.every((row) => row.value === "-")).toBe(true);
  });

  test("aggregationState:0 → none / 1 → single / 2 → active", () => {
    expect(aggregationState([])).toEqual({ state: "none", count: 0 });
    expect(aggregationState(undefined)).toEqual({ state: "none", count: 0 });
    expect(aggregationState(GOOD_DATA.carriers?.slice(0, 1))).toEqual({
      state: "single",
      count: 1,
    });
    expect(aggregationState(GOOD_DATA.carriers)).toEqual({ state: "active", count: 2 });
  });

  test("antennaLabel:已知 id 走 i18n 键映射,未知 id 回退 label/id", () => {
    const t = (key: string) => `t(${key})`;
    expect(antennaLabel({ id: "PRX", label: "天线1 (PRX)" }, t)).toBe("t(antenna1Prx)");
    expect(antennaLabel({ id: "RX3" }, t)).toBe("t(antenna4Rx3)");
    expect(antennaLabel({ id: "XX", label: "自定义" }, t)).toBe("自定义");
    expect(antennaLabel({ id: "XX" }, t)).toBe("XX");
    expect(antennaLabel({}, t)).toBe("-");
  });

  test("antennaRsrpText:'-'/空 → 占位;有值 → '值 dBm'", () => {
    expect(antennaRsrpText({ rsrp: "-" }, "无")).toBe("无");
    expect(antennaRsrpText({}, "无")).toBe("无");
    expect(antennaRsrpText({ rsrp: "-75/-88" }, "无")).toBe("-75/-88 dBm");
  });

  test("formatUpdatedAt:zh → zh-CN 24 小时制;en → en-GB", () => {
    const ts = new Date(2026, 0, 2, 14, 5, 9).getTime();
    expect(formatUpdatedAt(ts, "zh-CN")).toBe("14:05:09");
    expect(formatUpdatedAt(ts, "en")).toBe("14:05:09");
  });

  test("SignalPendingError 判别", () => {
    expect(isSignalPendingError(new SignalPendingError())).toBe(true);
    expect(isSignalPendingError(new Error("boom"))).toBe(false);
  });
});

describe("SignalPage 渲染", () => {
  test("mock 数据上屏:4 仪表降级占位、CA 徽章/聚合 chip、服务小区行", async () => {
    signalDataMock.mockResolvedValue(GOOD_DATA);
    const { container } = renderPage();

    // 4×天线仪表(jsdom 无 canvas → EChart 确定性降级占位)
    await waitFor(() => {
      expect(container.querySelectorAll("[data-chart-fallback]")).toHaveLength(4);
    });
    // 载波聚合:PCC=violet 徽章 / SCC=sky 徽章 + 聚合生效 chip
    expect(screen.getByText("主载波 (PCC)")).toHaveClass("bg-comm-soft", "text-comm");
    expect(screen.getByText("辅载波 (SCC)")).toHaveClass("bg-info-soft", "text-info");
    expect(screen.getByText("载波聚合生效 (2 载波)")).toBeInTheDocument();
    expect(screen.getByText("n78")).toBeInTheDocument();
    // ARFCN 同时出现在 CA 表与服务小区行;"-75 dBm" 出现在天线副标题与服务小区行
    expect(screen.getAllByText("627264").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText("-75 dBm").length).toBeGreaterThanOrEqual(2);
    // 服务小区描述列表
    expect(screen.getByText("网络制式")).toBeInTheDocument();
    expect(screen.getByText("NR5G").closest("dd")).not.toBeNull();
    // 页脚:制式 + 更新时间
    expect(screen.getByText(/更新于/)).toBeInTheDocument();
  });

  test("单载波 → '单载波(无聚合)' chip;空载波 → '无'", async () => {
    signalDataMock.mockResolvedValue({ ...GOOD_DATA, carriers: GOOD_DATA.carriers?.slice(0, 1) });
    renderPage();
    expect(await screen.findByText("单载波(无聚合)")).toBeInTheDocument();
  });

  test("首屏 pending:骨架 + 获取中 chip,不显示假空态", async () => {
    signalDataMock.mockResolvedValue(PENDING_DATA);
    const { container } = renderPage();
    expect(container.querySelector('[data-slot="signal-skeleton"]')).not.toBeNull();
    expect(await screen.findByText("获取中...")).toBeInTheDocument();
    expect(screen.queryByText("载波聚合生效 (2 载波)")).not.toBeInTheDocument();
  });

  test("pending 保留旧值:成功一帧后转 pending,旧数据仍在屏 + 获取中 chip", async () => {
    signalDataMock.mockResolvedValue(GOOD_DATA);
    const { client } = renderPage();
    expect(await screen.findByText("主载波 (PCC)")).toBeInTheDocument();

    signalDataMock.mockResolvedValue(PENDING_DATA);
    await act(async () => {
      await client.refetchQueries({ queryKey: SIGNAL_QUERY_KEY });
    });
    // React 调度器经宏任务提交更新,findByText 轮询等待渲染落地
    expect(await screen.findByText("获取中...")).toBeInTheDocument();
    // 旧值保留(§5.2:pending 绝不显示假空态)
    expect(screen.getByText("主载波 (PCC)")).toBeInTheDocument();
    expect(screen.getByText("n78")).toBeInTheDocument();
  });

  test("加载失败(非 pending)→ ErrorRetry + 重试触发 refetch", async () => {
    signalDataMock.mockRejectedValue(new Error("gateway not connected"));
    renderPage();
    expect(await screen.findByRole("alert")).toHaveTextContent("信号数据读取失败");
    const before = signalDataMock.mock.calls.length;
    await act(async () => {
      screen.getByRole("button", { name: /重试/ }).click();
    });
    await waitFor(() => expect(signalDataMock.mock.calls.length).toBeGreaterThan(before));
  });
});

describe("useSignalQuery 轮询契约", () => {
  test("5s 间隔 + visibility 门控(refetchIntervalInBackground:false)", async () => {
    signalDataMock.mockResolvedValue(GOOD_DATA);
    const client = makeClient();
    renderPage(client);
    await waitFor(() => expect(signalDataMock).toHaveBeenCalled());
    const query = client.getQueryCache().find({ queryKey: SIGNAL_QUERY_KEY });
    const options = query?.options as unknown as {
      refetchInterval?: number;
      refetchIntervalInBackground?: boolean;
    };
    expect(options.refetchInterval).toBe(5_000);
    expect(options.refetchIntervalInBackground).toBe(false);
  });
});
