// celllock 页单测:纯函数(小区标识/选择约束 NR 单选+LTE≤10、信号格、pairs 拼接、
// EARFCN 数量上限 NR≤32/LTE≤2、小区数量归一化、锁定状态映射)+ 组件(扫描长任务
// loading/结果表渲染/选择约束/empty 与 pending 语义)+ 锁定顺序(NR→LTE,失败中断)+
// 危险操作 confirm(mock stores/confirm)。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { toast } from "sonner";
import type { ReactNode } from "react";

import "@/lib/i18n";
import { networkData } from "@/lib/api";

import CellLockPage from "./page";
import { ScanCard } from "./components/ScanCard";
import { useLockSelectedCells } from "./hooks";
import {
  buildLtePairsParam,
  cellKey,
  cellLockStatusKey,
  collectLtePairs,
  findScannedCell,
  normalizeLteCellCount,
  parseEarfcnInput,
  rsrpPercent,
  signalBars,
  toggleCellSelection,
} from "./lib";
import type { SelectedCell } from "./lib";

const { confirmMock } = vi.hoisted(() => ({ confirmMock: vi.fn() }));

vi.mock("@/lib/api", () => ({ networkData: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }));
vi.mock("@/stores/confirm", () => ({
  useConfirmStore: (selector: (state: { confirm: typeof confirmMock }) => unknown) =>
    selector({ confirm: confirmMock }),
}));

// Radix 在 jsdom 缺失的指针/滚动 API 桩(与 common.test.tsx 相同)
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

const networkDataMock = vi.mocked(networkData);

const SETTINGS_OK = { ok: true, pending: false, cellLockStatus: "已锁定4G和5G" };
const EARFCN_OK = { ok: true, nr5g_arfcns: ["627264"], lte_arfcns: [] };

function mockApi(overrides: Record<string, unknown> = {}): void {
  networkDataMock.mockImplementation(((action: string) => {
    if (action in overrides) return Promise.resolve(overrides[action]);
    if (action === "settings") return Promise.resolve(SETTINGS_OK);
    if (action === "earfcn_lock_status") return Promise.resolve(EARFCN_OK);
    return Promise.resolve({ ok: true });
  }) as unknown as typeof networkData);
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  confirmMock.mockResolvedValue(true);
  mockApi();
});

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function renderWith(ui: ReactNode) {
  const Wrapper = createWrapper();
  return render(<Wrapper>{ui}</Wrapper>);
}

const nr = (pci: string, freq = "627264", rsrp = "-88") => ({
  type: "NR5G",
  provider: "",
  band: "78",
  freq,
  pci,
  rsrp,
});
const lte = (pci: string, freq: string, rsrp = "-73") => ({
  type: "LTE",
  provider: "",
  band: "-",
  freq,
  pci,
  rsrp,
});
const sel = (cell: {
  type?: string;
  pci?: string;
  provider?: string;
  freq?: string;
}): SelectedCell => ({
  type: cell.type ?? "",
  pci: cell.pci ?? "",
  provider: cell.provider ?? "",
  freq: cell.freq ?? "",
});

// ---------------------------------------------------------------------------
// 纯函数
// ---------------------------------------------------------------------------

