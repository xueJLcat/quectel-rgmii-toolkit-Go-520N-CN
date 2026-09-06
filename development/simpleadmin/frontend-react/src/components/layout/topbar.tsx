// Topbar(设计方案 §4.1):sticky 毛玻璃顶栏,收编旧版"每页复制 9 份"的全局控件。
// 左 = 移动端菜单钮(≤1024 可见)+ 当前页标题(路由匹配 PAGES);右 = 活力芯片区 + 操作区。
// 活力芯片:dashboardData 低频轮询 30s(仅网关连接时),pending/失败静默降级;
//   联网状态 StatusChip(已连接=success/未连接=danger)+ 信号迷你 5 格(质量色阶 §3.2)+ 运营商文本。
// 操作区:刷新(全量 invalidateQueries)、主题切换、语言切换、用户菜单(退出登录)。
// 网关断线时顶栏下方渲染细条提示(onStateChange 驱动)。
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { LogOutIcon, MenuIcon, MoonIcon, RefreshCwIcon, SunIcon, UserIcon } from "lucide-react";
import { useCallback, useSyncExternalStore } from "react";
import { useLocation } from "react-router-dom";

import { PAGES } from "@/app/routes";
import { signalQualityFromPercent } from "@/components/charts";
import type { SignalQuality } from "@/components/charts";
import { StatusChip } from "@/components/common";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { dashboardData, gateway } from "@/lib/api";
import type { GatewayState } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { useUiStore } from "@/stores/ui";
import { useLanguageSwitch, useLogout } from "./hooks";

/** 网关连接状态(React 绑定):useSyncExternalStore 订阅 gateway.onStateChange。 */
export function useGatewayState(): GatewayState {
  const subscribe = useCallback(
    (onStoreChange: () => void) => gateway.onStateChange(() => onStoreChange()),
    [],
  );
  const getSnapshot = useCallback(() => gateway.getState(), []);
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

// 信号迷你 5 格:格高递增,填充色 = 全站统一质量四档色阶(§3.2,阈值复用 charts 的
// signalQualityFromPercent:≥80 优秀 / ≥60 良好 / ≥40 一般 / 其余差)映射到语义令牌。
const BAR_HEIGHTS = ["h-1.5", "h-2", "h-2.5", "h-3", "h-3.5"];
const QUALITY_BAR_CLASS: Record<SignalQuality, string> = {
  excellent: "bg-success",
  good: "bg-info",
  fair: "bg-warning",
  poor: "bg-danger",
};

export interface SignalBarsProps {
  percentage?: number;
  className?: string;
}

export function SignalBars({ percentage, className }: SignalBarsProps) {
  const pct =
    typeof percentage === "number" && Number.isFinite(percentage)
      ? Math.max(0, Math.min(100, percentage))
      : 0;
  const filled = Math.min(5, Math.ceil(pct / 20));
  const color = QUALITY_BAR_CLASS[signalQualityFromPercent(pct)];
  return (
    <span
      data-slot="signal-bars"
      role="img"
      aria-label={`${Math.round(pct)}%`}
      className={cn("flex items-end gap-0.5", className)}
    >
      {BAR_HEIGHTS.map((height, index) => (
        <span
          key={height}
          data-slot="signal-bar"
          data-filled={index < filled}
          className={cn("w-1 rounded-sm", height, index < filled ? color : "bg-surface-3")}
        />
      ))}
    </span>
  );
}

function VitalChips() {
  const { t } = useT("dashboard");
  const connected = useGatewayState() === "connected";
  const { data } = useQuery({
    queryKey: ["dashboardData"],
    queryFn: () => dashboardData(),
    enabled: connected,
    refetchInterval: connected ? 30_000 : false,
  });
  // pending/失败静默降级:不渲染芯片区
  if (!data) return null;
  // 服务端 dashboard_data 的 internetConnection 为固定中文枚举(page_dashboard.go:已连接/未连接)
  const online = data.internetConnection === "已连接";
  return (
    <div data-slot="vital-chips" className="flex min-w-0 items-center gap-2">
      <StatusChip tone={online ? "success" : "danger"} pulse={online}>
        {online ? t("connected") : t("disconnected")}
      </StatusChip>
      <SignalBars percentage={data.signalPercentage} />
      {data.network_provider && (
        <span className="hidden max-w-28 truncate text-xs text-muted md:inline">
          {data.network_provider}
        </span>
      )}
    </div>
  );
}

export function Topbar() {
  const { t } = useT("nav");
  const { t: tc } = useT();
  const { t: ts } = useT("settings");
  const { pathname } = useLocation();
  const page = PAGES.find((item) => item.path === pathname);
  const setMobileNavOpen = useUiStore((state) => state.setMobileNavOpen);
  const theme = useUiStore((state) => state.theme);
  const toggleTheme = useUiStore((state) => state.toggleTheme);
  const { language, toggle: toggleLanguage } = useLanguageSwitch();
  const logoutAction = useLogout();
  const queryClient = useQueryClient();
  const gatewayState = useGatewayState();
  const ThemeIcon = theme === "dark" ? SunIcon : MoonIcon;
  return (
    <>
      <header
        data-slot="topbar"
        className="sticky top-0 z-40 border-b border-line bg-surface/80 backdrop-blur"
      >
        <div className="flex h-14 items-center gap-2 px-4 lg:px-6">
          <Button
            variant="ghost"
            size="icon"
            className="shrink-0 lg:hidden"
            aria-label={t("menu")}
            onClick={() => setMobileNavOpen(true)}
          >
            <MenuIcon />
          </Button>
          <h2 data-slot="topbar-title" className="truncate text-base font-semibold text-ink">
            {page ? t(page.titleKey) : "SimpleAdmin"}
          </h2>
          <div className="ml-auto flex min-w-0 items-center gap-1 sm:gap-2">
            <VitalChips />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={tc("refresh")}
                  onClick={() => void queryClient.invalidateQueries()}
                >
                  <RefreshCwIcon />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{tc("refresh")}</TooltipContent>
            </Tooltip>
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
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" aria-label={tc("actions")}>
                  <UserIcon />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem variant="destructive" onSelect={() => void logoutAction()}>
                  <LogOutIcon />
                  {t("logOut")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>
      {gatewayState !== "connected" && (
        <div
          role="status"
          data-slot="gateway-strip"
          className="border-b border-warning/30 bg-warning-soft px-4 py-1 text-center text-xs text-warning"
        >
          {tc("networkDisconnected")}
        </div>
      )}
    </>
  );
}
