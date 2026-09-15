// 系统设置页(设计方案 §6.15,域色 system/rose):设备操作 DangerZone(重启模块/重启设备/关机)
// + IMEI / 界面偏好 / 账户安全 / TTL 五卡;面板 stagger 40ms 进场(§5.1)。
import { motion } from "motion/react";
import type { Variants } from "motion/react";

import { PageHeader } from "@/components/layout/page-header";
import { useT } from "@/lib/i18n";

import { DeviceActionsCard } from "./components/DeviceActionsCard";
import { ImeiCard } from "./components/ImeiCard";
import { PasswordCard } from "./components/PasswordCard";
import { PreferencesCard } from "./components/PreferencesCard";
import { TtlCard } from "./components/TtlCard";

const STAGGER_PARENT: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.04 } },
};

const STAGGER_CHILD: Variants = {
  hidden: { opacity: 0, y: 12 },
  show: { opacity: 1, y: 0, transition: { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] } },
};

export default function SettingsPage() {
  const { t } = useT("settings");
  const { t: tn } = useT("nav");
  return (
    <>
      <PageHeader domain="system" title={tn("systemSettings")} description={t("pageDescription")} />
      <motion.div
        variants={STAGGER_PARENT}
        initial="hidden"
        animate="show"
        className="grid grid-cols-1 gap-4 xl:grid-cols-2"
      >
        <motion.div variants={STAGGER_CHILD} className="xl:col-span-2">
          <DeviceActionsCard />
        </motion.div>
        <motion.div variants={STAGGER_CHILD}>
          <ImeiCard />
        </motion.div>
        <motion.div variants={STAGGER_CHILD}>
          <PreferencesCard />
        </motion.div>
        <motion.div variants={STAGGER_CHILD}>
          <PasswordCard />
        </motion.div>
        <motion.div variants={STAGGER_CHILD}>
          <TtlCard />
        </motion.div>
      </motion.div>
    </>
  );
}