describe("cellKey / toggleCellSelection", () => {
  test("cellKey = type|pci|provider|freq(同 PCI 不同频点为不同小区)", () => {
    expect(cellKey({ type: "LTE", pci: "3", provider: "", freq: "1650" })).toBe("LTE|3||1650");
    expect(cellKey({ type: "LTE", pci: "3", provider: "", freq: "1850" })).not.toBe(
      cellKey({ type: "LTE", pci: "3", provider: "", freq: "1650" }),
    );
  });

  test("NR5G 单选:新选替换旧选并保留 LTE;再点取消", () => {
    const lteA = sel(lte("3", "1650"));
    const nr1 = sel(nr("101"));
    const nr2 = sel(nr("222", "640000"));
    let result = toggleCellSelection([], nr1);
    expect(result.blocked).toBeUndefined();
    result = toggleCellSelection(result.next, lteA);
    result = toggleCellSelection(result.next, nr2);
    expect(result.next).toHaveLength(2);
    expect(result.next.map(cellKey)).toEqual([cellKey(lteA), cellKey(nr2)]);
    result = toggleCellSelection(result.next, nr2);
    expect(result.next.map(cellKey)).toEqual([cellKey(lteA)]);
  });

  test("LTE 多选上限 10:第 11 条 blocked,已选可取消", () => {
    let selected: SelectedCell[] = [];
    for (let i = 1; i <= 10; i += 1) {
      const result = toggleCellSelection(selected, sel(lte(String(i), String(1000 + i))));
      expect(result.blocked).toBeUndefined();
      selected = result.next;
    }
    expect(selected).toHaveLength(10);
    const overflow = toggleCellSelection(selected, sel(lte("11", "1011")));
    expect(overflow.blocked).toBe("lte-limit");
    expect(overflow.next).toHaveLength(10);
    const remove = toggleCellSelection(selected, selected[0]);
    expect(remove.next).toHaveLength(9);
  });
});

describe("signalBars / rsrpPercent", () => {
  test.each([
    [-50, 5],
    [-55, 5],
    [-56, 4],
    [-85, 4],
    [-86, 3],
    [-95, 3],
    [-96, 2],
    [-105, 2],
    [-106, 1],
    [-115, 1],
    [-116, 0],
  ])("rsrp %i → %i 格", (rsrp, bars) => {
    expect(signalBars(rsrp)).toBe(bars);
  });

  test("非数值 → 0;percent = 格数/5*100", () => {
    expect(signalBars("abc")).toBe(0);
    expect(signalBars(undefined)).toBe(0);
    expect(rsrpPercent(-50)).toBe(100);
    expect(rsrpPercent(-96)).toBe(40);
    expect(rsrpPercent(-140)).toBe(0);
  });
});

describe("findScannedCell", () => {
  const cells = [nr("101", "627264"), lte("3", "1650")];

  test("pci+provider+freq 命中 → earfcn/pci/band", () => {
    expect(findScannedCell(cells, { pci: "101", provider: "", freq: "627264" })).toEqual({
      earfcn: "627264",
      pci: "101",
      band: "78",
    });
  });

  test("freq 缺省时仅按 pci+provider 匹配;未命中返回 null", () => {
    expect(findScannedCell(cells, { pci: "3", provider: "" })).not.toBeNull();
    expect(findScannedCell(cells, { pci: "999", provider: "" })).toBeNull();
    expect(findScannedCell(cells, { pci: "101", provider: "", freq: "1" })).toBeNull();
  });
});

describe("collectLtePairs / buildLtePairsParam", () => {
  test("完整纯数字 → pairs;拼接为 earfcn,pci;earfcn,pci", () => {
    const result = collectLtePairs([
      { earfcn: "100", pci: "1" },
      { earfcn: " 200 ", pci: " 2 " },
    ]);
    expect(result).toEqual({
      status: "ok",
      pairs: [
        { earfcn: "100", pci: "1" },
        { earfcn: "200", pci: "2" },
      ],
    });
    if (result.status === "ok") {
      expect(buildLtePairsParam(result.pairs)).toBe("100,1;200,2");
    }
  });

  test("字段为空 → incomplete;非纯数字 → non-digit", () => {
    expect(collectLtePairs([{ earfcn: "100", pci: "" }])).toEqual({ status: "incomplete" });
    expect(collectLtePairs([{ earfcn: "10a", pci: "1" }])).toEqual({ status: "non-digit" });
    expect(collectLtePairs([])).toEqual({ status: "ok", pairs: [] });
  });
});

