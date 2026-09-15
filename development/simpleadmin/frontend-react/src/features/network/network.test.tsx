// network 页单测:bandMap 完整性(与旧 www/js/band_map.js 频段集一致)、save_settings
// 载荷守卫、配置档 localStorage 旧格式兼容、频段 chip 交互、锁定/恢复(confirm mock)。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { toast } from "sonner";

import "@/lib/i18n";
import { networkData } from "@/lib/api";

import { BAND_DEFAULTS, buildBandGroups, joinBandValues, parseBandList } from "./lib/bandMap";
import {
  BAND_PROFILES_STORAGE_KEY,
  buildSaveSettingsPayload,
  loadBandProfiles,
  persistBandProfiles,
} from "./lib";
import type { BandProfile } from "./lib";
import NetworkPage from "./page";

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

const SETTINGS_OK = {
  ok: true,
  pending: false,
  sim: "1",
  apn: "cmnet",
  pdpType: "IP",
  prefNetwork: "AUTO",
  nrModeControl: "未禁用",
  nrModeControlNum: "0",
  bands: "B3 / n78",
  cellLockStatus: "未锁定",
};

const BANDS_OK = {
  ok: true,
  locked_lte_bands: "1:3",
  locked_nsa_bands: "78",
  locked_sa_bands: "",
};

function mockApi(overrides: Record<string, unknown> = {}): void {
  networkDataMock.mockImplementation(((action: string) => {
    if (action in overrides) return Promise.resolve(overrides[action]);
    if (action === "settings") return Promise.resolve(SETTINGS_OK);
    if (action === "bands") return Promise.resolve(BANDS_OK);
    return Promise.resolve({ ok: true });
  }) as unknown as typeof networkData);
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  confirmMock.mockResolvedValue(true);
  mockApi();
});

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <NetworkPage />
    </QueryClientProvider>,
  );
}

async function awaitChipsEnabled(): Promise<void> {
  const chip = await screen.findByRole("checkbox", { name: "B1" });
  await waitFor(() => expect(chip).toBeEnabled());
}

function panelOf(container: HTMLElement, mode: string): HTMLElement {
  const element = container.querySelector(`[data-slot="band-mode-panel"][data-mode="${mode}"]`);
  if (!element) throw new Error(`band panel ${mode} not found`);
  return element as HTMLElement;
}

// ---------------------------------------------------------------------------
// bandMap 完整性(与旧 band_map.js 一致)
// ---------------------------------------------------------------------------

describe("bandMap", () => {
  test("LTE 9 频段与旧 band_map.js DEFAULTS 一致", () => {
    expect(parseBandList(BAND_DEFAULTS.lte)).toEqual([
      "1",
      "3",
      "5",
      "8",
      "34",
      "38",
      "39",
      "40",
      "41",
    ]);
  });

  test("NSA/SA 集合与旧 band_map.js DEFAULTS 一致", () => {
    expect(parseBandList(BAND_DEFAULTS.nsa)).toEqual(["1", "8", "28", "41", "78"]);
    expect(parseBandList(BAND_DEFAULTS.sa)).toEqual(["1", "8", "28", "41", "78"]);
  });

  test("parseBandList 去空白去空项,null/undefined 返回空", () => {
    expect(parseBandList(" 1 : :3: ")).toEqual(["1", "3"]);
    expect(parseBandList(null)).toEqual([]);
    expect(parseBandList(undefined)).toEqual([]);
    expect(parseBandList("")).toEqual([]);
  });

  test("buildBandGroups:三列顺序/标题/前缀,checked 跟随已锁频段", () => {
    const groups = buildBandGroups({ LTE: "3:5", NSA: null, SA: "78" });
    expect(groups.map((group) => group.mode)).toEqual(["LTE", "NSA", "SA"]);
    expect(groups.map((group) => group.title)).toEqual(["LTE", "NR5G-NSA", "NR5G-SA"]);
    expect(groups.map((group) => group.prefix)).toEqual(["B", "N", "N"]);
    expect(groups[0].bands.map((band) => band.name)).toEqual(parseBandList(BAND_DEFAULTS.lte));
    expect(groups[0].bands.filter((band) => band.checked).map((band) => band.name)).toEqual([
      "3",
      "5",
    ]);
    expect(groups[1].bands.every((band) => !band.checked)).toBe(true);
    expect(groups[2].bands.filter((band) => band.checked).map((band) => band.name)).toEqual(["78"]);
  });

  test("joinBandValues 冒号拼接(lock_bands values 格式)", () => {
    expect(joinBandValues(["1", "3", "41"])).toBe("1:3:41");
  });
});

