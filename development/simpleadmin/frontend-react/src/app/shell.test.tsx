// 壳层测试:routes 导航契约(16 路由/6 分组/nav ns 词条/懒加载) + App 冒烟
// (vi.mock @/lib/api 隔离 WebSocket:gateway 为 spy,dashboardData 固定数据,logout spy)。
// motion 用真实时钟,断言最终态(先例:common.test.tsx)。
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import App from "@/app/App";
import { NAV_GROUPS, PAGES } from "@/app/routes";
import { gateway } from "@/lib/api";
import navZh from "@/lib/i18n/locales/zh-CN/nav.json";
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

// 冒烟只测壳层(路由/标题/生命周期),页面本体打桩,与 feature 演进解耦
vi.mock("@/features/dashboard/page", () => ({
  default: () => <div>dashboard-page-stub</div>,
}));
vi.mock("@/features/sysmon/page", () => ({
  default: () => <div>sysmon-page-stub</div>,
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

afterEach(() => {
  vi.clearAllMocks();
  vi.mocked(gateway.getState).mockReturnValue("connected");
  localStorage.clear();
  document.documentElement.removeAttribute("data-bs-theme");
  document.title = "";
  useUiStore.setState({ theme: "light", sidebarCollapsed: false, mobileNavOpen: false });
});

describe("routes 契约", () => {
  test("16 页,id/path 唯一且 path = /<id>", () => {
    expect(PAGES).toHaveLength(16);
    expect(new Set(PAGES.map((page) => page.id)).size).toBe(16);
    expect(new Set(PAGES.map((page) => page.path)).size).toBe(16);
    for (const page of PAGES) {
      expect(page.path, `${page.id} 的 path 必须为 /${page.id}`).toBe(`/${page.id}`);
    }
  });

  test("6 组顺序固定,groupId 全部合法", () => {
    expect(NAV_GROUPS.map((group) => group.id)).toEqual([
      "monitor",
      "network",
      "security",
      "comm",
      "tools",
      "system",
    ]);
    const groupIds = new Set(NAV_GROUPS.map((group) => group.id));
    for (const page of PAGES) {
      expect(groupIds.has(page.groupId), `${page.id} 的 groupId 非法`).toBe(true);
    }
  });

  test("titleKey 全部存在于 nav ns(zh-CN)", () => {
    for (const page of PAGES) {
      expect(navZh, `nav ns 缺少页面词条 ${page.titleKey}`).toHaveProperty(page.titleKey);
    }
    for (const group of NAV_GROUPS) {
      expect(navZh, `nav ns 缺少分组词条 ${group.titleKey}`).toHaveProperty(group.titleKey);
    }
  });

  test("组内页面与 IA 清单(设计方案 §4.2)一致", () => {
    const idsByGroup = Object.fromEntries(
      NAV_GROUPS.map((group) => [
        group.id,
        PAGES.filter((page) => page.groupId === group.id).map((page) => page.id),
      ]),
    );
    expect(idsByGroup).toEqual({
      monitor: ["dashboard", "sysmon"],
      network: ["signal", "network", "celllock", "netdetail", "netconfig"],
      security: ["firewall"],
      comm: ["sms", "smsforward"],
      tools: ["atcommands", "console", "diag"],
      system: ["automation", "settings", "deviceinfo"],
    });
  });

  test("lazy 均指向 @/features/<id>/page 且可加载 default 导出", async () => {
    for (const page of PAGES) {
      const mod = await page.lazy();
      expect(typeof mod.default, `${page.id} 页面缺少 default 导出`).toBe("function");
    }
  });
});

describe("App 冒烟", () => {
  test("初始兜底到 #/dashboard,渲染页面并启动网关生命周期", async () => {
    render(<App />);
    expect(window.location.hash).toBe("#/dashboard");
    expect(await screen.findByText("dashboard-page-stub")).toBeInTheDocument();
    // Topbar 活力芯片(dashboardData mock 成功)
    expect(await screen.findByText("已连接")).toBeInTheDocument();
    expect(vi.mocked(gateway.connect)).toHaveBeenCalled();
    expect(vi.mocked(gateway.startKeepalive)).toHaveBeenCalled();
    // document.title = "页面标题 - 型号"(对齐旧版 Brand.setPageTitle)
    await waitFor(() => expect(document.title).toBe("总览 - RG520N-CN"));
  });

  test("卸载时关闭网关并停止 keepalive", async () => {
    const { unmount } = render(<App />);
    await screen.findByText("dashboard-page-stub");
    unmount();
    expect(vi.mocked(gateway.stopKeepalive)).toHaveBeenCalled();
    expect(vi.mocked(gateway.close)).toHaveBeenCalled();
  });

  test("导航到 #sysmon:hash、Topbar 标题与 document.title 随动", async () => {
    const user = userEvent.setup();
    const { container } = render(<App />);
    await screen.findByText("dashboard-page-stub");
    await user.click(screen.getByRole("link", { name: "系统监控" }));
    await waitFor(() => expect(window.location.hash).toBe("#/sysmon"), { timeout: 3000 });
    await waitFor(() => expect(document.title).toBe("系统监控 - RG520N-CN"), { timeout: 3000 });
    await waitFor(
      () =>
        expect(container.querySelector('[data-slot="topbar-title"]')?.textContent).toBe("系统监控"),
      { timeout: 3000 },
    );
    // mode="wait":任一时刻只有一页在场,sysmon 页最终挂载
    expect(await screen.findByText("sysmon-page-stub")).toBeInTheDocument();
  });
});
