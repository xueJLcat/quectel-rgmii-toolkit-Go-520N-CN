// atcommands 页测试:纯函数(transcript 200 行截断对齐命令边界 / 行分类 / 历史去重 ≤50 /
// ↑↓ 召回含草稿)+ 组件测试(发送、快捷命令、历史召回、分号透传、空输入不发、
// in-flight 禁用、AT&F 双确认链 → 成功才弹重启确认)。
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";

import "@/lib/i18n";
import { atData, systemData } from "@/lib/api";
import { useConfirmStore } from "@/stores/confirm";
import { useRebootStore } from "@/stores/reboot";

import AtcommandsPage from "./page";
import {
  appendTranscript,
  AT_HISTORY_KEY,
  classifyTranscriptLine,
  historyNext,
  historyPrev,
  INITIAL_HISTORY_CURSOR,
  parseAtHistory,
  pushAtHistory,
  QUICK_COMMANDS,
  removeAtHistoryEntry,
  TRANSCRIPT_MAX_LINES,
} from "./lib";
import type { HistoryCursor } from "./lib";

vi.mock("@/lib/api", () => ({
  atData: vi.fn(),
  systemData: vi.fn(),
}));

const atDataMock = vi.mocked(atData);
const systemDataMock = vi.mocked(systemData);

// ---------------------------------------------------------------------------
// 纯函数:transcript 截断(对齐旧版 appendOutput)
// ---------------------------------------------------------------------------

describe("appendTranscript", () => {
  test("条目格式:> 命令 + 应答 + 空行", () => {
    expect(appendTranscript("", "ATI", "Quectel\nOK\n")).toBe("> ATI\nQuectel\nOK\n\n");
    const second = appendTranscript("> ATI\nOK\n\n", "AT+CSQ", "+CSQ: 20,99\nOK\n");
    expect(second.startsWith("> ATI\nOK\n\n> AT+CSQ\n")).toBe(true);
  });

  test("超过 200 行从头截断,且截断点对齐 > 命令边界(应答不被拦腰截断)", () => {
    let transcript = "";
    for (let i = 0; i < 150; i += 1) {
      transcript = appendTranscript(transcript, `CMD${i}`, `R${i}`);
    }
    const lines = transcript.split("\n");
    expect(lines.length).toBeLessThanOrEqual(TRANSCRIPT_MAX_LINES);
    // 首行必须是命令行,且其应答紧随其后(同一编号成对保留)
    const matched = /^> CMD(\d+)$/.exec(lines[0]);
    expect(matched).not.toBeNull();
    expect(lines[1]).toBe(`R${matched?.[1]}`);
  });

  test("无命令边界可对齐时退回按行数硬截断(与旧版一致)", () => {
    const hugeResponse = Array.from({ length: 300 }, (_, i) => `line${i}`).join("\n");
    const transcript = appendTranscript("", "AT+BIG", hugeResponse);
    expect(transcript.split("\n").length).toBe(TRANSCRIPT_MAX_LINES);
  });

  test("200 行以内不截断", () => {
    let transcript = "";
    for (let i = 0; i < 10; i += 1) {
      transcript = appendTranscript(transcript, `CMD${i}`, `R${i}`);
    }
    // 每条 "> 命令 + 应答" 2 行,末尾一个换行 → split 后 21 段
    expect(transcript.split("\n").length).toBe(21);
  });
});

describe("classifyTranscriptLine", () => {
  test.each([
    ["> ATI", "command"],
    ["+CME ERROR: 10", "error"],
    ["+CMS ERROR: 350", "error"],
    ["ERROR", "error"],
    ["OK", "ok"],
    ["  OK  ", "ok"],
    ["Quectel RG520N-CN", "plain"],
    ["+CSQ: 20,99", "plain"],
  ])("%j → %s", (line, kind) => {
    expect(classifyTranscriptLine(line)).toBe(kind);
  });
});

