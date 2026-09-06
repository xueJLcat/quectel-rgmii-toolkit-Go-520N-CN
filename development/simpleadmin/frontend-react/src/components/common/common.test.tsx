// common 组件 + stores + lib/theme 单测(vitest globals + jsdom + jest-dom)。
// 语义断言对齐设计方案 §3/§5 与 Vue 版行为参考(simpleadmin-ui.js / simpleadmin-reboot.js)。
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { InboxIcon } from "lucide-react";
import { toast } from "sonner";

import {
  ConfirmDialog,
  CopyButton,
  DangerZone,
  EmptyState,
  ErrorRetry,
  MetricCard,
  RebootOverlay,
  StatusChip,
} from "./index";
import { applyTheme, readCurrentTheme, readStoredTheme, storeTheme } from "@/lib/theme";
import { REBOOT_DEFAULT_SECONDS, useRebootStore } from "@/stores/reboot";
import { useConfirmStore } from "@/stores/confirm";
import { useUiStore } from "@/stores/ui";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Radix 在 jsdom 缺失的指针/滚动 API 桩(与 ui.test.tsx 相同)。
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
  vi.useRealTimers();
  localStorage.clear();
  document.documentElement.removeAttribute("data-bs-theme");
  document.documentElement.style.colorScheme = "";
  useUiStore.setState({ theme: "light", sidebarCollapsed: false, mobileNavOpen: false });
  useConfirmStore.setState({ options: null, resolve: null });
  useRebootStore.setState({
    countdownActive: false,
    endsAt: 0,
    total: REBOOT_DEFAULT_SECONDS,
    label: null,
  });
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
});

describe("lib/theme", () => {
  test("applyTheme 双写 data-bs-theme + colorScheme;readCurrentTheme 缺省 light", () => {
    expect(readCurrentTheme()).toBe("light");
    applyTheme("dark");
    expect(document.documentElement.getAttribute("data-bs-theme")).toBe("dark");
    expect(document.documentElement.style.colorScheme).toBe("dark");
    expect(readCurrentTheme()).toBe("dark");
    applyTheme("light");
    expect(document.documentElement.getAttribute("data-bs-theme")).toBe("light");
    expect(readCurrentTheme()).toBe("light");
  });

  test("storeTheme/readStoredTheme 走 localStorage 键 theme,非法值归 null", () => {
    expect(readStoredTheme()).toBeNull();
    storeTheme("dark");
    expect(localStorage.getItem("theme")).toBe("dark");
    expect(readStoredTheme()).toBe("dark");
    localStorage.setItem("theme", "neon");
    expect(readStoredTheme()).toBeNull();
  });
});

describe("stores/ui", () => {
  test("toggleTheme 翻转 data-bs-theme 与 localStorage", () => {
    useUiStore.getState().toggleTheme();
    expect(useUiStore.getState().theme).toBe("dark");
    expect(document.documentElement.getAttribute("data-bs-theme")).toBe("dark");
    expect(document.documentElement.style.colorScheme).toBe("dark");
    expect(localStorage.getItem("theme")).toBe("dark");
    useUiStore.getState().toggleTheme();
    expect(useUiStore.getState().theme).toBe("light");
    expect(document.documentElement.getAttribute("data-bs-theme")).toBe("light");
    expect(localStorage.getItem("theme")).toBe("light");
  });

  test("setTheme 应用指定模式;sidebar/mobileNav 状态独立翻转", () => {
    useUiStore.getState().setTheme("dark");
    expect(useUiStore.getState().theme).toBe("dark");
    expect(document.documentElement.getAttribute("data-bs-theme")).toBe("dark");
    expect(useUiStore.getState().sidebarCollapsed).toBe(false);
    useUiStore.getState().toggleSidebar();
    expect(useUiStore.getState().sidebarCollapsed).toBe(true);
    useUiStore.getState().setMobileNavOpen(true);
    expect(useUiStore.getState().mobileNavOpen).toBe(true);
    useUiStore.getState().setMobileNavOpen(false);
    expect(useUiStore.getState().mobileNavOpen).toBe(false);
  });
});

