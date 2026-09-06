// automation 测试:纯函数范围校验矩阵(1-60/1-1440/1-30、HH:MM、NTP 服务器、探测目标行)
// + 组件行为(三卡渲染、看门狗保存参数含 checkInterval 命名、Switch 即时生效仅提交 enabled、
// 非法值前端拦截不提交、保存后回读)。vi.mock @/lib/api 隔离 WS 网关。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import "@/lib/i18n";
import AutomationPage from "@/features/automation/page";
import {
  schedulerGet,
  schedulerSet,
  timesyncGet,
  timesyncNow,
  timesyncSet,
  watchdogGet,
  watchdogSet,
} from "@/lib/api";

import {
  MAX_WATCHDOG_TARGETS,
  buildSchedulerPayload,
  buildTimeSyncPayload,
  buildWatchdogPayload,
  isIntegerInRange,
  isValidNtpServer,
  isValidRebootTime,
  isValidTargetLine,
  parseTargetLines,
  validateTargets,
  validateTimeSyncForm,
  validateWatchdogForm,
  watchdogActionLabelKey,
} from "./lib";
import type { WatchdogForm } from "./lib";

vi.mock("@/lib/api", () => ({
  watchdogGet: vi.fn(),
  watchdogSet: vi.fn(),
  schedulerGet: vi.fn(),
  schedulerSet: vi.fn(),
  timesyncGet: vi.fn(),
  timesyncSet: vi.fn(),
  timesyncNow: vi.fn(),
}));

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

const WATCHDOG_DATA = {
  enabled: true,
  failThreshold: 5,
  cooldownMinutes: 30,
  checkIntervalMinutes: 3,
  targets: [],
  defaultTargets: ["223.5.5.5:53", "1.1.1.1:53", "8.8.8.8:53"],
  actionPolicy: "reboot",
  consecutiveFailures: 2,
  lastActionTime: "2026-09-06 10:00:00",
  lastActionType: "radio",
  pollerRunning: true,
};

const SCHEDULER_DATA = { rebootEnabled: false, rebootTime: "04:00", lastRebootDate: "" };

const TIMESYNC_DATA = {
  enabled: false,
  intervalMinutes: 60,
  server: "ntp.aliyun.com",
  defaultServer: "ntp.aliyun.com",
  pollerRunning: false,
  syncing: false,
  lastSyncTime: "",
  lastSyncOK: false,
  lastError: "",
  lastOffsetMs: 0,
  lastServer: "",
  systemTime: "2026-09-06 12:00:00",
};

function renderPage(): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <AutomationPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(watchdogGet).mockResolvedValue(WATCHDOG_DATA);
  vi.mocked(watchdogSet).mockResolvedValue({ ok: true });
  vi.mocked(schedulerGet).mockResolvedValue(SCHEDULER_DATA);
  vi.mocked(schedulerSet).mockResolvedValue({ ok: true });
  vi.mocked(timesyncGet).mockResolvedValue(TIMESYNC_DATA);
  vi.mocked(timesyncSet).mockResolvedValue({ ok: true });
  vi.mocked(timesyncNow).mockResolvedValue({ ok: true, offsetMs: -35 });
});

const VALID_FORM: WatchdogForm = {
  enabled: true,
  failThreshold: "5",
  cooldownMinutes: "30",
  checkIntervalMinutes: "3",
  targets: "",
  actionPolicy: "reboot",
};

