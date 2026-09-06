// 网络详情页单测:纯函数矩阵(字节/速率/租期格式化、速率滚动窗口)+ 组件渲染 mock 数据上屏 +
// pending 保留旧值 + chip(§5.2)+ 轮询契约(5s + visibility 门控)。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";

import "@/lib/i18n";
import { networkDetail } from "@/lib/api";
import type { NetworkDetail } from "@/lib/api";

import { NETDETAIL_QUERY_KEY, isNetdetailPendingError, useRateHistory } from "./hooks";
import {
  NetdetailPendingError,
  RATE_HISTORY_LIMIT,
  appendRatePoint,
  formatBytes,
  formatLease,
  formatRate,
  hasAddress,
  interfaceStateTone,
  leaseTone,
} from "./lib";
import NetdetailPage from "./page";

vi.mock("@/lib/api", () => ({
  networkDetail: vi.fn(),
  gateway: { onServerEvent: vi.fn(() => vi.fn()) },
}));

const networkDetailMock = vi.mocked(networkDetail);

const GOOD_DATA: NetworkDetail = {
  pending: false,
  wanIPv4: "10.0.0.1",
  wanIPv6: "240e::1",
  lanGateway: "192.168.5.1",
  interfaces: [
    {
      name: "rmnet_data0",
      mac: "AA:BB:CC:DD:EE:01",
      state: "up",
      mtu: 1500,
      rxBytes: 1536,
      txBytes: 2_097_152,
      rxRate: 1024,
      txRate: 512,
    },
    {
      name: "bridge0",
      mac: "AA:BB:CC:DD:EE:02",
      state: "down",
      mtu: 1500,
      rxBytes: 0,
      txBytes: 0,
      rxRate: 0,
      txRate: 0,
    },
  ],
  clients: [
    { mac: "11:22:33:44:55:66", ip: "192.168.5.20", hostname: "phone", leaseSeconds: 3661 },
    { mac: "77:88:99:AA:BB:CC", ip: "192.168.5.21", hostname: "", leaseSeconds: -1 },
  ],
};

const PENDING_DATA: NetworkDetail = { pending: true };

const LEASE_UNITS = { days: "天", hours: "小时", minutes: "分钟", underOneMinute: "不足1分钟" };

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity, staleTime: 0 } },
  });
}

function renderPage(client = makeClient()) {
  const utils = render(
    <QueryClientProvider client={client}>
      <NetdetailPage />
    </QueryClientProvider>,
  );
  return { ...utils, client };
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
});

describe("netdetail/lib 纯函数", () => {
  test("formatBytes:0/负值 → '0 B';B 整数;KB 及以上两位小数;逐级进位", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(-5)).toBe("0 B");
    expect(formatBytes(undefined)).toBe("0 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1024)).toBe("1.00 KB");
    expect(formatBytes(1536)).toBe("1.50 KB");
    expect(formatBytes(2_097_152)).toBe("2.00 MB");
    expect(formatBytes(3_221_225_472)).toBe("3.00 GB");
    expect(formatBytes(1_099_511_627_776)).toBe("1.00 TB");
    expect(formatBytes(2 * 1024 ** 5)).toBe("2048.00 TB"); // 顶级单位封顶
  });

  test("formatRate:formatBytes + '/s'", () => {
    expect(formatRate(1024)).toBe("1.00 KB/s");
    expect(formatRate(0)).toBe("0 B/s");
  });

  test("formatLease 矩阵:-1/非法 → '—';天/小时/分钟组合;不足 1 分钟", () => {
    expect(formatLease(-1, LEASE_UNITS)).toBe("—");
    expect(formatLease(undefined, LEASE_UNITS)).toBe("—");
    expect(formatLease(Number.NaN, LEASE_UNITS)).toBe("—");
    expect(formatLease(90061, LEASE_UNITS)).toBe("1 天 1 小时");
    expect(formatLease(86400, LEASE_UNITS)).toBe("1 天");
    expect(formatLease(3661, LEASE_UNITS)).toBe("1 小时 1 分钟");
    expect(formatLease(3600, LEASE_UNITS)).toBe("1 小时");
    expect(formatLease(300, LEASE_UNITS)).toBe("5 分钟");
    expect(formatLease(30, LEASE_UNITS)).toBe("不足1分钟");
    expect(formatLease(0, LEASE_UNITS)).toBe("不足1分钟");
  });

  test("appendRatePoint:按接口名累积,滚动窗口 ≤30 点,不改动入参", () => {
    let history = appendRatePoint({}, GOOD_DATA.interfaces);
    expect(history.rmnet_data0).toEqual({ rx: [1024], tx: [512] });
    expect(history.bridge0).toEqual({ rx: [0], tx: [0] });
    const frozen = history;
    history = appendRatePoint(history, [{ name: "rmnet_data0", rxRate: 2048, txRate: 0 }]);
    expect(history.rmnet_data0).toEqual({ rx: [1024, 2048], tx: [512, 0] });
    expect(frozen.rmnet_data0.rx).toEqual([1024]); // 不可变更新
    for (let i = 0; i < RATE_HISTORY_LIMIT + 10; i += 1) {
      history = appendRatePoint(history, [{ name: "rmnet_data0", rxRate: i, txRate: i }]);
    }
    expect(history.rmnet_data0.rx).toHaveLength(RATE_HISTORY_LIMIT);
    expect(history.rmnet_data0.rx[RATE_HISTORY_LIMIT - 1]).toBe(RATE_HISTORY_LIMIT + 9);
  });

  test("appendRatePoint:无名接口跳过,undefined 列表容错", () => {
    expect(appendRatePoint({}, undefined)).toEqual({});
    expect(appendRatePoint({}, [{ rxRate: 1 }, { name: "", rxRate: 2 }])).toEqual({});
  });

  test("chip 色调/地址判定", () => {
    expect(interfaceStateTone("up")).toBe("success");
    expect(interfaceStateTone("unknown")).toBe("success");
    expect(interfaceStateTone("down")).toBe("muted");
    expect(leaseTone(3661)).toBe("info");
    expect(leaseTone(-1)).toBe("muted");
    expect(hasAddress("10.0.0.1")).toBe(true);
    expect(hasAddress("-")).toBe(false);
    expect(hasAddress(undefined)).toBe(false);
    expect(isNetdetailPendingError(new NetdetailPendingError())).toBe(true);
    expect(isNetdetailPendingError(new Error("x"))).toBe(false);
  });
});

