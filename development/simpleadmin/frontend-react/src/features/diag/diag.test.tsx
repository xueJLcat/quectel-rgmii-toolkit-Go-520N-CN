// diag 测试:纯函数(探测目标 URL/域名/DNS 服务器校验矩阵、延迟条换算)
// + 组件行为(一键全测串行执行与进度指示、运行时失败渲染为失败条目而非页面级错误、
// DNS 查询参数与结果列表)。vi.mock @/lib/api,diagData 序列由测试编排。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import "@/lib/i18n";
import DiagPage from "@/features/diag/page";
import { diagData } from "@/lib/api";
import type { DiagResponse } from "@/lib/api";

import {
  BUILTIN_PROBE_TARGETS,
  isValidDnsServer,
  isValidDomain,
  isValidProbeTarget,
  latencyBarPercent,
  normalizeProbeTarget,
} from "./lib";

vi.mock("@/lib/api", () => ({
  diagData: vi.fn(),
}));

const diagDataMock = vi.mocked(diagData);

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

function renderPage(): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <DiagPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  diagDataMock.mockResolvedValue({ ok: true, statusCode: 200, latencyMs: 20 });
});

describe("校验纯函数", () => {
  test("normalizeProbeTarget:http(s) URL / host[:port] 合法,其他 scheme 拒绝", () => {
    expect(normalizeProbeTarget("http://example.com")).toBe("http://example.com/");
    expect(normalizeProbeTarget("https://example.com/path?q=1")).not.toBeNull();
    expect(normalizeProbeTarget("223.5.5.5")).toBe("http://223.5.5.5/");
    expect(normalizeProbeTarget("example.com:8080")).toBe("http://example.com:8080/");
    expect(normalizeProbeTarget("ftp://example.com")).toBeNull();
    expect(normalizeProbeTarget("file:///etc/passwd")).toBeNull();
    expect(normalizeProbeTarget("http://")).toBeNull();
    expect(normalizeProbeTarget("")).toBeNull();
    expect(normalizeProbeTarget("   ")).toBeNull();
    expect(isValidProbeTarget("https://example.com")).toBe(true);
    expect(isValidProbeTarget("ftp://example.com")).toBe(false);
    expect(BUILTIN_PROBE_TARGETS.every((target) => isValidProbeTarget(target))).toBe(true);
  });

  test("isValidDomain:镜像后端 diagDomainPattern", () => {
    expect(isValidDomain("example.com")).toBe(true);
    expect(isValidDomain("a-b.cn")).toBe(true);
    expect(isValidDomain("xn--fiq228c.cn")).toBe(true);
    expect(isValidDomain("example.com.")).toBe(true);
    expect(isValidDomain("a..b")).toBe(false);
    expect(isValidDomain("-lead.com")).toBe(false);
    expect(isValidDomain("lead-.com")).toBe(false);
    expect(isValidDomain("a b.com")).toBe(false);
    expect(isValidDomain("")).toBe(false);
    expect(isValidDomain(`${"a".repeat(250)}.com`)).toBe(false);
  });

  test("isValidDnsServer:空 = 系统解析器;host 或 host:port", () => {
    expect(isValidDnsServer("")).toBe(true);
    expect(isValidDnsServer("8.8.8.8")).toBe(true);
    expect(isValidDnsServer("8.8.8.8:53")).toBe(true);
    expect(isValidDnsServer("dns.example.com")).toBe(true);
    expect(isValidDnsServer("8.8.8.8:99999")).toBe(false);
    expect(isValidDnsServer("8.8.8.8:abc")).toBe(false);
    expect(isValidDnsServer(":53")).toBe(false);
    expect(isValidDnsServer("a b")).toBe(false);
  });

  test("latencyBarPercent:相对最大延迟,非零至少 2%,封顶 100%", () => {
    expect(latencyBarPercent(50, 100)).toBe(50);
    expect(latencyBarPercent(1, 1000)).toBe(2);
    expect(latencyBarPercent(2000, 1000)).toBe(100);
    expect(latencyBarPercent(0, 100)).toBe(0);
    expect(latencyBarPercent(undefined, 100)).toBe(0);
    expect(latencyBarPercent(50, 0)).toBe(0);
  });
});

