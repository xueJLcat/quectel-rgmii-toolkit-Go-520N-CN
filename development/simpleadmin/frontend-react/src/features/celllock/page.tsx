// 小区锁定页(设计方案 §6.5):锁定状态横幅 + 扫描卡 + 一键锁定服务小区 + 手动锁定卡 +
// 频点锁定卡。数据流对齐旧版 www/js/pages/celllock.js:进入页面拉取 settings 与
// earfcn_lock_status(RQ 挂载即拉 = onPageReturn 语义),锁定/解锁写操作 mutation+toast+
// 延迟 3s 回读;所有锁定/解锁操作经 stores/confirm;pending 保留旧值 + chip。
// 旧版与后端 page_network.go 均无 rebootAfterSeconds/rebootCountdownSeconds 字段,
// 本页无动作接 reboot store 倒计时。
import { CrosshairIcon, ListIcon, LockIcon, RadioTowerIcon } from "lucide-react";
import { motion } from "motion/react";
import type { Variants } from "motion/react";

import { ErrorRetry } from "@/components/common";
import { PageHeader } from "@/components/layout/page-header";
import { Panel } from "@/components/layout/panel";
import { useT } from "@/lib/i18n";

import { EarfcnLockCard } from "./components/EarfcnLockCard";
import { LockStatusBanner } from "./components/LockStatusBanner";
import { ManualLockCard } from "./components/ManualLockCard";
import { ScanCard } from "./components/ScanCard";
import { ServingCellCard } from "./components/ServingCellCard";
import { useCellLockSettingsQuery, useEarfcnLockStatusQuery } from "./hooks";

// 面板入场 stagger 40ms(§5.1,与 App 路由转场同缓动)
const STAGGER_CONTAINER: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.04 } },
};
const STAGGER_ITEM: Variants = {
  hidden: { opacity: 0, y: 8 },
  show: { opacity: 1, y: 0, transition: { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] } },
};

export default function CellLockPage() {
  const { t } = useT("celllock");
  const settingsQuery = useCellLockSettingsQuery();
  const earfcnQuery = useEarfcnLockStatusQuery();

  const settings = settingsQuery.settings;
  const settingsPending = settingsQuery.isPending || settingsQuery.data?.pending === true;
  const settingsFailed = settingsQuery.isError || Boolean(settingsQuery.data?.error);

  const earfcnStatus = earfcnQuery.status;
  const earfcnPending = earfcnQuery.data?.pending === true;
  const earfcnFailed =
    earfcnQuery.isError || earfcnQuery.data?.ok === false || Boolean(earfcnQuery.data?.error);

  return (
    <>
      <PageHeader domain="network" title={t("nav:cellLock")} />
      <motion.div
        variants={STAGGER_CONTAINER}
        initial="hidden"
        animate="show"
        className="flex flex-col gap-4"
      >
        <motion.div variants={STAGGER_ITEM}>
          <LockStatusBanner
            cellLockStatus={settings?.cellLockStatus}
            settingsPending={settingsPending}
            nr5gArfcns={earfcnStatus?.nr5g_arfcns ?? []}
            lteArfcns={earfcnStatus?.lte_arfcns ?? []}
            earfcnPending={earfcnPending}
          />
        </motion.div>

        {settingsFailed && (
          <motion.div variants={STAGGER_ITEM}>
            <ErrorRetry
              message={t("common:failedToReadSettings")}
              onRetry={() => void settingsQuery.refetch()}
              retrying={settingsQuery.isFetching}
            />
          </motion.div>
        )}

        <motion.div variants={STAGGER_ITEM}>
          <Panel domain="network" icon={RadioTowerIcon} title={t("cellScan")}>
            <ScanCard />
          </Panel>
        </motion.div>

        <motion.div variants={STAGGER_ITEM}>
          <Panel domain="network" icon={CrosshairIcon} title={t("lockServingCell")}>
            <ServingCellCard />
          </Panel>
        </motion.div>

        <motion.div variants={STAGGER_ITEM}>
          <Panel domain="network" icon={LockIcon} title={t("manualCellLock")}>
            <ManualLockCard cellLockStatus={settings?.cellLockStatus ?? t("lockStatusUnknown")} />
          </Panel>
        </motion.div>

        <motion.div variants={STAGGER_ITEM}>
          <Panel domain="network" icon={ListIcon} title={t("earfcnLock")}>
            <EarfcnLockCard
              nr5gArfcns={earfcnStatus?.nr5g_arfcns ?? []}
              lteArfcns={earfcnStatus?.lte_arfcns ?? []}
              pending={earfcnPending}
              failed={earfcnFailed}
              onRetry={() => void earfcnQuery.refetch()}
              retrying={earfcnQuery.isFetching}
            />
          </Panel>
        </motion.div>
      </motion.div>
    </>
  );
}
