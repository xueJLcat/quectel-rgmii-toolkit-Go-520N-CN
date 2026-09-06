// firewall 测试:纯函数校验矩阵(端口越界/重复/冲突/64 上限、DNAT 32 上限/IPv4/唯一性)、
// 暂存 diff 计算(新增/删除集合)+ 组件行为(Tabs 切换、脏条出现、应用 confirm 显示 diff 摘要、
// status6 失败 ErrorRetry)。vi.mock @/lib/api 隔离 WS 网关。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import "@/lib/i18n";
import { ConfirmDialog } from "@/components/common";
import FirewallPage from "@/features/firewall/page";
import { firewallData, networkConfigData } from "@/lib/api";
import { useConfirmStore } from "@/stores/confirm";

import {
  MAX_FWD_RULES,
  MAX_PORT_RULES,
  apiErrorMessage,
  buildSaveParams,
  computeFwdDiff,
  computeRuleDiff,
  formatBytes,
  formatCount,
  fwdSaveParam,
  isUntrustedDmzStatus,
  isValidIPv4,
  normalizeFwdRules,
  normalizeRules,
  resolveDmzMode,
  rulesEqual,
  sortRulesByPort,
  toFwdRule,
  validateAddFwd,
  validateAddRule,
} from "./lib";
import type { FwdRule, PortRule } from "./lib";

vi.mock("@/lib/api", () => ({
  firewallData: vi.fn(),
  networkConfigData: vi.fn(),
}));

const firewallDataMock = vi.mocked(firewallData);
const networkConfigDataMock = vi.mocked(networkConfigData);

// Radix 在 jsdom 缺失的指针/滚动 API 桩(先例:shell.test.tsx / ui.test.tsx)
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

const STATUS_PAYLOAD = {
  rules: [
    { port: "22", action: "block" },
    { port: "443", action: "accept" },
  ],
  ruleCount: 7,
  jumpInstalled: true,
  chains: [
    {
      name: "SADMIN_FW",
      policy: "DROP",
      pkts: 1234567,
      bytes: 5242880,
      rules: [
        {
          num: 1,
          pkts: 10,
          bytes: 600,
          target: "ACCEPT",
          proto: "tcp",
          in: "bridge0",
          out: "*",
          source: "0.0.0.0/0",
          destination: "0.0.0.0/0",
          extra: "tcp dpt:443",
        },
      ],
    },
  ],
};

interface MockOverrides {
  status?: unknown;
  status6?: unknown;
  fwd_list?: unknown;
}

function mockFirewall(overrides: MockOverrides = {}): void {
  firewallDataMock.mockImplementation((action: string) => {
    switch (action) {
      case "status":
        return Promise.resolve(overrides.status ?? STATUS_PAYLOAD);
      case "status6":
        return Promise.resolve(overrides.status6 ?? { ok: true, chains: [] });
      case "fwd_list":
        return Promise.resolve(
          overrides.fwd_list ?? { ok: true, forwarded: [], jumpInstalled: false },
        );
      case "save":
      case "fwd_save":
        return Promise.resolve({ ok: true });
      default:
        return Promise.resolve({});
    }
  });
}

function renderPage(): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <FirewallPage />
      <ConfirmDialog />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  useConfirmStore.setState({ options: null, resolve: null });
  mockFirewall();
  networkConfigDataMock.mockImplementation((action: string) => {
    if (action === "status") {
      return Promise.resolve({ dmzMode: "0", dmzIP: "", lanGwIp: "192.168.5.1" });
    }
    return Promise.resolve({ ok: true });
  });
});

describe("IPv4 规则校验矩阵", () => {
  const rules: PortRule[] = [
    { port: "22", action: "block" },
    { port: "443", action: "accept" },
  ];

  test("端口越界/非数字 → invalidPort", () => {
    for (const raw of ["0", "65536", "abc", "-1", "80.5", "", "  "]) {
      expect(validateAddRule(rules, raw, "block"), `raw=${raw}`).toBe("invalidPort");
    }
    expect(validateAddRule(rules, "1", "block")).toBeNull();
    expect(validateAddRule(rules, "65535", "block")).toBeNull();
  });

  test("同端口同动作 → duplicate(含前导零归一)", () => {
    expect(validateAddRule(rules, "22", "block")).toBe("duplicate");
    expect(validateAddRule(rules, "022", "block")).toBe("duplicate");
    expect(validateAddRule(rules, "443", "accept")).toBe("duplicate");
  });

  test("同端口反动作 → conflict", () => {
    expect(validateAddRule(rules, "22", "accept")).toBe("conflict");
    expect(validateAddRule(rules, "443", "block")).toBe("conflict");
  });

  test("规则总数达到 64 上限 → limit", () => {
    const full: PortRule[] = Array.from({ length: MAX_PORT_RULES }, (_, index) => ({
      port: String(index + 1),
      action: "block" as const,
    }));
    expect(validateAddRule(full, "100", "block")).toBe("limit");
    expect(validateAddRule(full.slice(0, MAX_PORT_RULES - 1), "100", "block")).toBeNull();
  });
});

