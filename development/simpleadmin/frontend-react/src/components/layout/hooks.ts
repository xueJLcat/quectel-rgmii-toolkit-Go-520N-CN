// 壳层共享动作 hooks:品牌型号(TanStack Query + 硬编码回退)、语言切换(浏览器侧 + 服务端持久化)、
// 退出登录(ConfirmDialog 确认 → auth.logout → 跳登录页)。Sidebar 与 Topbar 共用,单一实现。
import { useQuery } from "@tanstack/react-query";
import { useCallback } from "react";
import { toast } from "sonner";

import { languageSet, logout, moduleModel } from "@/lib/api";
import { changeLanguage, normalizeLanguage, useT } from "@/lib/i18n";
import type { AppLanguage } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

/** 型号拉取失败/未就绪时的回退值(与旧版 simpleadmin-brand.js MODEL_NAME 一致)。 */
export const DEFAULT_BRAND_MODEL = "RG520N-CN";

/**
 * 品牌型号:GET /api/module_model(公开端点,不走 WS 网关)。
 * pending/失败一律静默回退 DEFAULT_BRAND_MODEL;queryKey 全站共享(App 的 document.title 同源)。
 */
export function useBrandModel(): string {
  const { data } = useQuery({
    queryKey: ["moduleModel"],
    queryFn: () => moduleModel(),
    staleTime: Infinity,
    retry: 1,
  });
  const model = typeof data?.model === "string" ? data.model.trim() : "";
  return model === "" ? DEFAULT_BRAND_MODEL : model;
}

/**
 * 语言切换:zh-CN ↔ en。浏览器侧(changeLanguage:i18next + localStorage + <html lang>)先行,
 * 服务端持久化 /api/set_language best-effort;失败降级 localOnly 并 toast(对齐旧版 Lang.save 语义)。
 */
export function useLanguageSwitch() {
  const { i18n } = useT();
  const language = normalizeLanguage(i18n.language);
  const toggle = useCallback(() => {
    const next: AppLanguage = language === "en" ? "zh-CN" : "en";
    void changeLanguage(next);
    languageSet(next).catch(() => {
      toast.error(i18n.t("common:saveFailed"));
    });
  }, [language, i18n]);
  return { language, toggle };
}

/**
 * 退出登录:ConfirmDialog 确认 → POST /api/logout(best-effort)→ location.assign("/login.html")。
 * 服务端注销失败也跳登录页(会话由后端自行过期),对齐旧版 simpleadmin-logout.js。
 */
export function useLogout() {
  const confirm = useConfirmStore((state) => state.confirm);
  const { t } = useT("nav");
  const { t: tc } = useT();
  return useCallback(async () => {
    const ok = await confirm({ title: t("logOut"), message: tc("continue"), danger: true });
    if (!ok) return;
    try {
      await logout();
    } catch {
      // 注销请求失败不阻塞跳转
    }
    location.assign("/login.html");
  }, [confirm, t, tc]);
}
