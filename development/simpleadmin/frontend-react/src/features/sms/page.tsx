// 短信服务页(域色 comm,设计方案 §6.9):存储可视化条(新)+ 收件箱(会话式列表/多选/详情)+
// 发送表单(归一化预览/分段计数/CMS 释义)。短信转发(Webhook/Server酱)已迁至独立页 #/smsforward。
// 布局单列整行:三卡纵向堆叠各占一行;面板进场 stagger 40ms/项(§5.1);
// 详情"回复"把号码带进发送表单(页面级联动)。
import { motion } from "motion/react";
import { useState } from "react";
import type { ReactNode } from "react";

import { PageHeader } from "@/components/layout/page-header";
import { useT } from "@/lib/i18n";

import { InboxPanel } from "./components/InboxPanel";
import { SendPanel } from "./components/SendPanel";
import { StoragePanel } from "./components/StoragePanel";
import { useSmsImsi, useSmsInbox, useSmsStorage } from "./hooks";

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

export default function SmsPage() {
  const { t } = useT("sms");
  const inbox = useSmsInbox();
  const storage = useSmsStorage();
  const imsi = useSmsImsi();
  const [sendNumber, setSendNumber] = useState("");
  const [sendMessage, setSendMessage] = useState("");

  return (
    <>
      <PageHeader domain="comm" title={t("nav:smsService")} description={t("pageDescription")} />
      <div className="grid grid-cols-1 gap-4">
        <StaggerItem index={0}>
          <StoragePanel
            status={storage.data}
            isLoading={storage.isFetching}
            isError={storage.isError}
            onRefresh={() => void storage.refetch()}
          />
        </StaggerItem>
        <StaggerItem index={1}>
          <SendPanel
            number={sendNumber}
            onNumberChange={setSendNumber}
            message={sendMessage}
            onMessageChange={setSendMessage}
            imsi={imsi}
            onSent={() => void inbox.forceReload()}
          />
        </StaggerItem>
        <StaggerItem index={2}>
          <InboxPanel inbox={inbox} onReply={setSendNumber} />
        </StaggerItem>
      </div>
    </>
  );
}
