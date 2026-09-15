// 面板进场 stagger(设计方案 §5.1):容器 staggerChildren 40ms,子项 fade + y8px 220ms,
// 缓动与壳层路由转场同源(App.tsx PAGE_ENTER)。三个监控域页面各自持有一份(目录所有权隔离)。
import type { Variants } from "motion/react";

export const STAGGER_CONTAINER: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.04 } },
};

export const STAGGER_ITEM: Variants = {
  hidden: { opacity: 0, y: 8 },
  show: { opacity: 1, y: 0, transition: { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] } },
};
