// sms 页测试:纯函数矩阵(号码归一化/UTF-16 分段/CPMS 解析/CMS 字典/list_meta diff)+
// 组件测试(收件箱渲染、多选删除 confirm 流程、发送表单分段计数与归一化预览)。
// jsdom 无真 AT 通道:vi.mock("@/lib/api") 隔离全部端点。
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";

import "@/lib/i18n";
import { Providers } from "@/app/providers";
import { atData, deviceInfoData, smsData } from "@/lib/api";
import { useConfirmStore } from "@/stores/confirm";

import SmsPage from "./page";
import {
  appliedSyncState,
  avatarHue,
  buildDeleteIndices,
  createSmsSyncState,
  decodeMaybeUcs2,
  evaluateListResult,
  evaluateMetaPoll,
  explainCmsError,
  formatSmsDate,
  hasIncompleteMultipart,
  makeSmsIndexSignature,
  normalizeNumberPreview,
  parseCustomDate,
  parseImsi,
  parseSmsStorageResponse,
  segmentCount,
  SMS_STORAGE_AT_COMMAND,
  storageTone,
  toSmsRows,
  utf16Units,
} from "./lib";
import type { SmsRow, SmsSyncState } from "./lib";

vi.mock("@/lib/api", () => ({
  ApiError: class ApiError extends Error {
    readonly status: number;
    readonly body: string;
    constructor(status: number, body: string) {
      super(`API request failed (HTTP ${status})`);
      this.status = status;
      this.body = body;
    }
  },
  smsData: vi.fn(),
  atData: vi.fn(),
  deviceInfoData: vi.fn(),
}));

const smsDataMock = vi.mocked(smsData);
const atDataMock = vi.mocked(atData);
const deviceInfoDataMock = vi.mocked(deviceInfoData);

const IMSI_CN = "460011234567890";

const SAMPLE_LIST = {
  messages: [
    {
      sender: "10086",
      date: "24/03/15,08:30:00+32",
      text: "您的余额为 1 元",
      indices: [2],
      storage: "ME",
      concatTotal: 3,
      concatSeq: 1,
      concatRef: "A1",
    },
    {
      sender: "+8613800138000",
      date: "24/03/14,18:05:00+32",
      text: "hello world",
      indices: [1],
      storage: "SM",
    },
  ],
  serviceCenters: ['"+8613800210500"'],
};

// ---------------------------------------------------------------------------
// 纯函数:时间解析(移植 simpleadmin-time.js)
// ---------------------------------------------------------------------------

describe("parseCustomDate / formatSmsDate", () => {
  test("两位时区刻度 +32 剥离,年在先解构", () => {
    const date = parseCustomDate("24/03/15,08:30:00+32");
    expect(date).not.toBeNull();
    expect(date?.getTime()).toBe(Date.UTC(2024, 2, 15, 8, 30, 0));
  });

  test("时:分 形式时区 +08:00 同样可解析", () => {
    const date = parseCustomDate("24/03/15,08:30:00+08:00");
    expect(date?.getTime()).toBe(Date.UTC(2024, 2, 15, 8, 30, 0));
  });

  test("非法输入返回 null", () => {
    expect(parseCustomDate("")).toBeNull();
    expect(parseCustomDate("24/03/15")).toBeNull();
    expect(parseCustomDate("24/03,08:30:00")).toBeNull();
    expect(parseCustomDate(null)).toBeNull();
  });

  test("解析↔显示精确往返(YY/MM/DD 年在先)", () => {
    const date = parseCustomDate("24/03/15,08:30:00+32");
    expect(date).not.toBeNull();
    expect(formatSmsDate(date as Date)).toBe("24/03/15,08:30:00");
  });
});

// ---------------------------------------------------------------------------
// 纯函数:号码归一化矩阵(对齐后端 sms_number.go)
// ---------------------------------------------------------------------------