describe("暂存 diff 与保存参数", () => {
  const server: PortRule[] = [
    { port: "22", action: "block" },
    { port: "443", action: "accept" },
  ];

  test("computeRuleDiff 计算新增/删除集合", () => {
    const draft: PortRule[] = [
      { port: "22", action: "block" },
      { port: "8080", action: "block" },
    ];
    const diff = computeRuleDiff(server, draft);
    expect(diff.added).toEqual([{ port: "8080", action: "block" }]);
    expect(diff.removed).toEqual([{ port: "443", action: "accept" }]);
  });

  test("动作翻转 = 一删一增", () => {
    const draft: PortRule[] = [
      { port: "22", action: "accept" },
      { port: "443", action: "accept" },
    ];
    const diff = computeRuleDiff(server, draft);
    expect(diff.added).toEqual([{ port: "22", action: "accept" }]);
    expect(diff.removed).toEqual([{ port: "22", action: "block" }]);
  });

  test("rulesEqual 与顺序无关;normalizeRules 归一化动作", () => {
    expect(
      rulesEqual(server, [
        { port: "443", action: "accept" },
        { port: "22", action: "block" },
      ]),
    ).toBe(true);
    expect(rulesEqual(server, [{ port: "22", action: "block" }])).toBe(false);
    expect(normalizeRules([{ port: "80" }, { port: "443", action: "accept" }])).toEqual([
      { port: "80", action: "block" },
      { port: "443", action: "accept" },
    ]);
    expect(normalizeRules(undefined)).toEqual([]);
  });

  test("buildSaveParams:阻止=非 accept,分别逗号拼接(对齐旧 saveRules)", () => {
    expect(
      buildSaveParams([
        { port: "22", action: "block" },
        { port: "443", action: "accept" },
        { port: "80", action: "accept" },
      ]),
    ).toEqual({ block_ports: "22", accept_ports: "443,80" });
    expect(buildSaveParams([])).toEqual({ block_ports: "", accept_ports: "" });
  });

  test("sortRulesByPort 按端口数值排序", () => {
    expect(
      sortRulesByPort([
        { port: "100", action: "block" },
        { port: "22", action: "block" },
      ]).map((rule) => rule.port),
    ).toEqual(["22", "100"]);
  });
});

