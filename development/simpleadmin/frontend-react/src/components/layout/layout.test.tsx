// layout 组件测试:Sidebar(六组/16 项/aria-current/折叠态/退出确认)、MobileDrawer(滑入复用导航/点击关闭)、
// Topbar(活力芯片成功与失败降级/刷新 invalidate/断线细条/语言切换)、Panel/PageHeader(域色契约)。
// vi.mock @/lib/api 隔离 WebSocket;motion 用真实时钟断言最终态(先例:common.test.tsx)。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { GaugeIcon } from "lucide-react";
import { MemoryRouter } from "react-router-dom";

import { ConfirmDialog } from "@/components/common";
import { MobileDrawer } from "@/components/layout/mobile-drawer";
import { PageHeader } from "@/components/layout/page-header";
import { Panel } from "@/components/layout/panel";
import { Sidebar } from "@/components/layout/sidebar";
import { Topbar } from "@/components/layout/topbar";
import { dashboardData, gateway, languageSet, logout } from "@/lib/api";
import { changeLanguage } from "@/lib/i18n";
import { useUiStore } from "@/stores/ui";

vi.mock("@/lib/api", () => ({
  gateway: {
    connect: vi.fn(() => Promise.resolve()),
    close: vi.fn(),
    startKeepalive: vi.fn(),
    stopKeepalive: vi.fn(),
    isConnected: vi.fn(() => true),
    getState: vi.fn(() => "connected"),
    onStateChange: vi.fn(() => () => {}),
    onServerEvent: vi.fn(() => () => {}),
  },
  dashboardData: vi.fn(() =>
    Promise.resolve({
      internetConnection: "已连接",
      network_provider: "中国移动",
      signalPercentage: 82,
    }),
  ),
  moduleModel: vi.fn(() => Promise.resolve({ model: "RG520N-CN" })),
  languageSet: vi.fn(() => Promise.resolve({ language: "en" })),
  logout: vi.fn(() => Promise.resolve({ ok: true })),
}));

// Radix 在 jsdom 缺失的指针/滚动 API 桩(先例:ui.test.tsx / common.test.tsx)。
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

afterEach(async () => {
  vi.clearAllMocks();
  vi.mocked(gateway.getState).mockReturnValue("connected");
  localStorage.clear();
  document.documentElement.removeAttribute("data-bs-theme");
  useUiStore.setState({ theme: "light", sidebarCollapsed: false, mobileNavOpen: false });
  await changeLanguage("zh-CN");
});

function createClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderSidebar(route = "/dashboard") {
  return render(
    <QueryClientProvider client={createClient()}>
      <MemoryRouter initialEntries={[route]}>
        <Sidebar />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function renderTopbar(client = createClient(), route = "/dashboard") {
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[route]}>
        <Topbar />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("Sidebar", () => {
  test("渲染六组分组标题与 16 个导航项", async () => {
    const { container } = renderSidebar();
    expect(await screen.findAllByRole("link")).toHaveLength(16);
    const titles = container.querySelectorAll('[data-slot="nav-group-title"]');
    expect(titles).toHaveLength(6);
    expect([...titles].map((el) => el.textContent)).toEqual([
      "监控",
      "网络",
      "安全",
      "通信",
      "工具",
      "系统",
    ]);
  });

  test("激活项带 aria-current=page,其余没有", async () => {
    renderSidebar("/sysmon");
    await screen.findAllByRole("link");
    expect(screen.getByRole("link", { name: "系统监控" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "总览" })).not.toHaveAttribute("aria-current");
  });

  test("折叠态:标题隐藏、图标保留、宽度切到折叠变量", async () => {
    const user = userEvent.setup();
    const { container } = renderSidebar();
    await screen.findAllByRole("link");
    await user.click(screen.getByRole("button", { name: "切换导航" }));
    expect(screen.queryByText("总览")).not.toBeInTheDocument();
    expect(container.querySelectorAll('[data-slot="nav-group-title"]')).toHaveLength(0);
    expect(screen.getAllByRole("link")).toHaveLength(16);
    expect(container.querySelectorAll("li svg")).toHaveLength(16);
    expect(container.querySelector('[data-slot="sidebar"]')).toHaveClass(
      "w-[var(--sa-sidebar-collapsed-width)]",
    );
  });

  test("退出登录经 ConfirmDialog 确认,取消不调 logout", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={createClient()}>
        <MemoryRouter initialEntries={["/dashboard"]}>
          <Sidebar />
          <ConfirmDialog />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await user.click(await screen.findByRole("button", { name: "退出登录" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("退出登录")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "取消" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(vi.mocked(logout)).not.toHaveBeenCalled();
  });
});

describe("MobileDrawer", () => {
  test("滑入复用 16 项导航,点击导航项后关闭", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={createClient()}>
        <MemoryRouter initialEntries={["/dashboard"]}>
          <MobileDrawer />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    act(() => useUiStore.getState().setMobileNavOpen(true));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getAllByRole("link")).toHaveLength(16);
    await user.click(within(dialog).getByRole("link", { name: "系统监控" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(useUiStore.getState().mobileNavOpen).toBe(false);
  });
});

describe("Topbar", () => {
  test("dashboardData 成功:联网 chip、运营商与信号 5 格出现", async () => {
    const { container } = renderTopbar();
    expect(await screen.findByText("已连接")).toBeInTheDocument();
    expect(screen.getByText("中国移动")).toBeInTheDocument();
    expect(container.querySelectorAll('[data-slot="signal-bar"]')).toHaveLength(5);
    // 82% → ceil(82/20) = 5 格填充
    expect(container.querySelectorAll('[data-slot="signal-bar"][data-filled="true"]')).toHaveLength(
      5,
    );
  });

  test("dashboardData 失败:芯片静默降级,顶栏本体不炸", async () => {
    vi.mocked(dashboardData).mockRejectedValueOnce(new Error("gateway down"));
    const { container } = renderTopbar();
    await waitFor(() => expect(dashboardData).toHaveBeenCalled());
    expect(screen.queryByText("已连接")).not.toBeInTheDocument();
    expect(container.querySelector('[data-slot="topbar-title"]')?.textContent).toBe("总览");
  });

  test("刷新钮触发全量 invalidateQueries", async () => {
    const client = createClient();
    const spy = vi.spyOn(client, "invalidateQueries");
    renderTopbar(client);
    const button = await screen.findByRole("button", { name: "刷新" });
    fireEvent.click(button);
    expect(spy).toHaveBeenCalled();
  });

  test("网关断线:细条提示出现且芯片查询不发起", async () => {
    vi.mocked(gateway.getState).mockReturnValue("disconnected");
    renderTopbar();
    expect(await screen.findByRole("status")).toHaveTextContent("网络已断开");
    expect(dashboardData).not.toHaveBeenCalled();
  });

  test("语言切换:changeLanguage + 服务端 languageSet 持久化", async () => {
    const user = userEvent.setup();
    renderTopbar();
    await user.click(await screen.findByRole("button", { name: "中" }));
    await waitFor(() => expect(vi.mocked(languageSet)).toHaveBeenCalledWith("en"));
    expect(await screen.findByRole("button", { name: "EN" })).toBeInTheDocument();
  });
});

describe("Panel", () => {
  test("域色头部:图标 chip/标题/工具区/2px 渐变条/footer", () => {
    const { container } = render(
      <Panel
        domain="network"
        title="蜂窝网络"
        icon={GaugeIcon}
        tools={<button type="button">扫描</button>}
        footer="脚注说明"
      >
        内容
      </Panel>,
    );
    const panel = container.querySelector('[data-slot="panel"]');
    expect(panel).toHaveAttribute("data-domain", "network");
    expect(panel?.getAttribute("style")).toContain("var(--sa-grad-network)");
    expect(screen.getByRole("heading", { name: "蜂窝网络" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "扫描" })).toBeInTheDocument();
    expect(screen.getByText("脚注说明")).toBeInTheDocument();
    const bar = container.querySelector('[data-slot="panel-header-bar"]');
    expect(bar).not.toBeNull();
    expect(bar).toHaveClass("h-0.5");
  });

  test("danger 变体头部 danger-soft 底;无头部内容时不渲染 header", () => {
    render(
      <Panel domain="system" danger title="危险区">
        x
      </Panel>,
    );
    expect(screen.getByText("危险区").closest("header")).toHaveClass("bg-danger-soft");
    const bare = render(<Panel>正文</Panel>);
    expect(bare.container.querySelector('[data-slot="panel-header"]')).toBeNull();
  });

  test("interactive:hover 上浮类 + --panel-glow 注入域色", () => {
    const { container } = render(
      <Panel domain="tools" interactive>
        x
      </Panel>,
    );
    const panel = container.querySelector('[data-slot="panel"]');
    expect(panel).toHaveClass("hover:-translate-y-0.5");
    expect(panel?.getAttribute("style")).toContain("var(--sa-domain-tools)");
  });

  test("根为 flex-col + h-full,身体 flex-1:stretch 网格中填满单元格、同行卡片等高", () => {
    const { container } = render(
      <Panel title="等高卡片" footer="脚注说明">
        内容
      </Panel>,
    );
    const panel = container.querySelector('[data-slot="panel"]');
    expect(panel).toHaveClass("h-full", "flex-col");
    expect(panel?.querySelector(".p-4")).toHaveClass("flex-1");
  });
});

describe("PageHeader", () => {
  test("标题 h1、描述、操作区与 4px 域色渐变竖条", () => {
    const { container } = render(
      <PageHeader
        domain="security"
        title="防火墙"
        description="描述文本"
        actions={<button type="button">导出</button>}
      />,
    );
    expect(screen.getByRole("heading", { level: 1, name: "防火墙" })).toBeInTheDocument();
    expect(screen.getByText("描述文本")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "导出" })).toBeInTheDocument();
    const bar = container.querySelector('[data-slot="page-header"] > span');
    expect(bar?.getAttribute("style")).toContain("var(--sa-grad-security)");
    expect(bar).toHaveClass("w-1");
  });
});
