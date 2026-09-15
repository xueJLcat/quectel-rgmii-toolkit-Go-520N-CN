// 网络详情页(设计方案 §6.6,域色 network):WAN/LAN 地址卡组 + 在线设备 MetricCard +
// 接口表(速率内嵌 Sparkline,历史组件内滚动累积)+ 局域网设备表(剩余租期 chip)。
// 轮询 5s 仅激活期(visibility 门控);pending 保留旧值 + "获取中" chip;面板 stagger 40ms。
import { ActivityIcon, LoaderCircleIcon, RefreshCwIcon, UsersIcon } from "lucide-react";
import { motion } from "motion/react";
import type { Transition } from "motion/react";
import type { ReactNode } from "react";

import { ErrorRetry, MetricCard, StatusChip } from "@/components/common";
import { PageHeader } from "@/components/layout/page-header";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";

import { AddressCards } from "./components/AddressCards";
import { ClientTable } from "./components/ClientTable";
import { InterfaceTable } from "./components/InterfaceTable";
import { isNetdetailPendingError, useNetworkDetailQuery, useRateHistory } from "./hooks";

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

export default function NetdetailPage() {
  const { t } = useT("netdetail");
  const query = useNetworkDetailQuery();
  const data = query.data;
  const history = useRateHistory(data?.interfaces, query.dataUpdatedAt);
  const pending = query.isPending || (query.isError && isNetdetailPendingError(query.error));
  const failed = query.isError && !isNetdetailPendingError(query.error);

  return (
    <>
      <PageHeader
        domain="network"
        title={t("nav:networkDetails")}
        actions={
          <>
            {pending && (
              <StatusChip tone="info" pulse>
                {t("common:fetching")}
              </StatusChip>
            )}
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void query.refetch()}
              disabled={query.isFetching}
            >
              {query.isFetching ? <LoaderCircleIcon className="animate-spin" /> : <RefreshCwIcon />}
              {t("common:refresh")}
            </Button>
          </>
        }
      />
      {failed && (
        <ErrorRetry
          className="mb-4"
          message={t("failedToLoadNetworkDetails")}
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      )}
      {query.isPending && !data ? (
        <div data-slot="netdetail-skeleton" aria-busy="true" className="space-y-4">
          <Skeleton className="h-24 w-full rounded-md" />
          <Skeleton className="h-64 w-full rounded-md" />
        </div>
      ) : (
        data && (
          <div className="space-y-4">
            <Stagger index={0}>
              <div className="grid grid-cols-1 items-stretch gap-4 xl:grid-cols-4">
                <AddressCards
                  wanIPv4={data.wanIPv4}
                  wanIPv6={data.wanIPv6}
                  lanGateway={data.lanGateway}
                  className="xl:col-span-3"
                />
                <MetricCard
                  label={t("devicesOnline")}
                  value={data.clients?.length ?? 0}
                  icon={UsersIcon}
                  domain="network"
                  className="h-full"
                />
              </div>
            </Stagger>
            <Stagger index={1}>
              <Panel domain="network" icon={ActivityIcon} title={t("interfaceStatus")}>
                <InterfaceTable interfaces={data.interfaces ?? []} history={history} />
              </Panel>
            </Stagger>
            <Stagger index={2}>
              <Panel domain="network" icon={UsersIcon} title={t("lanDevices")}>
                <ClientTable clients={data.clients ?? []} />
              </Panel>
            </Stagger>
          </div>
        )
      )}
    </>
  );
}
