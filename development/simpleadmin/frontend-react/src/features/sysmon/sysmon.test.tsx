// 系统监控页测试:mock @/lib/api 注入固定数据(先例 src/app/shell.test.tsx),覆盖:
// 渲染上屏(仪表盘 fallback/进程表/MetricCard/温度 HeatBar)、sort=cpu|mem 切换、温度手动刷新、
// 失败 ErrorRetry、parseQtemp 纯函数矩阵(17 行正常/缺行/垃圾行/空)与格式化函数。
// jsdom 无 canvas:GaugeChart 走 charts 内建 data-chart-fallback 降级;HeatBar/MetricBar 为纯 div。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import "@/lib/i18n";
import SysmonPage from "@/features/sysmon/page";
import {
  formatCpu,
  formatNum,
  formatRamUsage,
  loadGaugePercent,
  parseLoadAverage,
  parseQtemp,
  parseUptimeDays,
  visiblePollInterval,
} from "@/features/sysmon/lib";
import { atData, systemMonitor } from "@/lib/api";

vi.mock("@/lib/api", () => ({
  systemMonitor: vi.fn(),
  atData: vi.fn(),
}));

const QTEMP_SENSORS = [
  `+QTEMP:"modem-lte-sub6-pa1","40"`,
  `+QTEMP:"modem-sdr0-pa0","0"`,
  `+QTEMP:"modem-sdr0-pa1","0"`,
  `+QTEMP:"modem-sdr0-pa2","0"`,
  `+QTEMP:"modem-sdr1-pa0","0"`,
  `+QTEMP:"modem-sdr1-pa1","0"`,
  `+QTEMP:"modem-sdr1-pa2","0"`,
  `+QTEMP:"modem-mmw0","-273"`,
  `+QTEMP:"aoss-0-usr","43"`,
  `+QTEMP:"cpuss-0-usr","41"`,
  `+QTEMP:"mdmq6-0-usr","42"`,
  `+QTEMP:"mdmss-0-usr","43"`,
  `+QTEMP:"mdmss-1-usr","41"`,
  `+QTEMP:"mdmss-2-usr","42"`,
  `+QTEMP:"mdmss-3-usr","41"`,
  `+QTEMP:"modem-lte-sub6-pa2","40"`,
  `+QTEMP:"modem-ambient-usr","41"`,
];

// 与 go-build/simpleadmin-go/cmd/simpleadmin-httpd/mock_at_responses.go 的响应结构一致:
// 命令回显行 + 传感器行 + OK
const QTEMP_RAW = ["AT+QTEMP", ...QTEMP_SENSORS, "", "OK", ""].join("\r\n");

const MONITOR = {
  cpuUsagePercent: 23,
  ramUsagePercent: 46,
  ramUsedHuman: "230 MB",
  ramTotalHuman: "512 MB",
  loadAverage: "0.42 0.35 0.28",
  uptime: "up 3 day, 4 hour, 12 min",
  processCount: 103,
  processes: [
    {
      pid: 1,
      user: "root",
      name: "init",
      state: "S",
      cpuPercent: 101.5,
      memPercent: 1.2,
      rssHuman: "6 MB",
    },
    {
      pid: 888,
      user: "root",
      name: "simpleadmin-httpd",
      state: "R",
      cpuPercent: 12.3,
      memPercent: 4.5,
      rssHuman: "23 MB",
    },
  ],
};

function createClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderPage(client = createClient()) {
  const utils = render(
    <QueryClientProvider client={client}>
      <SysmonPage />
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
  vi.mocked(systemMonitor).mockResolvedValue(MONITOR);
  vi.mocked(atData).mockResolvedValue({ ok: true, response: QTEMP_RAW });
});

afterEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
});