describe("useRateHistory", () => {
  test("dataUpdatedAt 变化才追加(同帧重复渲染不重复累积)", async () => {
    let captured: ReturnType<typeof useRateHistory> = { rmnet_data0: { rx: [], tx: [] } };
    function Probe({ stamp }: { stamp: number }) {
      captured = useRateHistory(GOOD_DATA.interfaces, stamp);
      return null;
    }
    const { rerender } = render(<Probe stamp={1} />);
    await waitFor(() => expect(captured.rmnet_data0?.rx).toEqual([1024]));
    rerender(<Probe stamp={1} />);
    await waitFor(() => expect(captured.rmnet_data0?.rx).toEqual([1024]));
    rerender(<Probe stamp={2} />);
    await waitFor(() => expect(captured.rmnet_data0?.rx).toEqual([1024, 1024]));
  });
});

describe("NetdetailPage 渲染", () => {
  test("mock 数据上屏:地址卡/接口表人性化/租期 chip/在线设备数", async () => {
    networkDetailMock.mockResolvedValue(GOOD_DATA);
    const { container } = renderPage();

    expect(await screen.findByText("10.0.0.1")).toBeInTheDocument();
    expect(screen.getByText("240e::1")).toBeInTheDocument();
    expect(screen.getByText("192.168.5.1")).toBeInTheDocument();
    // 接口表:累计/速率人性化
    expect(screen.getByText("1.50 KB")).toBeInTheDocument();
    expect(screen.getByText("2.00 MB")).toBeInTheDocument();
    expect(screen.getByText("1.00 KB/s")).toBeInTheDocument();
    expect(screen.getByText("512 B/s")).toBeInTheDocument();
    // 状态 chip:up=success / down=muted
    expect(screen.getByText("up")).toHaveClass("bg-success-soft");
    expect(screen.getByText("down")).toHaveClass("bg-surface-3");
    // 2 接口 × 2 速率 Sparkline(jsdom 降级占位)
    expect(container.querySelectorAll("[data-chart-fallback]")).toHaveLength(4);
    // 局域网设备:主机名/租期 chip
    expect(screen.getByText("phone")).toBeInTheDocument();
    expect(screen.getByText("1 小时 1 分钟")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
    // 在线设备数 MetricCard
    expect(container.querySelector('[data-slot="metric-value"]')).toHaveTextContent("2");
  });

  test("标题栏刷新按钮触发 refetch", async () => {
    networkDetailMock.mockResolvedValue(GOOD_DATA);
    renderPage();
    expect(await screen.findByText("10.0.0.1")).toBeInTheDocument();
    const before = networkDetailMock.mock.calls.length;
    await act(async () => {
      screen.getByRole("button", { name: /刷新/ }).click();
    });
    await waitFor(() => expect(networkDetailMock.mock.calls.length).toBeGreaterThan(before));
  });

  test("pending 保留旧值:转 pending 后旧地址仍在屏 + 获取中 chip", async () => {
    networkDetailMock.mockResolvedValue(GOOD_DATA);
    const { client } = renderPage();
    expect(await screen.findByText("10.0.0.1")).toBeInTheDocument();

    networkDetailMock.mockResolvedValue(PENDING_DATA);
    await act(async () => {
      await client.refetchQueries({ queryKey: NETDETAIL_QUERY_KEY });
    });
    expect(await screen.findByText("获取中...")).toBeInTheDocument();
    expect(screen.getByText("10.0.0.1")).toBeInTheDocument();
  });

  test("加载失败(非 pending)→ ErrorRetry", async () => {
    networkDetailMock.mockRejectedValue(new Error("gateway not connected"));
    renderPage();
    expect(await screen.findByRole("alert")).toHaveTextContent("网络详情加载失败");
  });
});

describe("轮询契约", () => {
  test("5s 间隔 + visibility 门控(refetchIntervalInBackground:false)", async () => {
    networkDetailMock.mockResolvedValue(GOOD_DATA);
    const client = makeClient();
    renderPage(client);
    await waitFor(() => expect(networkDetailMock).toHaveBeenCalled());
    const query = client.getQueryCache().find({ queryKey: NETDETAIL_QUERY_KEY });
    const options = query?.options as unknown as {
      refetchInterval?: number;
      refetchIntervalInBackground?: boolean;
    };
    expect(options.refetchInterval).toBe(5_000);
    expect(options.refetchIntervalInBackground).toBe(false);
  });
});