describe("parseEarfcnInput", () => {
  test("逗号分隔去空;NR5G ≤32,LTE ≤2", () => {
    expect(parseEarfcnInput(" 100 , 200 ", "LTE")).toEqual({
      status: "ok",
      arfcns: ["100", "200"],
    });
    expect(parseEarfcnInput("1,2,3", "LTE")).toEqual({ status: "limit" });
    expect(parseEarfcnInput("1,2", "LTE")).toEqual({ status: "ok", arfcns: ["1", "2"] });
    const nr32 = Array.from({ length: 32 }, (_, i) => String(i + 1)).join(",");
    expect(parseEarfcnInput(nr32, "NR5G").status).toBe("ok");
    expect(parseEarfcnInput(`${nr32},33`, "NR5G")).toEqual({ status: "limit" });
  });

  test("空输入 → empty;含非数字 → non-digit", () => {
    expect(parseEarfcnInput(" , ", "NR5G")).toEqual({ status: "empty" });
    expect(parseEarfcnInput("", "LTE")).toEqual({ status: "empty" });
    expect(parseEarfcnInput("10a", "LTE")).toEqual({ status: "non-digit" });
  });
});

describe("normalizeLteCellCount / cellLockStatusKey", () => {
  test.each([
    ["", 0],
    [null, 0],
    ["0", 0],
    ["-2", 0],
    ["3.7", 3],
    ["10", 10],
    ["11", 10],
  ])("小区数量 %j → %i", (raw, expected) => {
    expect(normalizeLteCellCount(raw)).toBe(expected);
  });

  test("cellLockStatus 映射与未知回退", () => {
    expect(cellLockStatusKey("未锁定")).toBe("unlocked");
    expect(cellLockStatusKey("已锁定4G")).toBe("locked4g");
    expect(cellLockStatusKey("已锁定5G")).toBe("locked5g");
    expect(cellLockStatusKey("已锁定4G和5G")).toBe("locked4gAnd5g");
    expect(cellLockStatusKey("其他")).toBe("unknown");
    expect(cellLockStatusKey(undefined)).toBe("unknown");
  });
});

// ---------------------------------------------------------------------------
// 扫描卡
// ---------------------------------------------------------------------------