describe("DiagPage 组件", () => {
  test("初始渲染:内置目标、ICMP 说明与空态", async () => {
    renderPage();
    expect(screen.getByText("连通探测")).toBeInTheDocument();
    expect(
      screen.getByText("ICMP（ping）常被运营商屏蔽，本页采用 HTTP 探测评估连通性与延迟。"),
    ).toBeInTheDocument();
    for (const target of BUILTIN_PROBE_TARGETS) {
      expect(screen.getByRole("button", { name: `${target} 探测` })).toBeInTheDocument();
    }
    expect(screen.getByText("尚无探测结果，点击「探测」开始")).toBeInTheDocument();
    expect(screen.getByText("完成探测后在此显示各目标延迟对比")).toBeInTheDocument();
  });

  test("单目标探测:成功条目显示状态码与延迟横条", async () => {
    const user = userEvent.setup();
    diagDataMock.mockResolvedValue({
      ok: true,
      target: "http://223.5.5.5/",
      statusCode: 200,
      latencyMs: 23,
    });
    renderPage();
    await user.click(await screen.findByRole("button", { name: "223.5.5.5 探测" }));
    await waitFor(() =>
      expect(diagDataMock).toHaveBeenCalledWith("http_probe", { target: "223.5.5.5" }),
    );
    expect(await screen.findByText("可达")).toBeInTheDocument();
    expect(screen.getByText("HTTP 200")).toBeInTheDocument();
    expect(screen.getByText("23 ms")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "延迟 23 ms" })).toBeInTheDocument();
  });

  test("运行时失败(ok:false)渲染为失败条目,而非页面级错误", async () => {
    const user = userEvent.setup();
    diagDataMock.mockResolvedValue({
      ok: false,
      target: "http://223.5.5.5/",
      error: "探测超时(10s)",
      latencyMs: 10000,
    });
    renderPage();
    await user.click(await screen.findByRole("button", { name: "223.5.5.5 探测" }));
    expect(await screen.findByText("不可达")).toBeInTheDocument();
    expect(screen.getByText("探测超时(10s)")).toBeInTheDocument();
    // 失败条目不是页面级 ErrorRetry(role=alert)
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  test("自定义目标非法(ftp://)被前端拦截,不发起请求", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.type(await screen.findByLabelText("自定义目标"), "ftp://example.com");
    await user.click(screen.getByRole("button", { name: "探测" }));
    expect(diagDataMock).not.toHaveBeenCalled();
  });

  test("一键全测:串行执行,进度指示,首个未决时不发起第二个", async () => {
    const user = userEvent.setup();
    let resolveFirst: (value: DiagResponse) => void = () => {};
    const first = new Promise<DiagResponse>((resolve) => {
      resolveFirst = resolve;
    });
    diagDataMock
      .mockReturnValueOnce(first)
      .mockResolvedValueOnce({ ok: true, statusCode: 200, latencyMs: 12 })
      .mockResolvedValueOnce({ ok: true, statusCode: 200, latencyMs: 180 });
    renderPage();
    await user.click(await screen.findByRole("button", { name: "一键全测" }));

    await waitFor(() => expect(diagDataMock).toHaveBeenCalledTimes(1));
    expect(diagDataMock).toHaveBeenNthCalledWith(1, "http_probe", { target: "223.5.5.5" });
    expect(screen.getByText(/正在探测 1\/3/)).toBeInTheDocument();
    expect(screen.getByText("探测中…")).toBeInTheDocument();

    // 第一个目标未决时,绝不发起第二个(串行语义)
    await act(async () => {
      await Promise.resolve();
    });
    expect(diagDataMock).toHaveBeenCalledTimes(1);

    act(() => {
      resolveFirst({ ok: true, statusCode: 200, latencyMs: 30 });
    });
    // 第一个解决后,第二、三个按序发起(不断言中间总次数:微任务可能一次刷完)
    await waitFor(() =>
      expect(diagDataMock).toHaveBeenNthCalledWith(2, "http_probe", { target: "1.1.1.1" }),
    );
    await waitFor(() =>
      expect(diagDataMock).toHaveBeenNthCalledWith(3, "http_probe", { target: "8.8.8.8" }),
    );
    await waitFor(() => expect(diagDataMock).toHaveBeenCalledTimes(3));

    await waitFor(() => expect(screen.queryByText(/正在探测/)).not.toBeInTheDocument());
    expect(await screen.findAllByText("可达")).toHaveLength(3);
    expect(screen.getByText("30 ms")).toBeInTheDocument();
    expect(screen.getByText("180 ms")).toBeInTheDocument();
    // 延迟对比图接管本轮结果(jsdom 无 canvas → EChart 确定性降级)
    expect(document.querySelector("[data-chart-fallback]")).not.toBeNull();
  });

  test("DNS 查询:参数省略空 server,结果渲染地址列表/耗时/系统解析器", async () => {
    const user = userEvent.setup();
    diagDataMock.mockResolvedValue({
      ok: true,
      domain: "example.com",
      server: "",
      addresses: ["93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"],
      latencyMs: 15,
    });
    renderPage();
    await user.type(await screen.findByLabelText("域名"), "example.com");
    await user.click(screen.getByRole("button", { name: "查询" }));
    await waitFor(() =>
      expect(diagDataMock).toHaveBeenCalledWith("dns_query", { domain: "example.com" }),
    );
    expect(await screen.findByText("93.184.216.34")).toBeInTheDocument();
    expect(screen.getByText("2606:2800:220:1:248:1893:25c8:1946")).toBeInTheDocument();
    expect(screen.getByText("解析耗时 15 ms")).toBeInTheDocument();
    expect(screen.getByText("服务器：系统解析器")).toBeInTheDocument();
  });

  test("DNS 指定服务器时透传 server 参数;非法域名前端拦截", async () => {
    const user = userEvent.setup();
    renderPage();
    const domain = await screen.findByLabelText("域名");
    await user.type(domain, "bad..domain");
    await user.click(screen.getByRole("button", { name: "查询" }));
    expect(diagDataMock).not.toHaveBeenCalled();

    await user.clear(domain);
    await user.type(domain, "example.com");
    await user.type(screen.getByLabelText("DNS 服务器（可选）"), "223.5.5.5:53");
    diagDataMock.mockResolvedValue({
      ok: true,
      domain: "example.com",
      server: "223.5.5.5:53",
      addresses: ["1.2.3.4"],
      latencyMs: 8,
    });
    await user.click(screen.getByRole("button", { name: "查询" }));
    await waitFor(() =>
      expect(diagDataMock).toHaveBeenCalledWith("dns_query", {
        domain: "example.com",
        server: "223.5.5.5:53",
      }),
    );
    expect(await screen.findByText("1.2.3.4")).toBeInTheDocument();
    expect(screen.getByText("服务器：223.5.5.5:53")).toBeInTheDocument();
  });

  test("DNS 运行时失败:展示失败条目", async () => {
    const user = userEvent.setup();
    diagDataMock.mockResolvedValue({
      ok: false,
      domain: "example.com",
      error: "域名不存在: example.com",
      latencyMs: 40,
    });
    renderPage();
    await user.type(await screen.findByLabelText("域名"), "example.com");
    await user.click(screen.getByRole("button", { name: "查询" }));
    expect(await screen.findByText("解析失败：域名不存在: example.com")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
