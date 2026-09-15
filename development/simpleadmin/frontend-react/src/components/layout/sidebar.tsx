// Sidebar(设计方案 §4.1):桌面固定左侧 w-64(折叠 w-[76px],读 ui store),≤1024px 隐藏(MobileDrawer 接管)。
// 品牌区 = 型号(/api/module_model,失败回退 RG520N-CN)+ 副标题 SimpleAdmin;
// 六组导航项 = 域色渐变圆角方块图标 + 标题,激活项 = 域色 soft 底 + motion layoutId 滑动指示条
// (spring 400/30,§5.1)+ aria-current="page"(NavLink 自带);折叠态只留图标 + Tooltip。
// 底部:主题切换(Sun/Moon)、语言切换(中/EN)、退出登录(ConfirmDialog 确认)。
// NavContent 为共享导出,MobileDrawer 复用同一份导航树。
import { ChevronsLeftIcon, ChevronsRightIcon, LogOutIcon, MoonIcon, SunIcon } from "lucide-react";
import { motion } from "motion/react";
import type { Transition } from "motion/react";
import { NavLink, useLocation } from "react-router-dom";

import { NAV_GROUPS, PAGES } from "@/app/routes";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { useUiStore } from "@/stores/ui";
import { DOMAIN_GRADIENT, DOMAIN_SOFT_CLASS } from "./panel";
import { useBrandModel, useLanguageSwitch, useLogout } from "./hooks";

// 导航指示条 spring(§5.1:layoutId spring stiffness 400 / damping 30)
const INDICATOR_SPRING: Transition = { type: "spring", stiffness: 400, damping: 30 };

/** 品牌缩写标记(对齐旧版 markFromName:前 2 个字母数字/汉字大写)。 */
function brandMark(model: string): string {
  const chars = Array.from(model.replace(/[^0-9A-Za-z\u4e00-\u9fa5]/g, ""));
  return chars.length === 0 ? "--" : chars.slice(0, 2).join("").toUpperCase();
}

export interface NavContentProps {
  /** 折叠态:只留图标 + Tooltip,隐藏组标题与页面名 */
  collapsed?: boolean;
  /** 导航点击回调(MobileDrawer 用于关闭抽屉) */
  onNavigate?: () => void;
}