describe("normalizeNumberPreview", () => {
  test.each([
    ["+86 138-0013-8000", "ready", "+8613800138000"],
    ["008613800138000", "ready", "+8613800138000"],
    ["+1-800-555-1234", "ready", "+18005551234"],
    ["10086", "short", "10086"],
    ["110", "short", "110"],
    ["95588", "short", "95588"],
    ["123456", "short", "123456"],
  ])("%s → %s %s(已有国际格式/短号不补)", (input, status, value) => {
    const preview = normalizeNumberPreview(input, IMSI_CN);
    expect(preview.status).toBe(status);
    if (preview.status === "ready" || preview.status === "short") {
      expect(preview.value).toBe(value);
    }
  });

  test("长号码按 IMSI MCC 460 → +86", () => {
    expect(normalizeNumberPreview("13800138000", IMSI_CN)).toEqual({
      status: "auto",
      value: "+8613800138000",
    });
  });

  test("长号码剥一个前导 0(010… 不拼成 +86010…)", () => {
    expect(normalizeNumberPreview("01012345678", IMSI_CN)).toEqual({
      status: "auto",
      value: "+861012345678",
    });
  });

  test("MCC 310 → +1", () => {
    expect(normalizeNumberPreview("2015550123", "310123456789012")).toEqual({
      status: "auto",
      value: "+12015550123",
    });
  });

  test("7 位号码不属于短号,仍补国家码", () => {
    expect(normalizeNumberPreview("1234567", IMSI_CN)).toEqual({
      status: "auto",
      value: "+861234567",
    });
  });

  test("字母显式报错(vanity 号码不得静默剔除)", () => {
    expect(normalizeNumberPreview("+1-800-FLOWERS", IMSI_CN).status).toBe("letters");
  });

  test("空/退化输入按空号码拦截,不拼裸 +", () => {
    expect(normalizeNumberPreview("", IMSI_CN).status).toBe("empty");
    expect(normalizeNumberPreview("00", IMSI_CN).status).toBe("empty");
    expect(normalizeNumberPreview("+", IMSI_CN).status).toBe("empty");
    expect(normalizeNumberPreview(" - ", IMSI_CN).status).toBe("empty");
  });

  test("IMSI 缺失 → imsi-missing;MCC 未收录 → mcc-missing", () => {
    expect(normalizeNumberPreview("13800138000", "").status).toBe("imsi-missing");
    const missing = normalizeNumberPreview("13800138000", "999011234567890");
    expect(missing.status).toBe("mcc-missing");
    if (missing.status === "mcc-missing") expect(missing.mcc).toBe("999");
  });
});

describe("parseImsi", () => {
  test("从 AT+CIMI 原始应答提取 10–20 位数字", () => {
    expect(parseImsi(`OK\n${IMSI_CN}\n`)).toBe(IMSI_CN);
    expect(parseImsi("短号 123")).toBe("");
    expect(parseImsi("")).toBe("");
  });
});

// ---------------------------------------------------------------------------
// 纯函数:UTF-16 分段计数(中文/英文/emoji 混合)
// ---------------------------------------------------------------------------

describe("utf16Units / segmentCount", () => {
  test.each([
    ["", 0, 0],
    ["abc", 3, 1],
    ["你好世界", 4, 1],
    ["😀", 2, 1], // emoji 按代理对计 2 码元
    ["a你😀", 4, 1],
    ["x".repeat(70), 70, 1],
    ["x".repeat(71), 71, 2],
    ["你".repeat(69) + "😀", 71, 2],
    ["x".repeat(140), 140, 2],
    ["x".repeat(141), 141, 3],
  ])("%j → %i 码元 %i 段", (text, units, segments) => {
    expect(utf16Units(text)).toBe(units);
    expect(segmentCount(text)).toBe(segments);
  });
});

// ---------------------------------------------------------------------------
// 纯函数:CPMS/CSCA 解析 + 存储预警色阶
// ---------------------------------------------------------------------------

const STORAGE_RAW = [
  "OK",
  '+CPMS: "SM",3,40,"SM",3,40,"SM",3,40',
  "OK",
  '+CPMS: "ME",205,255,"ME",205,255,"ME",205,255',
  "OK",
  '+CSCA: "+8613800210500"',
  "OK",
].join("\n");

