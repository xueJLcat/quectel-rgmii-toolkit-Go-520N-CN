// 全局 Providers:数据层 + 动效纪律 + toast 单一装配点(App/LoginApp 之外的壳层共用)。
// TanStack Query 默认口径:retry 8 次指数退避封顶 8×基线(1s→2s→4s→8s→8s…)、
// 窗口聚焦不重拉、staleTime 2s;MotionConfig reducedMotion="user"(§5.1:prefers-reduced-motion
// 自动降级纯 opacity);sonner Toaster 不用 richColors,经主题令牌类着色(bg-surface/text-ink/border-line)。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MotionConfig } from "motion/react";
import { useMemo } from "react";
import type { ReactNode } from "react";
import { Toaster } from "sonner";

export interface ProvidersProps {
  children: ReactNode;
}

export function Providers({ children }: ProvidersProps) {
  // 每个 Providers 实例一份 client(useMemo):避免模块级单例在测试/HMR 间串缓存
  const queryClient = useMemo(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            retry: 8,
            retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 8000),
            refetchOnWindowFocus: false,
            staleTime: 2000,
          },
        },
      }),
    [],
  );
  return (
    <QueryClientProvider client={queryClient}>
      <MotionConfig reducedMotion="user">
        {children}
        <Toaster
          position="top-right"
          visibleToasts={3}
          duration={4000}
          toastOptions={{ className: "border-line! bg-surface! text-ink!" }}
        />
      </MotionConfig>
    </QueryClientProvider>
  );
}