describe("StatusChip", () => {
  test("tone 映射 *-soft 底 + 语义文字类名", () => {
    render(
      <>
        <StatusChip tone="success">在线</StatusChip>
        <StatusChip tone="warning">降级</StatusChip>
        <StatusChip tone="danger">离线</StatusChip>
        <StatusChip tone="info">同步中</StatusChip>
        <StatusChip tone="accent">已激活</StatusChip>
        <StatusChip tone="muted">未知</StatusChip>
      </>,
    );
    expect(screen.getByText("在线")).toHaveClass("bg-success-soft", "text-success", "rounded-sm");
    expect(screen.getByText("降级")).toHaveClass("bg-warning-soft", "text-warning");
    expect(screen.getByText("离线")).toHaveClass("bg-danger-soft", "text-danger");
    expect(screen.getByText("同步中")).toHaveClass("bg-info-soft", "text-info");
    expect(screen.getByText("已激活")).toHaveClass("bg-accent-soft", "text-accent");
    expect(screen.getByText("未知")).toHaveClass("bg-surface-3", "text-muted");
  });

  test("pulse 时圆点叠加 animate-ping 光晕", () => {
    const { container } = render(<StatusChip tone="success">心跳</StatusChip>);
    expect(container.querySelector(".animate-ping")).toBeNull();
    const pulsing = render(
      <StatusChip tone="success" pulse>
        心跳
      </StatusChip>,
    );
    expect(pulsing.container.querySelector(".animate-ping")).not.toBeNull();
  });
});

describe("ConfirmDialog", () => {
  test("confirm() 挂起 promise;点确认 resolve(true),store 复位", async () => {
    const user = userEvent.setup();
    render(<ConfirmDialog />);
    let pending!: Promise<boolean>;
    act(() => {
      pending = useConfirmStore.getState().confirm({
        title: "重启设备?",
        message: "这将重启整个设备。",
      });
    });
    // 挂起中:未结算前 race 到哨兵值
    expect(await Promise.race([pending, Promise.resolve("PENDING")])).toBe("PENDING");

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("重启设备?")).toBeInTheDocument();
    expect(screen.getByText("这将重启整个设备。")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "确认" }));
    await expect(pending).resolves.toBe(true);
    expect(useConfirmStore.getState().options).toBeNull();
    expect(useConfirmStore.getState().resolve).toBeNull();
  });

  test("点取消与按 ESC 均 resolve(false)", async () => {
    const user = userEvent.setup();
    render(<ConfirmDialog />);

    let pending!: Promise<boolean>;
    act(() => {
      pending = useConfirmStore.getState().confirm({ title: "清空短信?" });
    });
    await screen.findByRole("dialog");
    await user.click(screen.getByRole("button", { name: "取消" }));
    await expect(pending).resolves.toBe(false);

    act(() => {
      pending = useConfirmStore.getState().confirm({ title: "清空全部短信?" });
    });
    await screen.findByRole("dialog");
    expect(screen.getByText("清空全部短信?")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    await expect(pending).resolves.toBe(false);
  });

  test("danger 时确认按钮为 danger 变体;自定义按钮文案生效", async () => {
    render(<ConfirmDialog />);
    act(() => {
      void useConfirmStore
        .getState()
        .confirm({ title: "恢复出厂", danger: true, confirmText: "恢复", cancelText: "放弃" });
    });
    await screen.findByRole("dialog");
    expect(screen.getByRole("button", { name: "恢复" })).toHaveClass("bg-danger", "text-white");
    expect(screen.getByRole("button", { name: "放弃" })).toHaveClass("bg-surface");
  });

  test("requireWord 输错禁用确认钮,输对才可确认", async () => {
    const user = userEvent.setup();
    render(<ConfirmDialog />);
    let pending!: Promise<boolean>;
    act(() => {
      pending = useConfirmStore.getState().confirm({
        title: "关闭模块电源",
        danger: true,
        requireWord: "SHUTDOWN",
      });
    });
    await screen.findByRole("dialog");
    const ok = screen.getByRole("button", { name: "确认" });
    expect(ok).toBeDisabled();

    const input = screen.getByPlaceholderText("SHUTDOWN");
    await user.type(input, "shutdown");
    expect(ok).toBeDisabled();
    expect(input).toHaveAttribute("aria-invalid", "true");
    await user.clear(input);
    await user.type(input, "SHUTDOWN");
    expect(ok).toBeEnabled();
    expect(input).not.toHaveAttribute("aria-invalid");
    await user.click(ok);
    await expect(pending).resolves.toBe(true);
  });
});