describe("parseSmsStorageResponse", () => {
  test("解析 SM/ME 占用与 SMSC", () => {
    const status = parseSmsStorageResponse(STORAGE_RAW);
    expect(status.sm).toEqual({ used: 3, total: 40 });
    expect(status.me).toEqual({ used: 205, total: 255 });
    expect(status.smsc).toBe("+8613800210500");
  });

  test("容量缺失/为 0 时回退 ME 255 / SM 40", () => {
    const status = parseSmsStorageResponse('+CPMS: "ME",7,0,"SM",1,');
    expect(status.me).toEqual({ used: 7, total: 255 });
  });

  test("无法解析的应答全部为 null(mock/未就绪)", () => {
    const status = parseSmsStorageResponse("ERROR\n");
    expect(status).toEqual({ me: null, sm: null, smsc: null });
    expect(parseSmsStorageResponse("")).toEqual({ me: null, sm: null, smsc: null });
  });

  test("真机原始应答(含 set 6 字段应答与 UCS2 SMSC)", () => {
    const raw = [
      'AT+CPMS="SM";+CPMS?;+CPMS="ME";+CPMS?;+CSCA?',
      "+CPMS: 1,40,0,255,0,255",
      '+CPMS: "SM",1,40,"ME",0,255,"ME",0,255',
      "+CPMS: 0,255,0,255,0,255",
      '+CPMS: "ME",0,255,"ME",0,255,"ME",0,255',
      '+CSCA: "002B0038003600310033003300340034003100380031003200300030",145',
      "OK",
    ].join("\n");
    const status = parseSmsStorageResponse(raw);
    expect(status.sm).toEqual({ used: 1, total: 40 });
    expect(status.me).toEqual({ used: 0, total: 255 });
    expect(status.smsc).toBe("+8613344181200");
  });
});

describe("decodeMaybeUcs2", () => {
  test("UCS2 hex 按 4 位一组解码", () => {
    expect(decodeMaybeUcs2("002B0038003600310033003800300030003200310030003500300030")).toBe(
      "+8613800210500",
    );
  });

  test.each([
    ["8613800210500"], // 纯数字同为合法 hex,防误判原样
    ["10086"],
    ["00410"], // 长度非 4 倍数
    ["+8613800210500"], // 非 hex
    [""],
  ])("%j 原样返回", (value) => {
    expect(decodeMaybeUcs2(value)).toBe(value);
  });
});

describe("storageTone", () => {
  test.each([
    [100, 255, "domain"],
    [204, 255, "domain"], // 80%
    [205, 255, "warning"], // >80%
    [230, 255, "danger"], // >90%
    [40, 40, "danger"], // 100%
    [1, 0, "domain"], // total 非法
  ])("%i/%i → %s", (used, total, tone) => {
    expect(storageTone(used, total)).toBe(tone);
  });
});

// ---------------------------------------------------------------------------
// 纯函数:CMS 错误码释义字典
// ---------------------------------------------------------------------------