describe("DNAT 校验矩阵", () => {
  const rules: FwdRule[] = [
    { extPort: 8080, intIP: "192.168.5.20", intPort: 80, proto: "tcp", enabled: true },
  ];

  test("端口越界/非数字", () => {
    expect(
      validateAddFwd(rules, { extPort: "0", intIP: "192.168.5.21", intPort: "80", proto: "tcp" }),
    ).toBe("invalidExtPort");
    expect(
      validateAddFwd(rules, { extPort: "x", intIP: "192.168.5.21", intPort: "80", proto: "tcp" }),
    ).toBe("invalidExtPort");
    expect(
      validateAddFwd(rules, {
        extPort: "9000",
        intIP: "192.168.5.21",
        intPort: "65536",
        proto: "tcp",
      }),
    ).toBe("invalidIntPort");
  });

  test("非法 IPv4", () => {
    for (const ip of ["999.1.1.1", "abc", "192.168.5", "192.168.5.20.1", ""]) {
      expect(
        validateAddFwd(rules, { extPort: "9000", intIP: ip, intPort: "80", proto: "tcp" }),
        `ip=${ip}`,
      ).toBe("invalidIp");
    }
    expect(isValidIPv4("192.168.5.20")).toBe(true);
    expect(isValidIPv4("255.255.255.255")).toBe(true);
    expect(isValidIPv4("256.1.1.1")).toBe(false);
  });

  test("proto 仅限 tcp/udp;(extPort,proto) 唯一", () => {
    expect(
      validateAddFwd(rules, {
        extPort: "9000",
        intIP: "192.168.5.21",
        intPort: "80",
        proto: "icmp",
      }),
    ).toBe("invalidProto");
    expect(
      validateAddFwd(rules, {
        extPort: "8080",
        intIP: "192.168.5.21",
        intPort: "81",
        proto: "tcp",
      }),
    ).toBe("duplicate");
    // 同端口不同协议不冲突
    expect(
      validateAddFwd(rules, {
        extPort: "8080",
        intIP: "192.168.5.21",
        intPort: "81",
        proto: "udp",
      }),
    ).toBeNull();
  });

  test("32 条上限", () => {
    const full: FwdRule[] = Array.from({ length: MAX_FWD_RULES }, (_, index) => ({
      extPort: index + 1,
      intIP: "192.168.5.5",
      intPort: index + 1,
      proto: "tcp" as const,
      enabled: true,
    }));
    expect(
      validateAddFwd(full, { extPort: "100", intIP: "192.168.5.9", intPort: "100", proto: "udp" }),
    ).toBe("limit");
    expect(
      validateAddFwd(full.slice(0, MAX_FWD_RULES - 1), {
        extPort: "100",
        intIP: "192.168.5.9",
        intPort: "100",
        proto: "udp",
      }),
    ).toBeNull();
  });

  test("normalizeFwdRules 容错归一化;fwdSaveParam 输出后端契约 JSON", () => {
    expect(
      normalizeFwdRules([
        { extPort: 8080, intIP: " 192.168.5.20 ", intPort: 80, proto: "UDP", enabled: true },
        { extPort: 22, intIP: "192.168.5.21", intPort: 22, proto: "tcp" },
      ]),
    ).toEqual([
      { extPort: 8080, intIP: "192.168.5.20", intPort: 80, proto: "udp", enabled: true },
      { extPort: 22, intIP: "192.168.5.21", intPort: 22, proto: "tcp", enabled: false },
    ]);
    expect(normalizeFwdRules(undefined)).toEqual([]);
    expect(
      JSON.parse(
        fwdSaveParam([
          toFwdRule({ extPort: "8080", intIP: "192.168.5.20", intPort: "80", proto: "tcp" }),
        ]),
      ),
    ).toEqual([{ extPort: 8080, intIP: "192.168.5.20", intPort: 80, proto: "tcp", enabled: true }]);
  });

  test("computeFwdDiff:新增/删除/修改分类", () => {
    const draft: FwdRule[] = [
      { extPort: 8080, intIP: "192.168.5.20", intPort: 80, proto: "tcp", enabled: false },
      { extPort: 9000, intIP: "192.168.5.30", intPort: 90, proto: "udp", enabled: true },
    ];
    const diff = computeFwdDiff(rules, draft);
    expect(diff.added).toHaveLength(1);
    expect(diff.added[0].extPort).toBe(9000);
    expect(diff.removed).toHaveLength(0);
    expect(diff.changed).toHaveLength(1);
    expect(diff.changed[0].extPort).toBe(8080);
  });
});

describe("格式化与 DMZ 判别", () => {
  test("formatBytes/formatCount(对齐旧实现)", () => {
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(2048)).toBe("2.0 KB");
    expect(formatBytes(5 * 1024 * 1024)).toBe("5.0 MB");
    expect(formatBytes(Number.NaN)).toBe("-");
    expect(formatCount(1234567)).toBe("1,234,567");
    expect(formatCount(0)).toBe("0");
    expect(formatCount(Number.NaN)).toBe("-");
  });

  test("resolveDmzMode:有效 IP 即启用,'-' 视为占位", () => {
    expect(resolveDmzMode("0", "")).toBe("0");
    expect(resolveDmzMode("1", "")).toBe("1");
    expect(resolveDmzMode("0", "192.168.5.5")).toBe("1");
    expect(resolveDmzMode("1", "-")).toBe("1");
    expect(resolveDmzMode("0", "-")).toBe("0");
  });

  test("isUntrustedDmzStatus:全默认值判不可信", () => {
    expect(
      isUntrustedDmzStatus({
        ipPassStatus: false,
        DNSV4ProxyStatus: false,
        DNSV6ProxyStatus: false,
        currentUsbNetMode: "未知",
        dmzIP: "",
        lanGwIp: "",
      }),
    ).toBe(true);
    expect(isUntrustedDmzStatus({ ipPassStatus: false, lanGwIp: "192.168.5.1" })).toBe(false);
    expect(isUntrustedDmzStatus(undefined)).toBe(false);
  });

  test("apiErrorMessage:body JSON error 优先,其次 message,最后 fallback", () => {
    expect(apiErrorMessage({ body: '{"error":"端口冲突: 22"}' }, "fb")).toBe("端口冲突: 22");
    expect(apiErrorMessage(new Error("boom"), "fb")).toBe("boom");
    expect(apiErrorMessage(new Error(""), "fb")).toBe("fb");
    expect(apiErrorMessage(null, "fb")).toBe("fb");
  });
});