describe("ScanCard", () => {
  const scanResult = {
    ok: true,
    nr5g_cells_parsed: [nr("101"), nr("222", "640000", "-101")],
    lte_cells_parsed: [lte("3", "1650"), lte("7", "1850", "-96")],
  };

  test("长任务:扫描中按钮 loading + 进度文案,完成后渲染结果表(MetricBar)", async () => {
    let resolveScan: ((value: unknown) => void) | undefined;
    networkDataMock.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveScan = resolve;
        }) as unknown as Promise<never>,
    );
    const { container } = renderWith(<ScanCard />);
    fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
    // TanStack 经微任务启动 mutationFn:先冲刷一次让延迟 Promise 就位
    await act(async () => {});
    expect(screen.getByRole("button", { name: /扫描中/ })).toBeDisabled();
    expect(screen.getByText("扫描中…最长约 3 分钟,请勿离开页面")).toBeInTheDocument();
    await act(async () => {
      resolveScan?.(scanResult);
    });
    expect(networkDataMock).toHaveBeenCalledWith("scan", { mode: "Neighbour Scan" });
    await screen.findByText("627264");
    expect(screen.getByText("101")).toBeInTheDocument();
    expect(screen.getByText("1850")).toBeInTheDocument();
    // RSRP 内嵌 MetricBar(每张结果行一条)
    expect(container.querySelectorAll('[class*="h-1.5"]')).toHaveLength(4);
  });

  test("选择约束:NR 单选替换、行高亮,锁定所选计数", async () => {
    mockApi({ scan: scanResult });
    renderWith(<ScanCard />);
    fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
    await screen.findByText("627264");

    const row101 = screen.getByText("101").closest("tr") as HTMLElement;
    const row222 = screen.getByText("222").closest("tr") as HTMLElement;
    const row3 = screen.getByText("3", { selector: "td" }).closest("tr") as HTMLElement;

    fireEvent.click(row101);
    expect(row101).toHaveAttribute("data-state", "selected");
    expect(screen.getByRole("button", { name: /锁定所选/ })).toHaveTextContent("1");

    // NR 单选:选 222 替换 101
    fireEvent.click(row222);
    expect(row101).not.toHaveAttribute("data-state");
    expect(row222).toHaveAttribute("data-state", "selected");
    expect(screen.getByRole("button", { name: /锁定所选/ })).toHaveTextContent("1");

    // LTE 可叠加
    fireEvent.click(row3);
    expect(screen.getByRole("button", { name: /锁定所选/ })).toHaveTextContent("2");

    // 再点已选 NR = 取消
    fireEvent.click(row222);
    expect(screen.getByRole("button", { name: /锁定所选/ })).toHaveTextContent("1");
  });

  test("LTE 超过 10 条 → toast 阻止", async () => {
    const manyLte = Array.from({ length: 11 }, (_, i) => lte(String(i + 1), String(1000 + i)));
    mockApi({ scan: { ok: true, nr5g_cells_parsed: [], lte_cells_parsed: manyLte } });
    renderWith(<ScanCard />);
    fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
    await screen.findByText("1000");
    for (let i = 0; i < 10; i += 1) {
      fireEvent.click(
        screen.getByText(String(1000 + i), { selector: "td" }).closest("tr") as HTMLElement,
      );
    }
    expect(screen.getByRole("button", { name: /锁定所选/ })).toHaveTextContent("10");
    fireEvent.click(screen.getByText("1010", { selector: "td" }).closest("tr") as HTMLElement);
    expect(toast.error).toHaveBeenCalledWith("最多只能选择 10 条小区进行锁定");
    expect(screen.getByRole("button", { name: /锁定所选/ })).toHaveTextContent("10");
  });

  test("pending → 模块尚未就绪提示,不渲染结果表", async () => {
    mockApi({ scan: { ok: true, pending: true } });
    renderWith(<ScanCard />);
    fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
    await screen.findByText("模块尚未就绪,请稍后重试");
    expect(screen.queryByText("627264")).not.toBeInTheDocument();
  });

  test("empty=true → 固件受限提示;成功但零结果 → 未扫描到小区", async () => {
    mockApi({
      scan: { ok: true, empty: true, nr5g_cells_parsed: [], lte_cells_parsed: [] },
    });
    const view1 = renderWith(<ScanCard />);
    fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
    await screen.findByText("扫描完成,模块未返回任何小区(该固件扫描功能可能受限)");
    view1.unmount();

    mockApi({ scan: { ok: true, nr5g_cells_parsed: [], lte_cells_parsed: [] } });
    renderWith(<ScanCard />);
    fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
    await screen.findByText("未扫描到小区");
  });

  test("ok:false → danger toast 展示后端 error", async () => {
    mockApi({ scan: { ok: false, error: "扫描失败：AT 无有效应答，请检查模块或稍后重试" } });
    renderWith(<ScanCard />);
    fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("扫描失败：AT 无有效应答，请检查模块或稍后重试"),
    );
  });
});

// ---------------------------------------------------------------------------
// 锁定所选(hook 级:NR→LTE 顺序与失败中断)
// ---------------------------------------------------------------------------

