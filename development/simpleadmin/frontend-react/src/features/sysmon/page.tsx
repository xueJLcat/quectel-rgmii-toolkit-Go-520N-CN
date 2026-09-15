// 系统监控页(设计方案 §6.14,域色 monitor):4×gauge(CPU/RAM/负载/在线时长)+ 进程总数 MetricCard、
// 进程 Top20 表(sort=cpu|mem 切换,3s 轮询 visibility 门控)、温度传感器面板(AT+QTEMP,60s 低频)。
// 行为对齐旧版 www/js/pages/sysmon.js:失败横幅 + 手动重试;首屏骨架;面板进场 stagger 40ms(§5.1)。
import { motion } from "motion/react";
import { useState } from "react";

import { PageHeader } from "@/components/layout/page-header";
import { useT } from "@/lib/i18n";
import { MonitorGauges } from "./components/MonitorGauges";
import { ProcessTable } from "./components/ProcessTable";
import { STAGGER_CONTAINER, STAGGER_ITEM } from "./components/stagger";
import { TemperaturePanel } from "./components/TemperaturePanel";
import { useQtempQuery, useSysmonQuery } from "./hooks";
import type { ProcessSort } from "./hooks";
import { formatClock } from "./lib";

export default function SysmonPage() {
  const { t } = useT("nav");
  const [sort, setSort] = useState<ProcessSort>("cpu");
  const monitor = useSysmonQuery(sort);
  const qtemp = useQtempQuery();
  const lastUpdate = monitor.dataUpdatedAt > 0 ? formatClock(monitor.dataUpdatedAt) : "-";
  return (
    <>
      <PageHeader domain="monitor" title={t("systemMonitor")} />
      <motion.div
        variants={STAGGER_CONTAINER}
        initial="hidden"
        animate="show"
        className="space-y-4"
      >
        <motion.div variants={STAGGER_ITEM}>
          <MonitorGauges data={monitor.data} />
        </motion.div>
        <motion.div variants={STAGGER_ITEM}>
          <ProcessTable
            data={monitor.data}
            failed={monitor.isError && !monitor.data}
            retrying={monitor.isFetching}
            sort={sort}
            onSortChange={setSort}
            onRetry={() => void monitor.refetch()}
            lastUpdate={lastUpdate}
          />
        </motion.div>
        <motion.div variants={STAGGER_ITEM}>
          <TemperaturePanel
            readings={qtemp.data}
            failed={qtemp.isError}
            retrying={qtemp.isFetching}
            onRetry={qtemp.refetch}
          />
        </motion.div>
      </motion.div>
    </>
  );
}
