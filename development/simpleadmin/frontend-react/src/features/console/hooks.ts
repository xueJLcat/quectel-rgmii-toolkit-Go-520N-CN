// console 页 hooks:终端配色随主题联动。
// data-bs-theme 由 stores/ui(applyTheme)驱动,订阅 store 即等价于监听该属性变化;
// CSS 变量在主题切换后于同一帧生效,readCssVars 直接读 computed 值(jsdom 下为空串,
// terminalThemeFor 内部回退令牌默认值)。
import { useMemo } from "react";

import { useUiStore } from "@/stores/ui";

import { terminalThemeFor } from "./lib";
import type { CssVarBag, TerminalThemeColors } from "./lib";

const VAR_NAMES: Record<keyof CssVarBag, string> = {
  bg: "--sa-bg",
  surface: "--sa-surface",
  text: "--sa-text",
  textSoft: "--sa-text-soft",
  accent: "--sa-accent",
};

/** 读取 tokens.css 设计令牌当前计算值(不可用时为空串,由回退逻辑兜底)。 */
export function readCssVars(): CssVarBag {
  const bag = { bg: "", surface: "", text: "", textSoft: "", accent: "" };
  if (typeof document === "undefined" || typeof getComputedStyle !== "function") return bag;
  const style = getComputedStyle(document.documentElement);
  for (const key of Object.keys(VAR_NAMES) as (keyof CssVarBag)[]) {
    bag[key] = style.getPropertyValue(VAR_NAMES[key]).trim();
  }
  return bag;
}

/** 亮暗两套终端配色;主题 store 变化时重算(组件据此更新 term.options.theme)。 */
export function useTerminalTheme(): TerminalThemeColors {
  const mode = useUiStore((state) => state.theme);
  return useMemo(() => terminalThemeFor(mode, readCssVars()), [mode]);
}