describe("sysmon 渲染", () => {
  test("mock 数据上屏:仪表盘/进程总数/进程表/温度面板", async () => {
    const { container } = renderPage();
    // 进程表数据到达(锚点),随后 MetricCard count-up 终值(jsdom rAF 慢于墙钟,放宽超时;
    // 先例 common.test.tsx 用假时钟驱动,此处走真实时钟断言终态)
    expect(await screen.findByText("simpleadmin-httpd")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("103")).toBeInTheDocument(), { timeout: 5000 });
    // 4 仪表盘 jsdom 无 canvas → fallback
    expect(container.querySelectorAll("[data-chart-fallback]")).toHaveLength(4);
    // 仪表盘 caption:RAM 已用/总量、负载原始三元组、运行时长原文
    expect(screen.getByText("230 MB / 512 MB")).toBeInTheDocument();
    expect(screen.getByText("0.42 0.35 0.28")).toBeInTheDocument();
    expect(screen.getByText("up 3 day, 4 hour, 12 min")).toBeInTheDocument();
    // 进程表:两行数据 + >100% CPU 的单核口径 tooltip 触发器
    expect(screen.getByText("init")).toBeInTheDocument();
    expect(screen.getByText("101.5%")).toBeInTheDocument();
    expect(screen.getByText("4.5")).toBeInTheDocument();
    expect(container.querySelector(".cursor-help")).not.toBeNull();
    // 更新时间 HH:MM:SS(旧版客户端时间戳)
    expect(screen.getByText(/^\d{2}:\d{2}:\d{2}$/)).toBeInTheDocument();
    // 温度面板:17 条 HeatBar + footer 低频说明;调用口径 manual_at + AT+QTEMP
    await waitFor(() => expect(screen.getAllByText(/°C$/)).toHaveLength(17));
    expect(screen.getByText("modem-lte-sub6-pa1")).toBeInTheDocument();
    expect(screen.getAllByText("43°C")).toHaveLength(2);
    expect(screen.getByText("-273°C")).toBeInTheDocument();
    expect(screen.getByText("低频刷新（60 秒），以免占用 AT 通道")).toBeInTheDocument();
    expect(vi.mocked(systemMonitor)).toHaveBeenCalledWith("cpu");
    expect(vi.mocked(atData)).toHaveBeenCalledWith("manual_at", { command: "AT+QTEMP" });
  });

  test("sort 切换:点按内存 → systemMonitor('mem'),tab 选中态随动", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText("simpleadmin-httpd");
    const memTab = screen.getByRole("tab", { name: "按内存" });
    await user.click(memTab);
    await waitFor(() => expect(vi.mocked(systemMonitor)).toHaveBeenCalledWith("mem"));
    expect(memTab).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "按 CPU" })).toHaveAttribute("aria-selected", "false");
  });

  test("温度面板手动刷新:再拉一次 AT+QTEMP", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText("modem-lte-sub6-pa1");
    expect(vi.mocked(atData)).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole("button", { name: "刷新" }));
    await waitFor(() => expect(vi.mocked(atData)).toHaveBeenCalledTimes(2));
  });

  test("systemMonitor 失败:ErrorRetry 失败横幅 + 重试", async () => {
    vi.mocked(systemMonitor).mockRejectedValue(new Error("boom"));
    renderPage();
    expect(await screen.findByText("系统监控数据加载失败")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重试" })).toBeInTheDocument();
  });

  test("AT+QTEMP 失败:温度面板 ErrorRetry;解析全空:error 态 + 重试入口", async () => {
    vi.mocked(atData).mockRejectedValue(new Error("at busy"));
    const first = renderPage();
    // useQtempQuery 显式 retry:2(AT 通道退避),错误态约 3s 后落定
    expect(
      await screen.findByText("温度传感器读取失败", undefined, { timeout: 6000 }),
    ).toBeInTheDocument();
    first.unmount();

    vi.mocked(atData).mockResolvedValue({ ok: true, response: "AT+QTEMP\r\n\r\nOK\r\n" });
    renderPage();
    expect(await screen.findByText("未解析到温度传感器数据")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重试" })).toBeInTheDocument();
  });

  test("visibility 门控:document.hidden 时轮询间隔为 false", () => {
    expect(visiblePollInterval(3000)).toBe(3000);
    Object.defineProperty(document, "hidden", { configurable: true, value: true });
    try {
      expect(visiblePollInterval(3000)).toBe(false);
    } finally {
      Reflect.deleteProperty(document, "hidden");
    }
    expect(visiblePollInterval(3000)).toBe(3000);
  });
});

