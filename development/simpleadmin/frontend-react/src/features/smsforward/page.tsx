// 短信转发页(域色 comm):Webhook 卡(自短信服务页迁入并扩展请求方式/超时/
// 请求头/JSON 载荷模板)+ Server酱 推送卡(sct.ftqq.com,SendKey 自动识别
// Turbo 与 Server酱³ 端点)。布局单列整行:两卡纵向堆叠各占一行;
// 面板进场 stagger 40ms/项(§5.1),与短信服务页同款节奏。
import { motion } from "motion/react";
import type { ReactNode } from "react";

import { PageHeader } from "@/components/layout/page-header";
import { useT } from "@/lib/i18n";

import { ServerChanPanel } from "./components/ServerChanPanel";
import { WebhookPanel } from "./components/WebhookPanel";

const STAGGER_STEP_S = 0.04;

function StaggerItem({ index, children }: { index: number; children: ReactNode }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.22, ease: [0.22, 0.7, 0.28, 1], delay: index * STAGGER_STEP_S }}
    >
      {children}
    </motion.div>
  );
}

export default function SmsForwardPage() {
  const { t } = useT("smsforward");
  return (
    <>
      <PageHeader domain="comm" title={t("nav:smsForwarding")} description={t("pageDescription")} />
      <div className="grid grid-cols-1 gap-4">
        <StaggerItem index={0}>
          <WebhookPanel />
        </StaggerItem>
        <StaggerItem index={1}>
          <ServerChanPanel />
        </StaggerItem>
      </div>
    </>
  );
}