// ---------------------------------------------------------------------------
// 纯函数:命令历史(localStorage 兼容旧格式,≤50 去重)
// ---------------------------------------------------------------------------

describe("parseAtHistory / pushAtHistory / removeAtHistoryEntry", () => {
  test("旧格式 JSON 字符串数组直接兼容", () => {
    expect(parseAtHistory('["ATI","AT+CSQ"]')).toEqual(["ATI", "AT+CSQ"]);
  });

  test("过滤非字符串/空白项,损坏数据回退空表", () => {
    expect(parseAtHistory('["ATI",42,"  ",""]')).toEqual(["ATI"]);
    expect(parseAtHistory("not-json")).toEqual([]);
    expect(parseAtHistory('{"a":1}')).toEqual([]);
    expect(parseAtHistory(null)).toEqual([]);
  });

  test("载入即截前 50", () => {
    const many = JSON.stringify(Array.from({ length: 60 }, (_, i) => `CMD${i}`));
    expect(parseAtHistory(many)).toHaveLength(50);
  });

  test("入栈去重:重复命令移到最前", () => {
    expect(pushAtHistory(["ATI", "AT+CSQ"], "AT+CSQ")).toEqual(["AT+CSQ", "ATI"]);
    expect(pushAtHistory(["ATI"], "AT+CIMI")).toEqual(["AT+CIMI", "ATI"]);
  });

  test("空/空白命令忽略", () => {
    expect(pushAtHistory(["ATI"], "  ")).toEqual(["ATI"]);
  });

  test("入栈上限 50", () => {
    let history: string[] = [];
    for (let i = 0; i < 60; i += 1) history = pushAtHistory(history, `CMD${i}`);
    expect(history).toHaveLength(50);
    expect(history[0]).toBe("CMD59");
  });

  test("单条删除", () => {
    expect(removeAtHistoryEntry(["ATI", "AT+CSQ"], "ATI")).toEqual(["AT+CSQ"]);
  });
});

// ---------------------------------------------------------------------------
// 纯函数:↑/↓ 历史召回(含草稿保存/恢复)
// ---------------------------------------------------------------------------

describe("historyPrev / historyNext", () => {
  const history = ["AT+CSQ", "ATI"]; // 新 → 旧

  test("上翻先存草稿,逐条向更旧移动,到最旧项停住", () => {
    let cursor: HistoryCursor = INITIAL_HISTORY_CURSOR;
    let input = "draft";
    ({ cursor, input } = historyPrev(cursor, history, input));
    expect(input).toBe("AT+CSQ");
    ({ cursor, input } = historyPrev(cursor, history, input));
    expect(input).toBe("ATI");
    ({ cursor, input } = historyPrev(cursor, history, input));
    expect(input).toBe("ATI");
    expect(cursor.index).toBe(1);
  });

  test("下翻回到底部恢复草稿并复位游标", () => {
    let cursor: HistoryCursor = INITIAL_HISTORY_CURSOR;
    let input = "draft";
    ({ cursor, input } = historyPrev(cursor, history, input));
    ({ cursor, input } = historyPrev(cursor, history, input));
    ({ cursor, input } = historyNext(cursor, history, input));
    expect(input).toBe("AT+CSQ");
    ({ cursor, input } = historyNext(cursor, history, input));
    expect(input).toBe("draft");
    expect(cursor).toEqual(INITIAL_HISTORY_CURSOR);
  });

  test("空历史/未激活游标不动", () => {
    expect(historyPrev(INITIAL_HISTORY_CURSOR, [], "x")).toEqual({
      cursor: INITIAL_HISTORY_CURSOR,
      input: "x",
    });
    expect(historyNext(INITIAL_HISTORY_CURSOR, history, "x")).toEqual({
      cursor: INITIAL_HISTORY_CURSOR,
      input: "x",
    });
  });
});

describe("QUICK_COMMANDS", () => {
  test("7 预设对齐设计方案", () => {
    expect(QUICK_COMMANDS.map((item) => item.cmd)).toEqual([
      "ATI",
      "AT+CSQ",
      'AT+QENG="servingcell"',
      "AT+QRSRP",
      "AT+QGMR",
      "AT+CIMI",
      'AT+QMAP="LANIP"',
    ]);
  });
});