export function NavContent({ collapsed = false, onNavigate }: NavContentProps) {
  const { t } = useT("nav");
  const { pathname } = useLocation();
  return (
    <nav aria-label={t("mainNavigation")} className="flex-1 overflow-y-auto px-3 py-3">
      {NAV_GROUPS.map((group) => (
        <div key={group.id} className="mb-3 last:mb-0">
          {!collapsed && (
            <div data-slot="nav-group-title" className="px-2 pb-1 text-xs font-semibold text-muted">
              {t(group.titleKey)}
            </div>
          )}
          <ul className="space-y-0.5">
            {PAGES.filter((page) => page.groupId === group.id).map((page) => {
              const active = pathname === page.path;
              const Icon = page.icon;
              const link = (
                <NavLink
                  to={page.path}
                  onClick={onNavigate}
                  className={cn(
                    "relative flex items-center gap-3 rounded-sm px-2 py-1.5 text-sm transition-colors duration-[var(--sa-dur-fast)] ease-sa",
                    active
                      ? cn("font-semibold text-ink", DOMAIN_SOFT_CLASS[group.id])
                      : "text-soft hover:bg-surface-2 hover:text-ink",
                  )}
                >
                  {active && (
                    <motion.span
                      layoutId="nav-indicator"
                      transition={INDICATOR_SPRING}
                      aria-hidden="true"
                      style={{ background: DOMAIN_GRADIENT[group.id] }}
                      className="absolute top-1/2 left-0 h-5 w-1 -translate-y-1/2 rounded-full"
                    />
                  )}
                  <span
                    aria-hidden="true"
                    style={{ background: DOMAIN_GRADIENT[group.id] }}
                    className="grid size-7 shrink-0 place-items-center rounded-sm text-white shadow-sm"
                  >
                    <Icon className="size-4" />
                  </span>
                  {!collapsed && <span className="truncate">{t(page.titleKey)}</span>}
                </NavLink>
              );
              return (
                <li key={page.id}>
                  {collapsed ? (
                    <Tooltip>
                      <TooltipTrigger asChild>{link}</TooltipTrigger>
                      <TooltipContent side="right">{t(page.titleKey)}</TooltipContent>
                    </Tooltip>
                  ) : (
                    link
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      ))}
    </nav>
  );
}

function SidebarFooter({ collapsed }: { collapsed: boolean }) {
  const theme = useUiStore((state) => state.theme);
  const toggleTheme = useUiStore((state) => state.toggleTheme);
  const { language, toggle: toggleLanguage } = useLanguageSwitch();
  const logoutAction = useLogout();
  const { t } = useT("nav");
  const { t: ts } = useT("settings");
  const ThemeIcon = theme === "dark" ? SunIcon : MoonIcon;
  return (
    <footer
      data-slot="sidebar-footer"
      className={cn(
        "flex gap-1 border-t border-line p-3",
        collapsed ? "flex-col items-center" : "items-center justify-between",
      )}
    >
      <Button
        variant="ghost"
        size="icon"
        onClick={toggleTheme}
        aria-label={ts("nightMode")}
        title={ts("nightMode")}
      >
        <ThemeIcon />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        onClick={toggleLanguage}
        title={language === "en" ? "中文" : "English"}
        className="text-xs font-semibold"
      >
        {language === "en" ? "EN" : "中"}
      </Button>
      <Button
        variant="ghost"
        size="icon"
        onClick={() => void logoutAction()}
        aria-label={t("logOut")}
        title={t("logOut")}
        className="text-danger hover:bg-danger-soft hover:text-danger"
      >
        <LogOutIcon />
      </Button>
    </footer>
  );
}

export function Sidebar() {
  const collapsed = useUiStore((state) => state.sidebarCollapsed);
  const toggleSidebar = useUiStore((state) => state.toggleSidebar);
  const model = useBrandModel();
  const { t } = useT("nav");
  return (
    <aside
      data-slot="sidebar"
      aria-label="SimpleAdmin"
      className={cn(
        "fixed inset-y-0 left-0 z-40 hidden flex-col border-r border-line bg-surface transition-[width] duration-[var(--sa-dur)] ease-sa lg:flex",
        collapsed ? "w-[var(--sa-sidebar-collapsed-width)]" : "w-[var(--sa-sidebar-width)]",
      )}
    >
      <div
        className={cn(
          "flex items-center gap-2 px-4 py-4",
          collapsed && "flex-col justify-center gap-2 px-2",
        )}
      >
        <div className={cn("flex min-w-0 items-center gap-3", collapsed && "justify-center")}>
          <span
            aria-hidden="true"
            className="grid size-10 shrink-0 place-items-center rounded-md bg-[image:var(--sa-gradient)] text-sm font-bold text-white shadow-md"
          >
            {brandMark(model)}
          </span>
          {!collapsed && (
            <div className="min-w-0">
              <div data-slot="sidebar-model" className="truncate text-base font-semibold text-ink">
                {model}
              </div>
              <div className="truncate text-xs text-muted">SimpleAdmin</div>
            </div>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={toggleSidebar}
          aria-label={t("toggleNavigation")}
          title={t("toggleNavigation")}
          className={cn("shrink-0", !collapsed && "ml-auto")}
        >
          {collapsed ? <ChevronsRightIcon /> : <ChevronsLeftIcon />}
        </Button>
      </div>
      <Separator />
      <NavContent collapsed={collapsed} />
      <SidebarFooter collapsed={collapsed} />
    </aside>
  );
}
