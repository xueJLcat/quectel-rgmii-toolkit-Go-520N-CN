// 主题工具(无 React 依赖):data-bs-theme + style.colorScheme 双写,与 index.html 首屏
// 防闪脚本同源——localStorage 键 "theme",值域 light/dark,非法值一律归 light。
// 服务端默认主题的持久化不在本模块(由壳层经 /api/set_theme 落盘),这里只管浏览器侧。

export type ThemeMode = "light" | "dark";

export const THEME_STORAGE_KEY = "theme";

function normalizeTheme(value: string | null | undefined): ThemeMode {
  return value === "dark" ? "dark" : "light";
}

/** 应用主题到 <html>:data-bs-theme(令牌切换)+ colorScheme(表单控件/滚动条随动)。 */
export function applyTheme(mode: ThemeMode): ThemeMode {
  if (typeof document === "undefined") return mode;
  const html = document.documentElement;
  html.setAttribute("data-bs-theme", mode);
  html.style.colorScheme = mode;
  return mode;
}

/** 读取当前生效主题(data-bs-theme),缺省 light。 */
export function readCurrentTheme(): ThemeMode {
  if (typeof document === "undefined") return "light";
  return normalizeTheme(document.documentElement.getAttribute("data-bs-theme"));
}

/** 读取 localStorage 持久化主题;无值/非法值/存储不可用返回 null。 */
export function readStoredTheme(): ThemeMode | null {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY);
    return stored === "dark" || stored === "light" ? stored : null;
  } catch {
    return null;
  }
}

/** 持久化主题到 localStorage;存储不可用(隐私模式等)时仅当前会话生效,不抛错。 */
export function storeTheme(mode: ThemeMode): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, mode);
  } catch {
    // 忽略:与 Vue 版 persistTheme 语义一致
  }
}