describe("FirewallPage 组件", () => {
  test("Tabs 切换:默认 IPv4 规则;切到 IPv6 拉取 status6 并显示只读说明", async () => {
    const user = userEvent.setup();
    renderPage();
    expect(await screen.findByText("防火墙状态")).toBeInTheDocument();
    expect(await screen.findByRole("cell", { name: "22" })).toBeInTheDocument();
    expect(firewallDataMock).toHaveBeenCalledWith("status");
    expect(screen.queryByText("未配置端口规则")).not.toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: "IPv6 链" }));
    await waitFor(() => expect(firewallDataMock).toHaveBeenCalledWith("status6"));
    expect(
      await screen.findByText("当前仅支持查看 IPv6 链状态，暂不支持编辑 IPv6 规则。"),
    ).toBeInTheDocument();
    expect(await screen.findByText("暂无规则数据")).toBeInTheDocument();
  });

  test("status6 失败(ip6tables 缺失)→ ErrorRetry + 说明 + 重试按钮", async () => {
    const user = userEvent.setup();
    mockFirewall({ status6: { ok: false, error: "ip6tables not found" } });
    renderPage();
    await screen.findByRole("cell", { name: "22" });
    await user.click(screen.getByRole("tab", { name: "IPv6 链" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("IPv6 链状态获取失败");
    expect(alert).toHaveTextContent("ip6tables not found");
    expect(within(alert).getByRole("button", { name: "重试" })).toBeInTheDocument();
  });

  test("暂存编辑:新增规则 → 脏条出现 → 应用 confirm 显示 diff → save 原子提交", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("cell", { name: "22" });
    expect(screen.queryByText(/项更改未应用/)).not.toBeInTheDocument();

    await user.type(screen.getByLabelText("端口"), "8080");
    await user.click(screen.getByRole("button", { name: "添加" }));
    expect(await screen.findByText("1 项更改未应用")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "应用更改" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("新增 1 条，删除 0 条");
    await user.click(within(dialog).getByRole("button", { name: "应用更改" }));

    await waitFor(() =>
      expect(firewallDataMock).toHaveBeenCalledWith("save", {
        block_ports: "22,8080",
        accept_ports: "443",
      }),
    );
    await waitFor(() => expect(screen.queryByText(/项更改未应用/)).not.toBeInTheDocument());
  });

  test("非法端口新增被前端拦截,不产生脏更改", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("cell", { name: "22" });
    await user.type(screen.getByLabelText("端口"), "99999");
    await user.click(screen.getByRole("button", { name: "添加" }));
    expect(await screen.findByText("端口必须是 1-65535 的数字")).toBeInTheDocument();
    expect(screen.queryByText(/项更改未应用/)).not.toBeInTheDocument();
  });

  test("端口转发 Tab:危险提示卡 + 暂存新增 + fwd_save 原子应用", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("cell", { name: "22" });
    await user.click(screen.getByRole("tab", { name: "端口转发" }));
    expect(await screen.findByText("未配置端口转发规则")).toBeInTheDocument();
    expect(
      screen.getByText(
        "端口转发（DNAT）会把内网服务暴露给外部网络，请仅转发受信任的端口并确认目标设备安全。",
      ),
    ).toBeInTheDocument();

    await user.type(screen.getByLabelText("外部端口"), "8080");
    await user.type(screen.getByLabelText("内部 IP"), "192.168.5.20");
    await user.type(screen.getByLabelText("内部端口"), "80");
    await user.click(screen.getByRole("button", { name: "添加转发" }));
    expect(await screen.findByText("1 项更改未应用")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "应用更改" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("新增 1 条，删除 0 条，修改 0 条");
    await user.click(within(dialog).getByRole("button", { name: "应用更改" }));

    await waitFor(() =>
      expect(firewallDataMock).toHaveBeenCalledWith("fwd_save", {
        rules: JSON.stringify([
          { extPort: 8080, intIP: "192.168.5.20", intPort: 80, proto: "tcp", enabled: true },
        ]),
      }),
    );
  });

  test("全部链 Tab:链折叠面板显示命中计数与字节人性化,展开显示规则表", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("cell", { name: "22" });
    await user.click(screen.getByRole("tab", { name: "全部链" }));
    const header = await screen.findByRole("button", { name: /SADMIN_FW/ });
    expect(header).toHaveTextContent("数据包 1,234,567");
    expect(header).toHaveTextContent("字节 5.0 MB");
    expect(header).toHaveAttribute("aria-expanded", "false");
    await user.click(header);
    expect(header).toHaveAttribute("aria-expanded", "true");
    expect(await screen.findByRole("cell", { name: "tcp dpt:443" })).toBeInTheDocument();
  });
});
