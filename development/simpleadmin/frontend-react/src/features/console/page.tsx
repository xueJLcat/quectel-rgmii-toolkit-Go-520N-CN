// 控制台页(域色 tools,设计方案 §6.11):xterm.js 原生终端,懒连接(路由首次挂载才建 WS,
// 页面本身经 routes.ts lazy 分包,xterm 由 vite manualChunks 独立 chunk)。
// 面板进场 stagger 40ms/项(§5.1)。
import { motion } from "motion/react";

import { PageHeader } from "@/components/layout/page-header";
import { useT } from "@/lib/i18n";

import { ConsoleTerminal } from "./components/ConsoleTerminal";

export default function ConsolePage() {
  const { t } = useT("console");
  return (
    <>
      <PageHeader domain="tools" title={t("nav:console")} description={t("pageDescription")} />
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.22, ease: [0.22, 0.7, 0.28, 1] }}
      >
        <ConsoleTerminal />
      </motion.div>
    </>
  );
}