// ---------------------------------------------------------------------------
// save_settings 载荷守卫(对齐旧版 saveChanges)
// ---------------------------------------------------------------------------

describe("buildSaveSettingsPayload", () => {
  const snapshot = {
    apn: "cmnet",
    pdpType: "IP",
    prefNetwork: "AUTO",
    nrModeControlCurrent: "0",
  };
  const untouched = {
    newApn: null,
    newPdpType: null,
    prefNetworkMode: null,
    nrModeControlNew: null,
  };

  test("无改动 → no-change;选中与当前相同的值不算改动", () => {
    expect(buildSaveSettingsPayload(snapshot, untouched)).toEqual({ kind: "no-change" });
    expect(buildSaveSettingsPayload(snapshot, { ...untouched, prefNetworkMode: "AUTO" })).toEqual({
      kind: "no-change",
    });
    expect(buildSaveSettingsPayload(snapshot, { ...untouched, nrModeControlNew: "0" })).toEqual({
      kind: "no-change",
    });
  });

  test("仅改 PDP:一并提交当前 APN(空 APN=只改 PDP 语义)", () => {
    expect(buildSaveSettingsPayload(snapshot, { ...untouched, newPdpType: "IPV4V6" })).toEqual({
      kind: "submit",
      payload: { pdpType: "IPV4V6", apn: "cmnet" },
    });
  });

  test("改 APN:trim 后与当前 PDP 一并提交;PDP 未就绪兜底 IPV4V6", () => {
    expect(buildSaveSettingsPayload(snapshot, { ...untouched, newApn: "  3gnet " })).toEqual({
      kind: "submit",
      payload: { pdpType: "IP", apn: "3gnet" },
    });
    expect(
      buildSaveSettingsPayload({ ...snapshot, pdpType: "-" }, { ...untouched, newApn: "3gnet" }),
    ).toEqual({ kind: "submit", payload: { pdpType: "IPV4V6", apn: "3gnet" } });
  });

  test("设置未就绪(APN 为 -)时仅改 PDP → not-ready;清空 APN → not-ready", () => {
    expect(
      buildSaveSettingsPayload({ ...snapshot, apn: "-" }, { ...untouched, newPdpType: "IPV4V6" }),
    ).toEqual({ kind: "not-ready" });
    expect(buildSaveSettingsPayload(snapshot, { ...untouched, newApn: "" })).toEqual({
      kind: "not-ready",
    });
  });

  test("首选网络/NR5G 模式控制仅在变化时提交", () => {
    expect(buildSaveSettingsPayload(snapshot, { ...untouched, prefNetworkMode: "LTE" })).toEqual({
      kind: "submit",
      payload: { modePref: "LTE" },
    });
    expect(buildSaveSettingsPayload(snapshot, { ...untouched, nrModeControlNew: "1" })).toEqual({
      kind: "submit",
      payload: { nrDisableMode: "1" },
    });
  });
});

// ---------------------------------------------------------------------------
// 配置档 localStorage(兼容旧格式)
// ---------------------------------------------------------------------------