describe("parseQtemp 矩阵", () => {
  test("17 行正常响应全部解析(回显/OK/空行跳过,负值保留)", () => {
    const readings = parseQtemp(QTEMP_RAW);
    expect(readings).toHaveLength(17);
    expect(readings[0]).toEqual({ sensor: "modem-lte-sub6-pa1", temperature: 40 });
    expect(readings).toContainEqual({ sensor: "modem-mmw0", temperature: -273 });
    expect(readings.at(-1)).toEqual({ sensor: "modem-ambient-usr", temperature: 41 });
  });

  test("缺行:仅解析存在的传感器行", () => {
    const raw = 'AT+QTEMP\r\n+QTEMP:"aoss-0-usr","43"\r\n\r\nOK\r\n';
    expect(parseQtemp(raw)).toEqual([{ sensor: "aoss-0-usr", temperature: 43 }]);
  });

  test("垃圾行跳过(缺逗号/非数字温度/残行),合法行保留", () => {
    const raw = [
      '+QTEMP:"ok","30"',
      "+QTEMP:broken",
      "garbage line",
      '+QTEMP:"x","notanumber"',
      "+QTEMP:",
      '+QTEMP:"y","41"',
    ].join("\n");
    expect(parseQtemp(raw)).toEqual([
      { sensor: "ok", temperature: 30 },
      { sensor: "y", temperature: 41 },
    ]);
  });

  test("空输入/全垃圾 → 空数组(UI 归 error 态)", () => {
    expect(parseQtemp("")).toEqual([]);
    expect(parseQtemp("OK\r\n")).toEqual([]);
  });

  test("无引号与小数温度兼容", () => {
    expect(parseQtemp("+QTEMP: aoss-0,43")).toEqual([{ sensor: "aoss-0", temperature: 43 }]);
    expect(parseQtemp('+QTEMP:"pmic","41.5"')).toEqual([{ sensor: "pmic", temperature: 41.5 }]);
  });
});

describe("sysmon lib 纯函数(旧版 sysmon.js 语义)", () => {
  test("formatCpu/formatNum:1 位小数,非有限值 -", () => {
    expect(formatCpu(101.45)).toBe("101.5%");
    expect(formatCpu(12)).toBe("12.0%");
    expect(formatCpu("x")).toBe("-");
    expect(formatNum(4.56)).toBe("4.6");
    expect(formatNum(undefined)).toBe("-");
  });

  test("formatRamUsage:双有效值拼接,否则回退获取中", () => {
    expect(formatRamUsage("230 MB", "512 MB", "获取中...")).toBe("230 MB / 512 MB");
    expect(formatRamUsage("-", "512 MB", "获取中...")).toBe("获取中...");
    expect(formatRamUsage(undefined, undefined, "获取中...")).toBe("获取中...");
  });

  test("parseLoadAverage/loadGaugePercent:首值折算 4 核满载百分比", () => {
    expect(parseLoadAverage("0.42 0.35 0.28")).toBe(0.42);
    expect(parseLoadAverage("-")).toBeNull();
    expect(loadGaugePercent("4 4 4")).toBe(100);
    expect(loadGaugePercent("8 1 1")).toBe(100);
    expect(loadGaugePercent(undefined)).toBe(0);
  });

  test("parseUptimeDays:day/hour/min 与时钟格式折算天数", () => {
    expect(parseUptimeDays("up 3 day, 4 hour, 12 min")).toBeCloseTo(3 + 4 / 24 + 12 / 1440);
    expect(parseUptimeDays("up 0 days, 2:30, load average...")).toBeCloseTo(2.5 / 24);
    expect(parseUptimeDays("nonsense")).toBeNull();
    expect(parseUptimeDays(undefined)).toBeNull();
  });
});