describe("useLockSelectedCells", () => {
  const nrCells = [nr("101")];
  const lteCells = [lte("3", "1650")];
  const args = {
    nr: sel(nr("101")),
    lte: [sel(lte("3", "1650"))],
    nrCells,
    lteCells,
  };

  function nthCallIndex(action: string, params?: Record<string, unknown>): number {
    return networkDataMock.mock.calls.findIndex(
      (call) =>
        call[0] === action &&
        (params === undefined || JSON.stringify(call[1]) === JSON.stringify(params)),
    );
  }

  test("NR 先锁(不带 scs,后端守护探测),成功后再锁 LTE(逗号并列)", async () => {
    const { result } = renderHook(() => useLockSelectedCells(), { wrapper: createWrapper() });
    await act(async () => {
      result.current.mutate(args);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(networkDataMock).toHaveBeenCalledWith("lock_scanned_cells", {
      mode: "NR5G Only",
      pci: "101",
      earfcn: "627264",
      band: "78",
    });
    expect(networkDataMock).toHaveBeenCalledWith("lock_scanned_cells", {
      mode: "LTE Only",
      earfcn: "1650",
      pci: "3",
    });
    expect(
      nthCallIndex("lock_scanned_cells", {
        mode: "NR5G Only",
        pci: "101",
        earfcn: "627264",
        band: "78",
      }),
    ).toBeLessThan(
      nthCallIndex("lock_scanned_cells", { mode: "LTE Only", earfcn: "1650", pci: "3" }),
    );
    expect(result.current.data).toEqual({ ok: true, nr5gLocked: true });
    expect(toast.success).toHaveBeenCalledWith("操作成功，正在刷新状态");
  });

  test("NR 锁定失败 → 中断,不发 LTE 请求", async () => {
    mockApi({ lock_scanned_cells: { ok: false, error: "boom" } });
    const { result } = renderHook(() => useLockSelectedCells(), { wrapper: createWrapper() });
    await act(async () => {
      result.current.mutate(args);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const lteCall = networkDataMock.mock.calls.find(
      (call) =>
        call[0] === "lock_scanned_cells" && (call[1] as { mode?: string })?.mode === "LTE Only",
    );
    expect(lteCall).toBeUndefined();
    expect(result.current.data).toEqual({
      ok: false,
      reason: "nr-lock-failed",
      nr5gLocked: false,
      error: "boom",
    });
    expect(toast.error).toHaveBeenCalledWith("锁定失败: boom");
    expect(toast.success).not.toHaveBeenCalled();
  });

  test("小区数据缺失 → cell-not-found 提示重新扫描", async () => {
    const { result } = renderHook(() => useLockSelectedCells(), { wrapper: createWrapper() });
    await act(async () => {
      result.current.mutate({ ...args, nr: sel(nr("999", "1")) });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toMatchObject({ ok: false, reason: "cell-not-found" });
    expect(networkDataMock).not.toHaveBeenCalledWith("lock_scanned_cells", expect.anything());
    expect(toast.error).toHaveBeenCalledWith("找不到对应的小区数据，请重新扫描");
  });
});

// ---------------------------------------------------------------------------
// 页面级:横幅 / 解锁 confirm / 一键锁定 / 频点锁
// ---------------------------------------------------------------------------

describe("CellLockPage", () => {
  test("状态横幅:双锁渐变 chip + NR5G 频点锁 chip", async () => {
    renderWith(<CellLockPage />);
    const chip = await screen.findByText("已锁定4G和5G");
    expect(chip.className).toContain("bg-[image:var(--sa-grad-network)]");
    expect(screen.getByText(/NR5G 已启用: 627264/)).toBeInTheDocument();
    expect(screen.getByText(/LTE 未启用/)).toBeInTheDocument();
  });

  test("解锁 LTE:取消确认不发请求;确认后 unlock_lte", async () => {
    renderWith(<CellLockPage />);
    const unlockButton = await screen.findByRole("button", { name: "解锁LTE小区" });
    confirmMock.mockResolvedValue(false);
    fireEvent.click(unlockButton);
    await waitFor(() =>
      expect(confirmMock).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "解锁LTE小区",
          message: "这将解除 LTE 小区锁定。继续？",
          danger: true,
        }),
      ),
    );
    expect(networkDataMock).not.toHaveBeenCalledWith("unlock_lte");

    confirmMock.mockResolvedValue(true);
    fireEvent.click(unlockButton);
    await waitFor(() => expect(networkDataMock).toHaveBeenCalledWith("unlock_lte"));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("操作成功，正在刷新状态"));
  });

  test("一键锁定当前服务小区:确认后 lock_serving_cell(auto 不带 scs),详情拼接 toast", async () => {
    mockApi({
      lock_serving_cell: { ok: true, locked: "LTE", pci: "3", freq: "1650" },
    });
    renderWith(<CellLockPage />);
    fireEvent.click(await screen.findByRole("button", { name: "锁定当前服务小区" }));
    await waitFor(() =>
      expect(confirmMock).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "锁定当前服务小区",
          message: "将锁定当前驻留小区，期间可能短暂断网。继续？",
          danger: true,
        }),
      ),
    );
    await waitFor(() => expect(networkDataMock).toHaveBeenCalledWith("lock_serving_cell", {}));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("LTE / 3 / 1650"));
  });

  test("服务小区锁定失败:透出后端 SCS 守护解锁文案", async () => {
    mockApi({
      lock_serving_cell: {
        ok: false,
        error: "SCS 探测失败，锁定已自动解除，请手动指定 SCS 重试",
      },
    });
    renderWith(<CellLockPage />);
    fireEvent.click(await screen.findByRole("button", { name: "锁定当前服务小区" }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("SCS 探测失败，锁定已自动解除，请手动指定 SCS 重试"),
    );
  });

  test("频点锁:LTE 超上限 toast 阻止;合法输入确认后 set_earfcn_lock 并清空", async () => {
    const { container } = renderWith(<CellLockPage />);
    const lteRow = await waitFor(() => {
      const row = container.querySelector('[data-slot="earfcn-row-LTE"]');
      if (!row) throw new Error("earfcn LTE row not rendered");
      return row as HTMLElement;
    });
    const input = within(lteRow).getByLabelText("LTE 频点（逗号分隔，最多 2 个）");

    fireEvent.change(input, { target: { value: "1,2,3" } });
    fireEvent.click(within(lteRow).getByRole("button", { name: "锁定频点" }));
    expect(toast.error).toHaveBeenCalledWith("频点数量超出限制");
    expect(networkDataMock).not.toHaveBeenCalledWith("set_earfcn_lock", expect.anything());

    fireEvent.change(input, { target: { value: "1650" } });
    fireEvent.click(within(lteRow).getByRole("button", { name: "锁定频点" }));
    await waitFor(() =>
      expect(confirmMock).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "锁定频点",
          message: "这将写入 LTE 频点锁定。继续？",
          danger: true,
        }),
      ),
    );
    await waitFor(() =>
      expect(networkDataMock).toHaveBeenCalledWith("set_earfcn_lock", {
        rat: "LTE",
        arfcns: "1650",
      }),
    );
    await waitFor(() => expect(input).toHaveValue(""));
  });

  test("清除频点锁:危险确认后 clear_earfcn_lock", async () => {
    const { container } = renderWith(<CellLockPage />);
    const nrRow = await waitFor(() => {
      const row = container.querySelector('[data-slot="earfcn-row-NR5G"]');
      if (!row) throw new Error("earfcn NR row not rendered");
      return row as HTMLElement;
    });
    fireEvent.click(within(nrRow).getByRole("button", { name: "解锁频点" }));
    await waitFor(() =>
      expect(confirmMock).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "解锁频点",
          message: "这将解除 NR5G 频点锁定。继续？",
          danger: true,
        }),
      ),
    );
    await waitFor(() =>
      expect(networkDataMock).toHaveBeenCalledWith("clear_earfcn_lock", { rat: "NR5G" }),
    );
  });

  test("频点锁读取失败 → ErrorRetry 横幅", async () => {
    mockApi({ earfcn_lock_status: { ok: false, error: "AT 数据读取失败" } });
    renderWith(<CellLockPage />);
    expect(await screen.findByText("状态获取失败")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "重试" }).length).toBeGreaterThan(0);
  });
});