describe("band profiles localStorage", () => {
  test("读取旧版格式:过滤无名/空条目", () => {
    localStorage.setItem(
      BAND_PROFILES_STORAGE_KEY,
      JSON.stringify([
        {
          name: "旧档",
          savedAt: "2024-01-01T00:00:00.000Z",
          data: {
            bands: { LTE: ["1", "3"], NSA: [], SA: [] },
            prefNetworkMode: "LTE",
            nrModeControlNew: null,
          },
        },
        null,
        { savedAt: "2024-01-02T00:00:00.000Z" },
      ]),
    );
    const profiles = loadBandProfiles();
    expect(profiles).toHaveLength(1);
    expect(profiles[0].name).toBe("旧档");
    expect(profiles[0].data?.bands?.LTE).toEqual(["1", "3"]);
    expect(profiles[0].data?.prefNetworkMode).toBe("LTE");
  });

  test("损坏 JSON / 非数组 → 空列表", () => {
    localStorage.setItem(BAND_PROFILES_STORAGE_KEY, "{oops");
    expect(loadBandProfiles()).toEqual([]);
    localStorage.setItem(BAND_PROFILES_STORAGE_KEY, '{"name":"not-array"}');
    expect(loadBandProfiles()).toEqual([]);
  });

  test("persist 往返写入;存储抛错返回 false", () => {
    const profiles: BandProfile[] = [{ name: "a", data: { bands: { LTE: ["1"] } } }];
    expect(persistBandProfiles(profiles)).toBe(true);
    expect(loadBandProfiles()).toEqual(profiles);
    const throwing = {
      setItem: () => {
        throw new Error("quota");
      },
    };
    expect(persistBandProfiles(profiles, throwing)).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// 频段矩阵组件交互
// ---------------------------------------------------------------------------

describe("NetworkPage 频段矩阵", () => {
  test("渲染三列 chip 网格,已锁频段选中并带 locked 角标", async () => {
    const { container } = renderPage();
    await awaitChipsEnabled();
    expect(container.querySelectorAll('[data-slot="band-mode-panel"]')).toHaveLength(3);
    const lte = panelOf(container, "LTE");
    expect(within(lte).getAllByRole("checkbox")).toHaveLength(9);
    expect(within(lte).getByRole("checkbox", { name: "B1" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(within(lte).getByRole("checkbox", { name: "B1" })).toHaveAttribute("data-locked");
    expect(within(lte).getByRole("checkbox", { name: "B5" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    const nsa = panelOf(container, "NSA");
    expect(within(nsa).getAllByRole("checkbox")).toHaveLength(5);
    expect(within(nsa).getByRole("checkbox", { name: "N78" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    const sa = panelOf(container, "SA");
    expect(
      within(sa)
        .getAllByRole("checkbox")
        .every((chip) => chip.getAttribute("aria-checked") === "false"),
    ).toBe(true);
    expect(screen.getByText(/B3 \/ n78/)).toBeInTheDocument();
  });

  test("chip 点击切换选中态", async () => {
    const { container } = renderPage();
    await awaitChipsEnabled();
    const lte = panelOf(container, "LTE");
    const b5 = within(lte).getByRole("checkbox", { name: "B5" });
    fireEvent.click(b5);
    expect(b5).toHaveAttribute("aria-checked", "true");
    const b1 = within(lte).getByRole("checkbox", { name: "B1" });
    fireEvent.click(b1);
    expect(b1).toHaveAttribute("aria-checked", "false");
  });

  test("全选/取消选中切换整列", async () => {
    const { container } = renderPage();
    await awaitChipsEnabled();
    const lte = panelOf(container, "LTE");
    fireEvent.click(within(lte).getByRole("button", { name: "全部选中" }));
    expect(
      within(lte)
        .getAllByRole("checkbox")
        .every((chip) => chip.getAttribute("aria-checked") === "true"),
    ).toBe(true);
    fireEvent.click(within(lte).getByRole("button", { name: "取消选中" }));
    expect(
      within(lte)
        .getAllByRole("checkbox")
        .every((chip) => chip.getAttribute("aria-checked") === "false"),
    ).toBe(true);
  });

  test("零选中锁定 → danger toast 且不发请求", async () => {
    const { container } = renderPage();
    await awaitChipsEnabled();
    const sa = panelOf(container, "SA");
    fireEvent.click(within(sa).getByRole("button", { name: "锁定" }));
    expect(toast.error).toHaveBeenCalledWith("没有选中任何频段，请选择至少一个频段！");
    expect(networkDataMock).not.toHaveBeenCalledWith("lock_bands", expect.anything());
  });

  test("锁定提交 lock_bands(mode+values 冒号拼接)并 toast", async () => {
    const { container } = renderPage();
    await awaitChipsEnabled();
    const lte = panelOf(container, "LTE");
    fireEvent.click(within(lte).getByRole("button", { name: "锁定" }));
    await waitFor(() =>
      expect(networkDataMock).toHaveBeenCalledWith("lock_bands", {
        mode: "LTE",
        values: "1:3",
      }),
    );
    await waitFor(() => expect(toast.info).toHaveBeenCalledWith("频段锁定已提交，正在刷新状态"));
  });

  test("恢复全部:危险确认后提交 reset_bands(全频段默认值)", async () => {
    renderPage();
    await screen.findByRole("checkbox", { name: "B1" });
    fireEvent.click(screen.getByRole("button", { name: "恢复全部" }));
    expect(confirmMock).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "恢复全部",
        message: "这将把频段锁定恢复为全部频段。继续？",
        danger: true,
      }),
    );
    await waitFor(() =>
      expect(networkDataMock).toHaveBeenCalledWith("reset_bands", {
        lte: BAND_DEFAULTS.lte,
        nsa: BAND_DEFAULTS.nsa,
        sa: BAND_DEFAULTS.sa,
      }),
    );
  });

  test("恢复全部:取消确认不发请求", async () => {
    confirmMock.mockResolvedValue(false);
    renderPage();
    await screen.findByRole("checkbox", { name: "B1" });
    fireEvent.click(screen.getByRole("button", { name: "恢复全部" }));
    await waitFor(() => expect(confirmMock).toHaveBeenCalled());
    expect(networkDataMock).not.toHaveBeenCalledWith("reset_bands", expect.anything());
  });
});

// ---------------------------------------------------------------------------
// 蜂窝设置表单
// ---------------------------------------------------------------------------

describe("NetworkPage 蜂窝设置", () => {
  test("无改动保存 → info toast 且不发请求", async () => {
    renderPage();
    await screen.findByPlaceholderText("cmnet");
    // 当前首选网络 AUTO 经选项映射表显示可读标签"自动",不再出现原始值
    expect(screen.getByText("当前：自动")).toBeInTheDocument();
    expect(screen.queryByText("当前：AUTO")).not.toBeInTheDocument();
    // 无草稿时下方选项默认勾选与当前值一致的"自动"(展示态,draft 仍为 null)
    expect(screen.getByRole("radio", { name: "自动" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("radio", { name: "仅LTE" })).toHaveAttribute("aria-checked", "false");
    fireEvent.click(screen.getByRole("button", { name: "保存更改" }));
    expect(toast.info).toHaveBeenCalledWith("没有做出更改");
    expect(networkDataMock).not.toHaveBeenCalledWith("save_settings", expect.anything());
  });

  test("改 APN → save_settings 携带当前 PDP,成功 toast 已保存", async () => {
    renderPage();
    await screen.findByPlaceholderText("cmnet");
    fireEvent.change(screen.getByLabelText("APN"), { target: { value: "3gnet" } });
    fireEvent.click(screen.getByRole("button", { name: "保存更改" }));
    await waitFor(() =>
      expect(networkDataMock).toHaveBeenCalledWith("save_settings", {
        pdpType: "IP",
        apn: "3gnet",
      }),
    );
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("已保存"));
  });

  test("设置未就绪:APN 占位显示获取中,无改动保存不发请求", async () => {
    mockApi({ settings: { ...SETTINGS_OK, apn: "-", pdpType: "-" } });
    renderPage();
    await screen.findByPlaceholderText("获取中...");
    fireEvent.click(screen.getByRole("button", { name: "保存更改" }));
    expect(toast.info).toHaveBeenCalledWith("没有做出更改");
    expect(networkDataMock).not.toHaveBeenCalledWith("save_settings", expect.anything());
  });
});

// ---------------------------------------------------------------------------
// 锁频配置档面板
// ---------------------------------------------------------------------------

describe("NetworkPage 锁频配置档", () => {
  const oldProfile: BandProfile = {
    name: "旧档",
    savedAt: "2024-01-01T00:00:00.000Z",
    data: {
      bands: { LTE: ["5"], NSA: [], SA: [] },
      prefNetworkMode: "LTE",
      nrModeControlNew: "1",
    },
  };

  test("保存当前配置:写入旧格式 localStorage 并 toast", async () => {
    renderPage();
    await awaitChipsEnabled();
    fireEvent.change(screen.getByLabelText("档名"), { target: { value: "夜间档" } });
    fireEvent.click(screen.getByRole("button", { name: "保存当前配置" }));
    expect(toast.success).toHaveBeenCalledWith("配置档已保存");
    const stored = JSON.parse(localStorage.getItem(BAND_PROFILES_STORAGE_KEY) ?? "[]");
    expect(stored).toHaveLength(1);
    expect(stored[0].name).toBe("夜间档");
    expect(stored[0].savedAt).toEqual(expect.any(String));
    expect(stored[0].data.bands.LTE).toEqual(["1", "3"]);
    expect(stored[0].data.bands.NSA).toEqual(["78"]);
    expect(stored[0].data.bands.SA).toEqual([]);
    expect(stored[0].data.prefNetworkMode).toBeNull();
  });

  test("空档名 → danger toast 不写入", async () => {
    renderPage();
    await awaitChipsEnabled();
    fireEvent.click(screen.getByRole("button", { name: "保存当前配置" }));
    expect(toast.error).toHaveBeenCalledWith("请输入档名");
    expect(localStorage.getItem(BAND_PROFILES_STORAGE_KEY)).toBeNull();
  });

  test("旧格式配置档渲染摘要 chips;应用仅填充界面不提交", async () => {
    localStorage.setItem(BAND_PROFILES_STORAGE_KEY, JSON.stringify([oldProfile]));
    const { container } = renderPage();
    await screen.findByText("旧档");
    await screen.findByRole("checkbox", { name: "B1" });
    expect(screen.getByText(/LTE: B5/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    expect(toast.info).toHaveBeenCalledWith("配置档已应用，仅填充界面，未自动提交");
    expect(networkDataMock).not.toHaveBeenCalledWith("save_settings", expect.anything());
    const lte = panelOf(container, "LTE");
    expect(within(lte).getByRole("checkbox", { name: "B5" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(within(lte).getByRole("checkbox", { name: "B1" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  test("删除配置档:危险确认后移除并持久化", async () => {
    localStorage.setItem(BAND_PROFILES_STORAGE_KEY, JSON.stringify([oldProfile]));
    renderPage();
    await screen.findByText("旧档");
    fireEvent.click(screen.getByRole("button", { name: "删除配置档 旧档" }));
    expect(confirmMock).toHaveBeenCalledWith(
      expect.objectContaining({ title: "删除配置档", danger: true }),
    );
    await waitFor(() => expect(screen.queryByText("旧档")).not.toBeInTheDocument());
    expect(JSON.parse(localStorage.getItem(BAND_PROFILES_STORAGE_KEY) ?? "[]")).toEqual([]);
    expect(toast.success).toHaveBeenCalledWith("配置档已删除");
  });
});