describe("RebootOverlay", () => {
  function remainingEl(): Element | null {
    return document.querySelector('[data-slot="reboot-remaining"]');
  }

  // motion 的 frameloop 在模块加载时即捕获真实 rAF(motion-dom frameloop/frame.mjs),
  // 假定时器推不动其退场帧:倒计时数学用 fake timers 驱动,退场卸载切回真实时钟 waitFor。

  test("startCountdown 后遮罩出现,推进 3s 剩余秒数按真实时间戳递减,closeCountdown 消失", async () => {
    vi.useFakeTimers();
    render(<RebootOverlay />);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();

    act(() => {
      useRebootStore.getState().startCountdown({ seconds: 40 });
    });
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    expect(remainingEl()).toHaveTextContent("40");
    expect(screen.getByText("重启中…请稍候,请勿关闭页面")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(remainingEl()).toHaveTextContent("37");

    act(() => {
      useRebootStore.getState().closeCountdown();
    });
    vi.useRealTimers();
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  });

  test("倒计时到 0 进入完成态:成功 chip + 关闭按钮", async () => {
    vi.useFakeTimers();
    render(<RebootOverlay />);
    act(() => {
      useRebootStore.getState().startCountdown({ seconds: 2 });
    });
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(remainingEl()).toHaveTextContent("0");
    expect(screen.getByText("重启倒计时结束")).toHaveClass("bg-success-soft", "text-success");
    vi.useRealTimers();
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  });

  test("endsAtServerMs 采用服务端绝对时间戳;seconds 非法回退默认 40s", () => {
    vi.useFakeTimers();
    const endsAt = Date.now() + 10_000;
    render(<RebootOverlay />);
    act(() => {
      useRebootStore.getState().startCountdown({ seconds: 40, endsAtServerMs: endsAt });
    });
    expect(useRebootStore.getState().endsAt).toBe(endsAt);
    expect(remainingEl()).toHaveTextContent("10");

    act(() => {
      useRebootStore.getState().startCountdown({ seconds: Number.NaN, label: "网口重启中" });
    });
    expect(useRebootStore.getState().total).toBe(REBOOT_DEFAULT_SECONDS);
    expect(screen.getByText("网口重启中")).toBeInTheDocument();
  });
});

describe("MetricCard", () => {
  test("值变化 count-up 600ms,终值文本正确;countUp=false 立即显示", () => {
    vi.useFakeTimers();
    const view = render(<MetricCard label="CPU 使用率" value={0} unit="%" />);
    expect(screen.getByText("0")).toBeInTheDocument();

    act(() => {
      view.rerender(<MetricCard label="CPU 使用率" value={87} unit="%" />);
    });
    act(() => {
      vi.advanceTimersByTime(700); // 600ms 动画 + 帧余量
    });
    expect(screen.getByText("87")).toBeInTheDocument();

    act(() => {
      view.rerender(<MetricCard label="CPU 使用率" value={42} unit="%" countUp={false} />);
    });
    expect(screen.getByText("42")).toBeInTheDocument();
  });

  test("decimals 控制小数位,缺省取目标值小数位;字符串值原样渲染", () => {
    vi.useFakeTimers();
    const view = render(<MetricCard label="温度" value={41.25} decimals={1} />);
    expect(screen.getByText("41.3")).toBeInTheDocument(); // (41.25).toFixed(1) 平票向上
    act(() => {
      view.rerender(<MetricCard label="下载速率" value={3.5} />);
    });
    act(() => {
      vi.advanceTimersByTime(700);
    });
    expect(screen.getByText("3.5")).toBeInTheDocument();
    render(<MetricCard label="运营商" value="中国移动" />);
    expect(screen.getByText("中国移动")).toBeInTheDocument();
  });

  test("icon 置于域色渐变圆角方块;卡片 hover 上浮", () => {
    const { container } = render(
      <MetricCard label="短信" value={3} icon={InboxIcon} domain="comm" />,
    );
    const chip = container.querySelector<HTMLElement>('span[aria-hidden="true"]');
    expect(chip).not.toBeNull();
    expect(chip?.style.backgroundImage).toBe("var(--sa-grad-comm)");
    expect(chip).toHaveClass("rounded-sm", "text-white");
    expect(container.querySelector("svg.lucide-inbox")).not.toBeNull();
    expect(screen.getByText("短信").closest('[data-slot="card"]')).toHaveClass(
      "hover:-translate-y-0.5",
    );
  });
});

describe("CopyButton", () => {
  test("clipboard 可用:点击后 toast.success 触发 + check 图标出现", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    render(<CopyButton text="192.168.1.1" label="复制" />);
    expect(document.querySelector("svg.lucide-copy")).not.toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "复制" }));
    });
    expect(toast.success).toHaveBeenCalledTimes(1);
    expect(writeText).toHaveBeenCalledWith("192.168.1.1");
    expect(document.querySelector("svg.lucide-check")).not.toBeNull();
  });

  test("clipboard 不可用时降级 textarea + execCommand", async () => {
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
    const exec = vi.fn().mockReturnValue(true);
    document.execCommand = exec;
    render(<CopyButton text="868123456789012" />);
    // common.copy 词条已就位:无 label 时 aria-label 为译文"复制"
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "复制" }));
    });
    expect(exec).toHaveBeenCalledWith("copy");
    expect(toast.success).toHaveBeenCalledTimes(1);
  });

  test("复制失败 toast.error", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const exec = vi.fn().mockReturnValue(false);
    document.execCommand = exec;
    render(<CopyButton text="x" />);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "复制" }));
    });
    expect(toast.error).toHaveBeenCalledTimes(1);
    expect(document.querySelector("svg.lucide-check")).toBeNull();
  });
});