describe("范围校验纯函数", () => {
  test("isIntegerInRange:整数 + 闭区间", () => {
    expect(isIntegerInRange("7", 1, 60)).toBe(true);
    expect(isIntegerInRange(60, 1, 60)).toBe(true);
    expect(isIntegerInRange("0", 1, 60)).toBe(false);
    expect(isIntegerInRange("61", 1, 60)).toBe(false);
    expect(isIntegerInRange("7.5", 1, 60)).toBe(false);
    expect(isIntegerInRange("", 1, 60)).toBe(false);
    expect(isIntegerInRange("abc", 1, 60)).toBe(false);
  });

  test("isValidRebootTime:严格 HH:MM", () => {
    expect(isValidRebootTime("04:00")).toBe(true);
    expect(isValidRebootTime("23:59")).toBe(true);
    expect(isValidRebootTime("00:00")).toBe(true);
    expect(isValidRebootTime("24:00")).toBe(false);
    expect(isValidRebootTime("4:00")).toBe(false);
    expect(isValidRebootTime("04:60")).toBe(false);
    expect(isValidRebootTime("aa:bb")).toBe(false);
    expect(isValidRebootTime("")).toBe(false);
  });

  test("isValidNtpServer:空值合法,镜像后端 normalizeTimeSyncServer 规则", () => {
    expect(isValidNtpServer("")).toBe(true);
    expect(isValidNtpServer("ntp.aliyun.com")).toBe(true);
    expect(isValidNtpServer("ntp://pool.ntp.org")).toBe(true);
    expect(isValidNtpServer("ntps://pool.ntp.org")).toBe(true);
    expect(isValidNtpServer("a b")).toBe(false);
    expect(isValidNtpServer("host/path")).toBe(false);
    expect(isValidNtpServer("host:123")).toBe(false);
    expect(isValidNtpServer("host#frag")).toBe(false);
    expect(isValidNtpServer("x".repeat(254))).toBe(false);
    expect(isValidNtpServer("x".repeat(253))).toBe(true);
  });

  test("isValidTargetLine:host[:port] / [IPv6]:port / IPv6 字面量", () => {
    expect(isValidTargetLine("223.5.5.5")).toBe(true);
    expect(isValidTargetLine("223.5.5.5:53")).toBe(true);
    expect(isValidTargetLine("example.com:5353")).toBe(true);
    expect(isValidTargetLine("[2001:db8::1]:53")).toBe(true);
    expect(isValidTargetLine("2001:db8::1")).toBe(true);
    expect(isValidTargetLine("host:99999")).toBe(false);
    expect(isValidTargetLine("host:port")).toBe(false);
    expect(isValidTargetLine("[2001:db8::1]")).toBe(false);
    expect(isValidTargetLine("a b")).toBe(false);
    expect(isValidTargetLine("")).toBe(false);
  });

  test("validateTargets:≤4 行,非法行返回行内容", () => {
    expect(validateTargets("")).toBeNull();
    expect(validateTargets("223.5.5.5\n1.1.1.1:53")).toBeNull();
    expect(validateTargets("a\nb\nc\nd\ne")).toEqual({ reason: "tooMany" });
    expect(validateTargets("ok\nbad line")).toEqual({ reason: "invalidLine", line: "bad line" });
    expect(parseTargetLines(" a \n\n b \n")).toEqual(["a", "b"]);
    expect(MAX_WATCHDOG_TARGETS).toBe(4);
  });

  test("validateWatchdogForm:字段级错误码", () => {
    expect(validateWatchdogForm(VALID_FORM)).toBeNull();
    expect(validateWatchdogForm({ ...VALID_FORM, failThreshold: "61" })).toBe("failThreshold");
    expect(validateWatchdogForm({ ...VALID_FORM, cooldownMinutes: "0" })).toBe("cooldownMinutes");
    expect(validateWatchdogForm({ ...VALID_FORM, checkIntervalMinutes: "31" })).toBe(
      "checkIntervalMinutes",
    );
    expect(validateWatchdogForm({ ...VALID_FORM, targets: "a\nb\nc\nd\ne" })).toBe(
      "targetsTooMany",
    );
    expect(validateWatchdogForm({ ...VALID_FORM, targets: "bad line" })).toBe("targetLineInvalid");
  });

  test("payload 构造:set 契约参数名(checkInterval)与字符串化", () => {
    expect(buildWatchdogPayload({ ...VALID_FORM, failThreshold: "7" })).toEqual({
      enabled: "1",
      failThreshold: "7",
      cooldownMinutes: "30",
      checkInterval: "3",
      targets: "",
      actionPolicy: "reboot",
    });
    expect(
      buildWatchdogPayload({ ...VALID_FORM, enabled: false, actionPolicy: "escalate" }),
    ).toEqual(expect.objectContaining({ enabled: "0", actionPolicy: "escalate" }));
    expect(buildSchedulerPayload(true, " 04:30 ")).toEqual({
      rebootEnabled: "1",
      rebootTime: "04:30",
    });
    expect(buildTimeSyncPayload(false, "60", " ntp.aliyun.com ")).toEqual({
      enabled: "0",
      intervalMinutes: "60",
      server: "ntp.aliyun.com",
    });
    expect(validateTimeSyncForm("1441", "")).toBe("intervalMinutes");
    expect(validateTimeSyncForm("60", "bad server")).toBe("server");
    expect(validateTimeSyncForm("60", "")).toBeNull();
    expect(watchdogActionLabelKey("radio")).toBe("radioReRegistration");
    expect(watchdogActionLabelKey("reboot")).toBe("moduleReboot");
    expect(watchdogActionLabelKey("")).toBeNull();
  });
});

