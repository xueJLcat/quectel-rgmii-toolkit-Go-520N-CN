// 自动化页(设计方案 §6.13,域色 system/rose):3 张配置卡(短信 Webhook 已迁 sms 页)——
// 断网自愈看门狗 / 每日定时重启 / 时间同步;面板 stagger 40ms 进场(§5.1)。
import { motion } from "motion/react";
import type { Variants } from "motion/react";

import { PageHeader } from "@/components/layout/page-header";
import { useT } from "@/lib/i18n";

import { SchedulerCard } from "./components/SchedulerCard";
import { TimeSyncCard } from "./components/TimeSyncCard";
import { WatchdogCard } from "./components/WatchdogCard";

const STAGGER_PARENT: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.04 } },
};

const STAGGER_CHILD: Variants = {
  hidden: { opacity: 0, y: 12 },
  show: { opacity: 1, y: 0, transition: { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] } },
};

export default function AutomationPage() {
  const { t } = useT("automation");
  const { t: tn } = useT("nav");
  return (
    <>
      <PageHeader domain="system" title={tn("automation")} description={t("pageDescription")} />
      <motion.div
        variants={STAGGER_PARENT}
        initial="hidden"
        animate="show"
        className="grid grid-cols-1 gap-4 xl:grid-cols-2"
      >
        <motion.div variants={STAGGER_CHILD} className="xl:col-span-2">
          <WatchdogCard />
        </motion.div>
        <motion.div variants={STAGGER_CHILD}>
          <SchedulerCard />
        </motion.div>
        <motion.div variants={STAGGER_CHILD}>
          <TimeSyncCard />
        </motion.div>
      </motion.div>
    </>
  );
}