// ---------------------------------------------------------------------------
// 组件测试
// ---------------------------------------------------------------------------

function renderPage(): void {
  render(<AtcommandsPage />);
}

function commandInput(): HTMLInputElement {
  return screen.getByLabelText("AT 命令输入") as HTMLInputElement;
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  atDataMock.mockResolvedValue({ ok: true, response: "OK" });
  systemDataMock.mockResolvedValue({ ok: true });
});

afterEach(() => {
  // 全局 store 复位,避免测试间串状态
  useConfirmStore.getState().settle(false);
  useRebootStore.getState().closeCountdown();
});

describe("AtcommandsPage 组件", () => {
  test("发送:manual_at 请求 → transcript 显示 > 命令与应答,历史落 localStorage", async () => {
    atDataMock.mockResolvedValue({ ok: true, response: "Quectel\nOK" });
    renderPage();
    fireEvent.change(commandInput(), { target: { value: "ATI" } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /发送/ }));
    });
    expect(atDataMock).toHaveBeenCalledWith("manual_at", { command: "ATI" });
    expect(await screen.findByText("> ATI")).toBeInTheDocument();
    expect(screen.getByText("Quectel")).toBeInTheDocument();
    // 命令行 cyan / OK 行 emerald(恒深色终端着色)
    expect(screen.getByText("> ATI").className).toContain("text-[#22d3ee]");
    expect(screen.getByText("OK").className).toContain("text-[#34d399]");
    expect(JSON.parse(localStorage.getItem(AT_HISTORY_KEY) ?? "[]")).toEqual(["ATI"]);
  });

  test("ERROR 应答行着 rose", async () => {
    atDataMock.mockResolvedValue({ ok: true, response: "+CME ERROR: 10" });
    renderPage();
    fireEvent.change(commandInput(), { target: { value: "AT+CPIN?" } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /发送/ }));
    });
    expect((await screen.findByText("+CME ERROR: 10")).className).toContain("text-[#fb7185]");
  });

  test("分号组合命令原样透传,空输入不发", async () => {
    renderPage();
    const sendButton = screen.getByRole("button", { name: /发送/ });
    expect(sendButton).toBeDisabled(); // 空输入禁用
    fireEvent.change(commandInput(), { target: { value: "AT+CFUN?;+CCID" } });
    await act(async () => {
      fireEvent.click(sendButton);
    });
    expect(atDataMock).toHaveBeenCalledWith("manual_at", { command: "AT+CFUN?;+CCID" });
  });

  test("快捷命令:点击回填输入行并发送", async () => {
    renderPage();
    await act(async () => {
      fireEvent.click(screen.getByText("AT+CSQ"));
    });
    expect(atDataMock).toHaveBeenCalledWith("manual_at", { command: "AT+CSQ" });
    expect(commandInput().value).toBe("AT+CSQ");
  });

  test("↑/↓ 历史召回:发送两条后上翻逐条召回,翻回底部恢复草稿", async () => {
    renderPage();
    await act(async () => {
      fireEvent.click(screen.getByText("ATI"));
    });
    await act(async () => {
      fireEvent.click(screen.getByText("AT+CSQ"));
    });
    // 快捷命令回填后输入行是 AT+CSQ;改成草稿再召回
    fireEvent.change(commandInput(), { target: { value: "草稿" } });
    fireEvent.keyDown(commandInput(), { key: "ArrowUp" });
    expect(commandInput().value).toBe("AT+CSQ");
    fireEvent.keyDown(commandInput(), { key: "ArrowUp" });
    expect(commandInput().value).toBe("ATI");
    fireEvent.keyDown(commandInput(), { key: "ArrowDown" });
    expect(commandInput().value).toBe("AT+CSQ");
    fireEvent.keyDown(commandInput(), { key: "ArrowDown" });
    expect(commandInput().value).toBe("草稿");
  });

  test("历史卡:点击复用回填,单条删除同步 localStorage", async () => {
    localStorage.setItem(AT_HISTORY_KEY, JSON.stringify(["ATI", "AT+CSQ"]));
    renderPage();
    // 快捷 chip 也有 ATI/AT+CSQ 文本,历史项经 aria-label 定位避免歧义
    await screen.findByLabelText("删除历史命令 ATI");
    fireEvent.click(screen.getByLabelText("删除历史命令 ATI"));
    await waitFor(() =>
      expect(JSON.parse(localStorage.getItem(AT_HISTORY_KEY) ?? "[]")).toEqual(["AT+CSQ"]),
    );
    fireEvent.click(screen.getByTitle("点击回填到输入行"));
    expect(commandInput().value).toBe("AT+CSQ");
  });

  test("in-flight 禁用:manual_at 未决期间发送/快捷/危险按钮全部禁用", async () => {
    let resolveAt!: (value: { ok: boolean; response?: string }) => void;
    atDataMock.mockReturnValue(
      new Promise((resolve) => {
        resolveAt = resolve;
      }),
    );
    renderPage();
    fireEvent.change(commandInput(), { target: { value: "ATI" } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /发送/ }));
    });
    expect(screen.getByRole("button", { name: /发送/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "重置 AT&F" })).toBeDisabled();
    expect(screen.getByText("AT+CSQ").closest("button")).toBeDisabled();
    await act(async () => {
      resolveAt({ ok: true, response: "OK" });
    });
    await waitFor(() => expect(screen.getByRole("button", { name: /发送/ })).toBeEnabled());
  });

  test("AT&F:危险确认 → 成功后才弹重启确认 → 确认后倒计时 + system_data reboot", async () => {
    renderPage();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "重置 AT&F" }));
    });
    expect(useConfirmStore.getState().options?.title).toBe("重置 AT&F");
    expect(useConfirmStore.getState().options?.danger).toBe(true);
    expect(atDataMock).not.toHaveBeenCalledWith("reset_at");
    await act(async () => {
      useConfirmStore.getState().settle(true);
    });
    await waitFor(() => expect(atDataMock).toHaveBeenCalledWith("reset_at"));
    // 成功 → 重启确认弹出
    await waitFor(() => expect(useConfirmStore.getState().options?.title).toBe("重启调制解调器"));
    await act(async () => {
      useConfirmStore.getState().settle(true);
    });
    await waitFor(() => expect(systemDataMock).toHaveBeenCalledWith("reboot"));
    expect(useRebootStore.getState().countdownActive).toBe(true);
  });

  test("AT&F:取消危险确认不发请求;reset 失败不弹重启确认", async () => {
    renderPage();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "重置 AT&F" }));
    });
    await act(async () => {
      useConfirmStore.getState().settle(false);
    });
    expect(atDataMock).not.toHaveBeenCalledWith("reset_at");

    atDataMock.mockResolvedValue({ ok: false, response: "ERROR" });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "重置 AT&F" }));
    });
    await act(async () => {
      useConfirmStore.getState().settle(true);
    });
    await waitFor(() => expect(atDataMock).toHaveBeenCalledWith("reset_at"));
    expect(useConfirmStore.getState().options).toBeNull();
    expect(systemDataMock).not.toHaveBeenCalled();
  });

  test("重启模块按钮:确认 → 倒计时;后端 ok:false 取消倒计时", async () => {
    systemDataMock.mockResolvedValue({ ok: false });
    renderPage();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "重启调制解调器" }));
    });
    expect(useConfirmStore.getState().options?.title).toBe("重启调制解调器");
    await act(async () => {
      useConfirmStore.getState().settle(true);
    });
    await waitFor(() => expect(systemDataMock).toHaveBeenCalledWith("reboot"));
    await waitFor(() => expect(useRebootStore.getState().countdownActive).toBe(false));
  });
});
