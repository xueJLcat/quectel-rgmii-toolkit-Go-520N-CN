import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";

import "@/lib/i18n";
import LoginApp from "@/app/LoginApp";
import { LoginForm } from "@/features/login/LoginForm";
import { sanitizeNext } from "@/features/login/sanitizeNext";
import { fetchModuleModel, login } from "@/lib/api/auth";

vi.mock("@/lib/api/auth", () => ({
  login: vi.fn(),
  logout: vi.fn(),
  fetchModuleModel: vi.fn(),
}));

const loginMock = vi.mocked(login);
const fetchModuleModelMock = vi.mocked(fetchModuleModel);

function fillCredentials(username = "admin", password = "secret"): void {
  fireEvent.change(screen.getByLabelText("用户名"), { target: { value: username } });
  fireEvent.change(screen.getByLabelText("密码"), { target: { value: password } });
}

beforeEach(() => {
  vi.clearAllMocks();
  sessionStorage.clear();
  localStorage.clear();
  fetchModuleModelMock.mockResolvedValue({ model: "RG520N-CN" });
});

describe("sanitizeNext", () => {
  test.each([
    ["/", "/"],
    ["/#dashboard", "/#dashboard"],
    ["#x", "/#x"],
    ["//evil.com", "/"],
    ["/\\evil", "/"],
    ["http://x", "/"],
    ["", "/"],
  ])("sanitizeNext(%j) === %j", (input, expected) => {
    expect(sanitizeNext(input)).toBe(expected);
  });
});

describe("LoginApp 渲染", () => {
  test("表单、品牌型号与语言切换按钮出现", async () => {
    render(<LoginApp />);
    expect(screen.getByLabelText("用户名")).toBeInTheDocument();
    expect(screen.getByLabelText("密码")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "登录" })).toBeInTheDocument();
    expect(await screen.findAllByText("RG520N-CN")).not.toHaveLength(0);
    expect(fetchModuleModelMock).toHaveBeenCalled();
    expect(screen.getByRole("button", { name: /switch language/i })).toBeInTheDocument();
  });

  test("fetchModuleModel 失败时静默回退硬编码型号", async () => {
    fetchModuleModelMock.mockRejectedValue(new Error("offline"));
    render(<LoginApp />);
    expect((await screen.findAllByText("RG520N-CN")).length).toBeGreaterThan(0);
  });
});

describe("LoginForm 提交", () => {
  test("成功后消费 sessionStorage hash 并经 navigate 恢复跳转", async () => {
    loginMock.mockResolvedValue({ ok: true, redirect: "/" });
    sessionStorage.setItem("simpleadmin.postLoginHash", "#dashboard");
    const navigate = vi.fn();
    render(<LoginForm navigate={navigate} />);
    fillCredentials();
    fireEvent.click(screen.getByRole("button", { name: "登录" }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/#dashboard"));
    expect(loginMock).toHaveBeenCalledWith("admin", "secret");
    expect(sessionStorage.getItem("simpleadmin.postLoginHash")).toBeNull();
  });

  test("401 失败:错误文案出现在 aria-live 区域", async () => {
    loginMock.mockResolvedValue({ ok: false, error: "invalid credentials" });
    render(<LoginForm />);
    fillCredentials();
    fireEvent.click(screen.getByRole("button", { name: "登录" }));
    const alert = screen.getByRole("alert");
    await waitFor(() => expect(alert).toHaveTextContent("用户名或密码错误"));
    expect(alert).toHaveAttribute("aria-live", "assertive");
    expect(screen.getByRole("button", { name: "登录" })).toBeEnabled();
  });

  test("429 retry_after=3:倒计时文案出现,3 秒后按钮恢复可用", async () => {
    vi.useFakeTimers();
    try {
      loginMock.mockResolvedValue({
        ok: false,
        error: "too many failed attempts",
        retry_after: 3,
      });
      render(<LoginForm />);
      fillCredentials();
      await act(async () => {
        fireEvent.click(screen.getByRole("button", { name: "登录" }));
      });
      // login ns 无 {{seconds}} 插值词条,按现有词条拼接:"尝试次数过多，请 " + N + "秒后重试"
      expect(screen.getByRole("alert")).toHaveTextContent("尝试次数过多，请 3秒后重试");
      const lockedButton = screen.getByRole("button", { name: "3秒后可重试" });
      expect(lockedButton).toBeDisabled();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(3000);
      });
      const restored = screen.getByRole("button", { name: "登录" });
      expect(restored).toBeEnabled();
      expect(screen.getByRole("alert")).toHaveTextContent("");
    } finally {
      vi.useRealTimers();
    }
  });
});
