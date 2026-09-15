// 信号详情页(设计方案 §6.3,域色 network):4×天线仪表 + 载波聚合表 + 服务小区描述列表。
// 5s 轮询仅激活期(hooks.useSignalQuery,refetchIntervalInBackground:false 即 visibility 门控);
// pending 保留旧值 + "获取中" chip(§5.2);首屏骨架;面板 stagger 40ms 进场(§5.1)。
import { AntennaIcon, LayersIcon, RadioTowerIcon } from "lucide-react";
import { motion } from "motion/react";
import type { Transition } from "motion/react";
import type { ReactNode } from "react";

import { ErrorRetry, StatusChip } from "@/components/common";
import { PageHeader } from "@/components/layout/page-header";
import { Panel } from "@/components/layout/panel";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";

import { AntennaGauges } from "./components/AntennaGauges";
import { AggregationStatusChip, CarrierTable } from "./components/CarrierTable";
import { CellInfo } from "./components/CellInfo";
import { isSignalPendingError, useSignalQuery } from "./hooks";
import { formatUpdatedAt } from "./lib";

// 面板进场(§5.1):fade + y 12px→0,stagger 40ms/项
const PANEL_ENTER: Transition = { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] };

function Stagger({ index, children }: { index: number; children: ReactNode }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ ...PANEL_ENTER, delay: index * 0.04 }}
    >
      {children}
    </motion.div>
  );
}

export default function SignalPage() {
  const { t, i18n } = useT("signal");
  const query = useSignalQuery();
  const data = query.data;
  const pending = query.isPending || (query.isError && isSignalPendingError(query.error));
  const failed = query.isError && !isSignalPendingError(query.error);
  const pendingChip = pending ? (
    <StatusChip tone="info" pulse>
      {t("common:fetching")}
    </StatusChip>
  ) : undefined;

  return (
    <>
      <PageHeader domain="network" title={t("nav:signalDetails")} actions={pendingChip} />
      {failed && (
        <ErrorRetry
          className="mb-4"
          message={t("failedToLoadSignalData")}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      )}
      {query.isPending && !data ? (
        <div data-slot="signal-skeleton" aria-busy="true" className="space-y-4">
          <Skeleton className="h-64 w-full rounded-md" />
          <Skeleton className="h-40 w-full rounded-md" />
          <Skeleton className="h-40 w-full rounded-md" />
        </div>
      ) : (
        data && (
          <div className="space-y-4">
            <Stagger index={0}>
              <Panel
                domain="network"
                icon={AntennaIcon}
                title={t("antennaSignals")}
                footer={`${t("networkMode")}: ${data.rat || "-"} · ${t("updatedAt")} ${formatUpdatedAt(query.dataUpdatedAt, i18n.language)}`}
              >
                <AntennaGauges antennas={data.antennas ?? []} />
              </Panel>
            </Stagger>
            <Stagger index={1}>
              <Panel
                domain="network"
                icon={LayersIcon}
                title={t("carrierAggregation")}
                tools={<AggregationStatusChip carriers={data.carriers ?? []} />}
              >
                <CarrierTable carriers={data.carriers ?? []} />
              </Panel>
            </Stagger>
            <Stagger index={2}>
              <Panel domain="network" icon={RadioTowerIcon} title={t("servingCell")}>
                <CellInfo cell={data.cell} />
              </Panel>
            </Stagger>
          </div>
        )
      )}
    </>
  );
}
