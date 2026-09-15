// 界面偏好卡:语言(zh-CN/en,即时生效:i18n changeLanguage + localStorage + languageSet
// 服务端持久化,失败降级 localOnly warning toast)+ 设备默认主题(light/dark,先本地生效再
// themeSet 写设备默认值,失败同样降级)。文案与顶栏"仅本机"切换明确区分(§6.15)。
import { LanguagesIcon, LoaderCircleIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { languageSet, themeSet } from "@/lib/api";
import { changeLanguage, normalizeLanguage, useT } from "@/lib/i18n";
import type { AppLanguage } from "@/lib/i18n";
import type { ThemeMode } from "@/lib/theme";
import { useUiStore } from "@/stores/ui";

import { useDeviceTheme } from "../hooks";

export function PreferencesCard() {
  const { t, i18n } = useT("settings");
  const themeQuery = useDeviceTheme();
  const setLocalTheme = useUiStore((state) => state.setTheme);
  const language = normalizeLanguage(i18n.language);
  const [selectedTheme, setSelectedTheme] = useState<ThemeMode | null>(null);
  const [savingLanguage, setSavingLanguage] = useState(false);
  const [savingTheme, setSavingTheme] = useState(false);

  const themeValue: ThemeMode = selectedTheme ?? themeQuery.data ?? "light";

  async function handleLanguageChange(next: string): Promise<void> {
    const lang: AppLanguage = next === "en" ? "en" : "zh-CN";
    // 浏览器侧先行(i18next + localStorage + <html lang>),再服务端持久化
    await changeLanguage(lang);
    setSavingLanguage(true);
    try {
      await languageSet(lang);
      toast.success(t("languageSaved"));
    } catch {
      // 降级 localOnly(对齐旧 Lang.save 语义):已本地生效,服务器保存失败
      toast.warning(t("appliedLocallyButSavingOnTheServerFailed"));
    } finally {
      setSavingLanguage(false);
    }
  }

  async function handleSaveTheme(): Promise<void> {
    // 先本地生效(当前浏览器立即切换并记住),再保存为设备默认值(对齐旧 saveThemeSetting)
    setLocalTheme(themeValue);
    setSavingTheme(true);
    try {
      const data = await themeSet(themeValue);
      if (data?.theme === "dark" || data?.theme === "light") setSelectedTheme(data.theme);
      toast.success(t("common:saved"));
      void themeQuery.refetch();
    } catch {
      toast.warning(t("appliedLocallyButSavingOnTheServerFailed"));
    } finally {
      setSavingTheme(false);
    }
  }

  return (
    <Panel domain="system" icon={LanguagesIcon} title={t("interfacePreferences")}>
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-3">
          <Label className="w-36 shrink-0" id="language-label">
            {t("interfaceLanguage")}
          </Label>
          <Select
            value={language}
            onValueChange={(value) => void handleLanguageChange(value)}
            disabled={savingLanguage}
          >
            <SelectTrigger aria-labelledby="language-label" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="zh-CN">{t("chinese")}</SelectItem>
              <SelectItem value="en">{t("english")}</SelectItem>
            </SelectContent>
          </Select>
          {savingLanguage && <LoaderCircleIcon className="animate-spin text-muted" />}
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Label className="w-36 shrink-0" id="device-theme-label">
            {t("deviceDefaultTheme")}
          </Label>
          <Select
            value={themeValue}
            onValueChange={(value) => setSelectedTheme(value === "dark" ? "dark" : "light")}
            disabled={savingTheme || themeQuery.isPending}
          >
            <SelectTrigger aria-labelledby="device-theme-label" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="light">{t("lightMode")}</SelectItem>
              <SelectItem value="dark">{t("nightMode")}</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void handleSaveTheme()}
            disabled={savingTheme || themeQuery.isPending}
          >
            {savingTheme && <LoaderCircleIcon className="animate-spin" />}
            {t("common:save")}
          </Button>
        </div>
        <p className="text-sm text-muted">
          {t(
            "theToggleButtonAtTheTopOnlyAffectsTheCurrentBrowserThisDefaultAppliesToNewBrowsersOrAfterClearingTheCache",
          )}
        </p>
      </div>
    </Panel>
  );
}
