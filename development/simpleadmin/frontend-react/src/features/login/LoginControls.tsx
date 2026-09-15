import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Languages, Moon, Sun } from "lucide-react";

import { Button } from "@/components/ui/button";
import { changeLanguage } from "@/lib/i18n";

type ThemeMode = "light" | "dark";

const THEME_STORAGE_KEY = "theme";

/**
 * 主题仅在本 feature 内做极简处理(src/lib/theme 尚不存在):读取
 * document.documentElement 的 data-bs-theme 决定配色,切换时同步
 * colorScheme 与 localStorage "theme"(与 login.html 防闪脚本同一键位),
 * 不做服务端持久化。
 */
function readTheme(): ThemeMode {
  return document.documentElement.getAttribute("data-bs-theme") === "dark" ? "dark" : "light";
}

/** 登录页右上角小控件:语言切换(zh/en)+ 主题切换(明/暗)。 */
export function LoginControls() {
  const { i18n } = useTranslation("login");
  const [theme, setTheme] = useState<ThemeMode>(readTheme);

  const isEn = i18n.language.toLowerCase().startsWith("en");

  function toggleTheme(): void {
    const next: ThemeMode = theme === "dark" ? "light" : "dark";
    document.documentElement.setAttribute("data-bs-theme", next);
    document.documentElement.style.colorScheme = next;
    try {
      localStorage.setItem(THEME_STORAGE_KEY, next);
    } catch {
      // localStorage 不可用时仅内存生效
    }
    setTheme(next);
  }

  return (
    <div className="flex items-center gap-1">
      <Button
        variant="ghost"
        size="sm"
        aria-label="Switch language"
        onClick={() => void changeLanguage(isEn ? "zh-CN" : "en")}
      >
        <Languages aria-hidden />
        {isEn ? "中文" : "EN"}
      </Button>
      <Button variant="ghost" size="icon" aria-label="Switch theme" onClick={toggleTheme}>
        {theme === "dark" ? <Sun aria-hidden /> : <Moon aria-hidden />}
      </Button>
    </div>
  );
}