describe("ErrorRetry", () => {
  test("danger-soft 卡片 + 点击重试回调", () => {
    const onRetry = vi.fn();
    render(<ErrorRetry message="状态获取失败" onRetry={onRetry} />);
    const card = screen.getByRole("alert");
    expect(card).toHaveClass("bg-danger-soft");
    expect(screen.getByText("状态获取失败")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  test("retrying 时按钮禁用并显示 spinner", () => {
    render(<ErrorRetry message="数据更新失败" onRetry={() => {}} retrying />);
    const button = screen.getByRole("button", { name: "重试" });
    expect(button).toBeDisabled();
    expect(button.querySelector(".animate-spin")).not.toBeNull();
  });
});

describe("EmptyState / DangerZone", () => {
  test("EmptyState 居中标题/描述/action 插槽", () => {
    render(
      <EmptyState
        icon={InboxIcon}
        title="暂无短信"
        description="收件箱为空"
        action={<button>刷新</button>}
      />,
    );
    expect(screen.getByText("暂无短信")).toBeInTheDocument();
    expect(screen.getByText("收件箱为空")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "刷新" })).toBeInTheDocument();
    expect(document.querySelector("svg.lucide-inbox")).not.toBeNull();
  });

  test("DangerZone rose 边框卡:danger 边框 + danger-soft header + children", () => {
    render(
      <DangerZone title="危险操作" description="以下操作不可撤销">
        <button>关机</button>
      </DangerZone>,
    );
    const zone = screen.getByText("危险操作").closest("section");
    expect(zone).toHaveClass("border-danger/40");
    expect(screen.getByText("危险操作").closest("header")).toHaveClass("bg-danger-soft");
    expect(screen.getByRole("button", { name: "关机" })).toBeInTheDocument();
  });
});
