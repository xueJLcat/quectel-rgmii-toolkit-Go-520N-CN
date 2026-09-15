import { render, screen } from "@testing-library/react";
import App from "@/app/App";

// 壳层冒烟:mock @/lib/api 隔离 WebSocket(细化断言见 src/app/shell.test.tsx)。
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
  dashboardData: vi.fn(() => Promise.resolve({})),
  moduleModel: vi.fn(() => Promise.resolve({ model: "RG520N-CN" })),
  languageSet: vi.fn(() => Promise.resolve({ language: "zh-CN" })),
  logout: vi.fn(() => Promise.resolve({ ok: true })),
}));

test("renders app shell", async () => {
  render(<App />);
  // 主导航(侧栏)挂载即壳层就绪;占位页/网关生命周期的细化断言在 app/shell.test.tsx
  expect(await screen.findByRole("navigation")).toBeInTheDocument();
});
