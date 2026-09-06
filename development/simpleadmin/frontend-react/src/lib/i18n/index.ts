import i18n from "i18next";
import { initReactI18next, useTranslation } from "react-i18next";

import manifest from "./locales/manifest.json";

export const LANGUAGE_STORAGE_KEY = "simpleadmin.language";

export type AppLanguage = "zh-CN" | "en";

type NsBag = Record<string, string>;

const localeModules = import.meta.glob<{ default: NsBag }>("./locales/*/*.json", {
  eager: true,
});

// import.meta.glob 键形如 "./locales/zh-CN/common.json",装配为 i18next resources
// 结构 { lng: { ns: bag } };manifest 之外的语言目录一律忽略。
function buildResources(): Record<AppLanguage, Record<string, NsBag>> {
  const resources: Record<AppLanguage, Record<string, NsBag>> = { "zh-CN": {}, en: {} };
  for (const [file, mod] of Object.entries(localeModules)) {
    const matched = /^\.\/locales\/([^/]+)\/([^/]+)\.json$/.exec(file);
    if (!matched) continue;
    const [, lng, ns] = matched;
    if (lng !== "zh-CN" && lng !== "en") continue;
    resources[lng][ns] = mod.default;
  }
  return resources;
}

export function normalizeLanguage(value: string | null | undefined): AppLanguage {
  if (!value) return "zh-CN";
  const v = value.toLowerCase();
  if (v.startsWith("zh")) return "zh-CN";
  if (v.startsWith("en")) return "en";
  return "zh-CN";
}

export function detectLanguage(): AppLanguage {
  try {
    const stored = localStorage.getItem(LANGUAGE_STORAGE_KEY);
    if (stored === "en" || stored === "zh-CN") return stored;
    return normalizeLanguage(stored);
  } catch {
    return "zh-CN";
  }
}

if (!i18n.isInitialized) {
  void i18n.use(initReactI18next).init({
    lng: detectLanguage(),
    fallbackLng: "zh-CN",
    defaultNS: manifest.defaultNamespace,
    ns: [...manifest.namespaces],
    resources: buildResources(),
    interpolation: { escapeValue: false },
  });
  if (typeof document !== "undefined") {
    document.documentElement.lang = i18n.language;
  }
}

/**
 * 切换界面语言:i18next 切换 + localStorage 持久化 + <html lang> 同步。
 *
 * 服务器端持久化不在本模块做——那需要 WS 网关客户端(等价于 Vue 版
 * SimpleAdmin.Lang.setLanguage(lang, { save: true }) 走 /api/set_language),
 * 由壳层代理在网关客户端就绪后自行调用服务端接口,本模块只负责浏览器侧。
 */
export async function changeLanguage(lang: AppLanguage): Promise<AppLanguage> {
  await i18n.changeLanguage(lang);
  try {
    localStorage.setItem(LANGUAGE_STORAGE_KEY, lang);
  } catch {
    // localStorage 不可用(隐私模式等)时仅内存生效,不阻断切换。
  }
  if (typeof document !== "undefined") {
    document.documentElement.lang = lang;
  }
  return lang;
}

/** useTranslation 便捷封装:默认落在 manifest.defaultNamespace(common)。 */
export function useT(ns?: string) {
  return useTranslation(ns ?? manifest.defaultNamespace);
}

export default i18n;
