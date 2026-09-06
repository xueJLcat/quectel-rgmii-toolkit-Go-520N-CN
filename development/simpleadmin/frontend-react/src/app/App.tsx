// App(设计方案 §4.1 壳层装配):Providers → HashRouter → Sidebar/MobileDrawer/Topbar + 主内容区。
// 职责:WS 网关生命周期(挂载 connect+startKeepalive,卸载 close+stop)、document.title 随路由+语言
// 同步(格式对齐旧版 Brand.setPageTitle:"页面标题 - 型号")、AnimatePresence 路由转场(fade+y8px
// 220ms,出场 120ms,§5.1)、PAGES 懒加载 + Suspense 骨架屏、"*" 兜底重定向 /dashboard;
// ConfirmDialog/RebootOverlay 为全局单例,在此挂载一次(stores 驱动)。
import { AnimatePresence, motion } from "motion/react";
import type { Transition } from "motion/react";
import { lazy, Suspense, useEffect } from "react";
import { HashRouter, Navigate, Route, Routes, useLocation } from "react-router-dom";

import { Providers } from "@/app/providers";
import { PAGES } from "@/app/routes";
import { ConfirmDialog, RebootOverlay } from "@/components/common";
import { useBrandModel } from "@/components/layout/hooks";
import { MobileDrawer } from "@/components/layout/mobile-drawer";
import { Sidebar } from "@/components/layout/sidebar";
import { Topbar } from "@/components/layout/topbar";
import { Skeleton } from "@/components/ui/skeleton";
import { gateway } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { useUiStore } from "@/stores/ui";

// lazy 组件在模块级一次性创建:避免 render 内反复 lazy() 生成新组件类型导致整页重挂载
const ROUTE_ENTRIES = PAGES.map((page) => ({ page, Component: lazy(page.lazy) }));

// 路由转场(§5.1):入场 fade + translateY 8px 220ms ease-out,旧页 120ms 淡出
const PAGE_ENTER: Transition = { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] };
const PAGE_EXIT: Transition = { duration: 0.12 };

function PageSkeleton() {
  return (
    <div data-slot="page-skeleton" aria-busy="true" className="space-y-4">
      <Skeleton className="h-8 w-56" />
      <Skeleton className="h-72 w-full rounded-md" />
    </div>
  );
}

function Shell() {
  const location = useLocation();
  const { t } = useT("nav");
  const collapsed = useUiStore((state) => state.sidebarCollapsed);
  const brand = useBrandModel();

  // WS 网关生命周期:首连失败由 gateway 内部退避重连接管(onclose → scheduleReconnect)
  useEffect(() => {
    gateway.connect().catch(() => {
      /* 首连失败静默,重连由 gateway 自治 */
    });
    gateway.startKeepalive();
    return () => {
      gateway.stopKeepalive();
      gateway.close();
    };
  }, []);

  // document.title = "页面标题 - 型号";语言切换时 t() 输出变化 → title 随动
  const page = PAGES.find((item) => item.path === location.pathname);
  const title = page ? t(page.titleKey) : "";
  useEffect(() => {
    document.title = title === "" ? brand : `${title} - ${brand}`;
  }, [title, brand]);

  return (
    <div className="min-h-svh bg-bg text-ink">
      <Sidebar />
      <MobileDrawer />
      <div
        className={cn(
          "flex min-h-svh flex-col transition-[padding] duration-[var(--sa-dur)] ease-sa",
          collapsed
            ? "lg:pl-[var(--sa-sidebar-collapsed-width)]"
            : "lg:pl-[var(--sa-sidebar-width)]",
        )}
      >
        <Topbar />
        <main className="mx-auto w-full max-w-[1440px] flex-1 p-4 lg:p-6">
          <AnimatePresence mode="wait" initial={false}>
            <motion.div
              key={location.pathname}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0, transition: PAGE_ENTER }}
              exit={{ opacity: 0, transition: PAGE_EXIT }}
            >
              <Routes location={location}>
                {ROUTE_ENTRIES.map(({ page: item, Component }) => (
                  <Route
                    key={item.id}
                    path={item.path}
                    element={
                      <Suspense fallback={<PageSkeleton />}>
                        <Component />
                      </Suspense>
                    }
                  />
                ))}
                <Route path="*" element={<Navigate to="/dashboard" replace />} />
              </Routes>
            </motion.div>
          </AnimatePresence>
        </main>
      </div>
      <ConfirmDialog />
      <RebootOverlay />
    </div>
  );
}

export default function App() {
  return (
    <Providers>
      <HashRouter>
        <Shell />
      </HashRouter>
    </Providers>
  );
}