describe("explainCmsError", () => {
  test("+CMS ERROR: 350 原始应答 → 字典释义键", () => {
    const info = explainCmsError("AT+CMGS=...\n+CMS ERROR: 350\n");
    expect(info).toEqual({
      family: "CMS",
      code: "350",
      label: "+CMS ERROR 350",
      hintKey: "cmsError350",
    });
  });

  test("后端归一格式 CMS 304（中文提示）同样命中", () => {
    const info = explainCmsError("CMS 304（固件不接受该 PDU 形态,请使用厂商文本模式）");
    expect(info?.hintKey).toBe("cmsError304");
  });

  test.each(["310", "330", "331", "332", "341", "342", "305"])("已知码 %s 全部收录", (code) => {
    expect(explainCmsError(`+CMS ERROR: ${code}`)?.hintKey).toBe(`cmsError${code}`);
  });

  test("未收录 CMS 码回退通用释义", () => {
    expect(explainCmsError("+CMS ERROR: 999")?.hintKey).toBe("cmsErrorGeneric");
  });

  test("CME 族无专用字典,回退通用释义", () => {
    const info = explainCmsError("+CME ERROR: 10");
    expect(info?.family).toBe("CME");
    expect(info?.hintKey).toBe("cmsErrorGeneric");
  });

  test("无错误码文本返回 null(按原始错误展示)", () => {
    expect(explainCmsError("发送结果未知，短信可能已发送，请勿重复发送")).toBeNull();
    expect(explainCmsError("")).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// 纯函数:list_meta 轮询 diff 状态机(对齐旧版 sms.js)
// ---------------------------------------------------------------------------

function metaMessages(count: number, concatTotal = 0) {
  return Array.from({ length: count }, (_, i) => ({
    sender: `1000${i}`,
    date: `24/03/1${i},08:00:00+32`,
    indices: [i + 1],
    storage: "ME",
    ...(concatTotal > 1 ? { concatTotal, concatSeq: i + 1, concatRef: "R1" } : {}),
  }));
}

describe("evaluateMetaPoll / evaluateListResult", () => {
  test("pending/error 应答不参与 diff,状态原样跳过", () => {
    const state = appliedSyncState({ messages: metaMessages(1) });
    const step = evaluateMetaPoll(state, { pending: true }, 1000);
    expect(step.fetch).toBeNull();
    expect(step.state).toBe(state);
    expect(evaluateMetaPoll(state, { error: "boom" }, 1000).fetch).toBeNull();
  });

  test("签名一致 → 不拉正文", () => {
    const messages = metaMessages(2);
    const state = appliedSyncState({ messages });
    const step = evaluateMetaPoll(state, { messages }, 1000);
    expect(step.fetch).toBeNull();
  });

  test("新变化第一轮只追踪,连续稳定 2 轮才拉正文", () => {
    const applied = appliedSyncState({ messages: metaMessages(1) });
    const changed = { messages: metaMessages(2) };
    const first = evaluateMetaPoll(applied, changed, 1000);
    expect(first.fetch).toBeNull();
    expect(first.state.pendingStableCount).toBe(1);
    const second = evaluateMetaPoll(first.state, changed, 6000);
    expect(second.fetch).toEqual({
      expectedIndexSignature: makeSmsIndexSignature(changed.messages),
    });
    expect(second.state.pendingStableCount).toBe(2);
  });

  test("签名抖动重置稳定计数与等待起点,重新稳定后拉正文(对齐旧版)", () => {
    const sigA = { messages: metaMessages(1) };
    const sigB = { messages: metaMessages(2) };
    let state: SmsSyncState = createSmsSyncState();
    // A 首轮只追踪
    const roundA1 = evaluateMetaPoll(state, sigA, 1000);
    expect(roundA1.fetch).toBeNull();
    expect(roundA1.state.pendingStableCount).toBe(1);
    state = roundA1.state;
    // 抖动到 B:稳定计数与等待起点重置
    const roundB = evaluateMetaPoll(state, sigB, 2000);
    expect(roundB.fetch).toBeNull();
    expect(roundB.state.pendingStableCount).toBe(1);
    expect(roundB.state.pendingFirstSeenAt).toBe(2000);
    state = roundB.state;
    // 回到 A 再稳定一轮 → 拉正文,expected 为 A 的索引签名
    const roundA2 = evaluateMetaPoll(state, sigA, 3000);
    expect(roundA2.fetch).toBeNull();
    state = roundA2.state;
    const roundA3 = evaluateMetaPoll(state, sigA, 8000);
    expect(roundA3.fetch).toEqual({ expectedIndexSignature: makeSmsIndexSignature(sigA.messages) });
  });

  test("concat 未齐时等待,15s 超时后强制拉取", () => {
    const applied = createSmsSyncState();
    const incomplete = { messages: metaMessages(1, 3) }; // concatTotal=3 但只有 1 段
    expect(hasIncompleteMultipart(incomplete.messages)).toBe(true);
    const first = evaluateMetaPoll(applied, incomplete, 0);
    expect(first.fetch).toBeNull();
    const second = evaluateMetaPoll(first.state, incomplete, 5000);
    expect(second.fetch).toBeNull(); // 即使稳定 2 轮,concat 未齐仍等待
    const third = evaluateMetaPoll(second.state, incomplete, 16_000);
    expect(third.fetch).not.toBeNull();
  });

  test("正文结果:一致 unchanged / expected 不符 retrack / 否则 apply", () => {
    const applied = appliedSyncState({ messages: metaMessages(1) });
    const same = { messages: metaMessages(1) };
    expect(evaluateListResult(applied, same, "", 0).decision).toEqual({ kind: "unchanged" });

    const expected = makeSmsIndexSignature(metaMessages(2));
    const moved = { messages: metaMessages(3) };
    const retrack = evaluateListResult(applied, moved, expected, 100);
    expect(retrack.decision).toEqual({ kind: "retrack" });
    expect(retrack.state.pendingStableCount).toBe(1);

    const apply = evaluateListResult(applied, moved, "", 100);
    expect(apply.decision).toEqual({ kind: "apply" });
    expect(apply.state.indexSignature).toBe(makeSmsIndexSignature(moved.messages));
  });
});

// ---------------------------------------------------------------------------
// 纯函数:删除索引串 / 行模型 / 头像色相
// ---------------------------------------------------------------------------

describe("buildDeleteIndices / toSmsRows / avatarHue", () => {
  test("索引串格式对齐旧版 ME:2,SM:1", () => {
    const rows = toSmsRows(SAMPLE_LIST.messages) as SmsRow[];
    expect(buildDeleteIndices(rows)).toBe("ME:2,SM:1");
    expect(buildDeleteIndices([rows[0]])).toBe("ME:2");
    expect(buildDeleteIndices([])).toBe("");
  });

  test("行模型:时间解析、正文规范化、存储归一", () => {
    const rows = toSmsRows([
      {
        sender: "A",
        date: "24/03/15,08:30:00+32",
        text: "a\r\nb\\nc",
        indices: [1],
        storage: "XX",
      },
    ]);
    expect(rows[0].dateText).toBe("24/03/15,08:30:00");
    expect(rows[0].text).toBe("a\nb\nc");
    expect(rows[0].storage).toBe("ME");
    expect(toSmsRows([{ date: "broken" }])[0].dateText).toBe("-");
  });

  test("头像色相确定且在 comm 域色相区间 250–310", () => {
    expect(avatarHue("10086")).toBe(avatarHue("10086"));
    for (const name of ["10086", "+8613800138000", "阿里云", ""]) {
      const hue = avatarHue(name);
      expect(hue).toBeGreaterThanOrEqual(250);
      expect(hue).toBeLessThanOrEqual(310);
    }
  });
});

// ---------------------------------------------------------------------------
// 组件测试
// ---------------------------------------------------------------------------

function mockApis(): void {
  smsDataMock.mockImplementation((action: string) => {
    switch (action) {
      case "list":
      case "list_meta":
        return Promise.resolve(SAMPLE_LIST);
      case "sim_status":
        return Promise.resolve({ inserted: true });
      case "send":
        return Promise.resolve({ ok: true, segments: 1 });
      case "delete_indices":
        return Promise.resolve({ ok: true, deleted: 1, total: 1 });
      case "delete_all":
        return Promise.resolve({ ok: true, me: true, sm: true });
      default:
        return Promise.resolve({});
    }
  });
  atDataMock.mockResolvedValue({ ok: true, response: STORAGE_RAW });
  deviceInfoDataMock.mockResolvedValue({ imsi: IMSI_CN });
}

function renderPage(): void {
  render(
    <Providers>
      <SmsPage />
    </Providers>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockApis();
});

describe("SmsPage 组件", () => {
  test("收件箱渲染:发件人/摘要/时间/存储徽章/concat 标记", async () => {
    renderPage();
    expect(await screen.findByText("10086")).toBeInTheDocument();
    expect(screen.getByText("+8613800138000")).toBeInTheDocument();
    expect(screen.getByText("您的余额为 1 元")).toBeInTheDocument();
    expect(screen.getByText("24/03/15,08:30:00")).toBeInTheDocument();
    // concat 合并标记 n/m(3 段中已合并 1 段)
    expect(screen.getByText("1/3 段")).toBeInTheDocument();
    // 存储位置徽章 ME/SM
    expect(screen.getAllByText("ME").length).toBeGreaterThan(0);
    expect(screen.getAllByText("SM").length).toBeGreaterThan(0);
  });

  test("存储可视化条:ME/SM 占用与 SMSC 号码", async () => {
    renderPage();
    expect(await screen.findByText("205/255 条")).toBeInTheDocument();
    expect(screen.getByText("3/40 条")).toBeInTheDocument();
    expect(screen.getByText("+8613800210500")).toBeInTheDocument();
    expect(atDataMock).toHaveBeenCalledWith("manual_at", {
      command: SMS_STORAGE_AT_COMMAND,
    });
    // 防回归:分号后的后续命令不得再带 AT 前缀(3GPP TS 27.007 多命令行语法)
    expect(SMS_STORAGE_AT_COMMAND).not.toMatch(/;AT/);
  });

  test("多选删除:选中 → confirm 挂起 → 确认后 delete_indices 索引串 ME:2", async () => {
    renderPage();
    await screen.findByText("10086");
    fireEvent.click(screen.getByLabelText("选择短信 1"));
    const deleteButton = screen.getByRole("button", { name: /删除选中/ });
    expect(deleteButton).toBeEnabled();
    await act(async () => {
      fireEvent.click(deleteButton);
    });
    // confirm() 挂起:ConfirmDialog store 收到删除确认选项
    expect(useConfirmStore.getState().options?.title).toBe("删除短信");
    expect(smsDataMock).not.toHaveBeenCalledWith("delete_indices", expect.anything());
    await act(async () => {
      useConfirmStore.getState().settle(true);
    });
    await waitFor(() =>
      expect(smsDataMock).toHaveBeenCalledWith("delete_indices", { indices: "ME:2" }),
    );
  });

  test("多选删除:取消 confirm 不发请求", async () => {
    renderPage();
    await screen.findByText("10086");
    fireEvent.click(screen.getByLabelText("选择短信 1"));
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /删除选中/ }));
    });
    await act(async () => {
      useConfirmStore.getState().settle(false);
    });
    expect(smsDataMock).not.toHaveBeenCalledWith("delete_indices", expect.anything());
  });

  test("全选后删除走清空确认(对齐旧版 deleteSelected→confirmClearAll)", async () => {
    renderPage();
    await screen.findByText("10086");
    fireEvent.click(screen.getByRole("button", { name: "全选" }));
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /删除选中/ }));
    });
    expect(useConfirmStore.getState().options?.title).toBe("清空短信");
    await act(async () => {
      useConfirmStore.getState().settle(true);
    });
    await waitFor(() => expect(smsDataMock).toHaveBeenCalledWith("delete_all"));
  });

  test("发送表单:UTF-16 分段计数实时显示 + IMSI 归一化预览", async () => {
    renderPage();
    const numberInput = await screen.findByPlaceholderText("输入收件人号码");
    const messageInput = screen.getByPlaceholderText("输入短信内容");
    fireEvent.change(numberInput, { target: { value: "13800138000" } });
    expect(await screen.findByText(/将发送为：\+8613800138000/)).toBeInTheDocument();
    fireEvent.change(messageInput, { target: { value: "你".repeat(69) + "😀" } });
    expect(screen.getByText("71 码元 · 2 段（每段 ≤70）")).toBeInTheDocument();
  });

  test("发送表单:短号提示不补国家码", async () => {
    renderPage();
    const numberInput = await screen.findByPlaceholderText("输入收件人号码");
    fireEvent.change(numberInput, { target: { value: "10086" } });
    expect(await screen.findByText("客服/应急短号，不自动添加国家码")).toBeInTheDocument();
  });

});