describe("AutomationPage 组件", () => {
  test("三卡渲染:标题与运行状态行", async () => {
    renderPage();
    expect(await screen.findByText("断网自愈看门狗")).toBeInTheDocument();
    expect(screen.getByText("每日定时重启")).toBeInTheDocument();
    expect(screen.getByText("时间同步")).toBeInTheDocument();
    expect(await screen.findByText("连续失败: 2 / 5")).toBeInTheDocument();
    expect(screen.getByText("上次自愈: 2026-09-06 10:00:00 · 无线电重注册")).toBeInTheDocument();
    expect(screen.getByText("状态: 轮询中")).toBeInTheDocument();
    expect(screen.getByText("系统时间: 2026-09-06 12:00:00")).toBeInTheDocument();
    expect(screen.getByText("上次重启日期: 无")).toBeInTheDocument();
  });

  test("看门狗保存:watchdogSet 收到旧契约参数并在成功后回读", async () => {
    const user = userEvent.setup();
    renderPage();
    const threshold = await screen.findByLabelText("连续失败次数");
    await user.clear(threshold);
    await user.type(threshold, "7");
    await user.click(screen.getByRole("button", { name: "保存看门狗设置" }));
    await waitFor(() =>
      expect(watchdogSet).toHaveBeenCalledWith({
        enabled: "1",
        failThreshold: "7",
        cooldownMinutes: "30",
        checkInterval: "3",
        targets: "",
        actionPolicy: "reboot",
      }),
    );
    // 保存后回读(get/set 契约)
    await waitFor(() => expect(watchdogGet).toHaveBeenCalledTimes(2));
  });

  test("Switch 即时生效:切换看门狗只提交 enabled", async () => {
    const user = userEvent.setup();
    renderPage();
    const toggle = await screen.findByRole("switch", { name: "启用看门狗" });
    // 查询数据到达后 Switch 才反映服务端 enabled 状态
    await waitFor(() => expect(toggle).toHaveAttribute("aria-checked", "true"));
    await user.click(toggle);
    await waitFor(() => expect(watchdogSet).toHaveBeenCalledWith({ enabled: "0" }));
  });

  test("范围校验前端拦截:间隔 99 / 时刻 25:99 / NTP 含空格 均不提交", async () => {
    const user = userEvent.setup();
    renderPage();
    const interval = await screen.findByLabelText("检测间隔（分钟）");
    await user.clear(interval);
    await user.type(interval, "99");
    await user.click(screen.getByRole("button", { name: "保存看门狗设置" }));
    expect(watchdogSet).not.toHaveBeenCalled();

    // type=time 在 jsdom 中非法值会被清洗为空串,userEvent 分段模拟不适用,改用 fireEvent.change
    const time = await screen.findByLabelText("重启时刻");
    fireEvent.change(time, { target: { value: "25:99" } });
    await user.click(screen.getByRole("button", { name: "保存定时重启" }));
    expect(schedulerSet).not.toHaveBeenCalled();

    const server = await screen.findByLabelText("NTP 服务器");
    await user.clear(server);
    await user.type(server, "bad server");
    await user.click(screen.getByRole("button", { name: "保存时间同步" }));
    expect(timesyncSet).not.toHaveBeenCalled();
  });

  test("定时重启保存:HH:MM 合法 → schedulerSet 参数", async () => {
    const user = userEvent.setup();
    renderPage();
    // type=time 改用 fireEvent.change 直接赋值(合法值 jsdom 原样保留)
    const time = await screen.findByLabelText("重启时刻");
    fireEvent.change(time, { target: { value: "04:30" } });
    await user.click(screen.getByRole("button", { name: "保存定时重启" }));
    await waitFor(() =>
      expect(schedulerSet).toHaveBeenCalledWith({ rebootEnabled: "0", rebootTime: "04:30" }),
    );
  });

  test("时间同步:保存参数 + 手动同步调用 timesyncNow 并回读", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "保存时间同步" }));
    await waitFor(() =>
      expect(timesyncSet).toHaveBeenCalledWith({
        enabled: "0",
        intervalMinutes: "60",
        server: "ntp.aliyun.com",
      }),
    );
    await user.click(screen.getByRole("button", { name: "手动同步一次" }));
    await waitFor(() => expect(timesyncNow).toHaveBeenCalledTimes(1));
    // 初次加载 + 保存回读 + 同步回读
    await waitFor(() => expect(timesyncGet).toHaveBeenCalledTimes(3));
  });
});
