// 信号与流量趋势卡(设计方案 §6.2 区块④):historyData 60s 轮询(hooks 内 visibility 门控),
// points < 2 时 EmptyState 说明(旧版 sa-empty-state 文案),图表下方 meta 行 = 最新信号/速率/时间范围
// (旧版 sa-history-meta 四项,取值纯函数见 lib.historyLatest*)。
import { TrendingUpIcon } from "lucide-react";

import { TrendChart } from "@/components/charts";
import { EmptyState } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import type { HistoryPoint } from "@/lib/api";
import { useT } from "@/lib/i18n";
import {
  historyLatestRxRate,
  historyLatestSignal,
  historyLatestTxRate,
  historyTimeRange,
} from "../lib";

function TrendMeta({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted">{label}</div>
      <div className="truncate text-sm font-medium text-ink tabular-nums" title={value}>
        {value}
      </div>
    </div>
  );
}

export interface TrendCardProps {
  points: readonly HistoryPoint[];
}

export function TrendCard({ points }: TrendCardProps) {
  const { t } = useT("dashboard");
  const ready = points.length >= 2;
  return (
    <Panel domain="monitor" icon={TrendingUpIcon} title={t("signalTrafficTrend")}>
      {ready ? (
        <>
          <TrendChart points={points} className="h-64 w-full" />
          <div className="mt-3 grid grid-cols-2 gap-3 border-t border-line pt-3 sm:grid-cols-4">
            <TrendMeta label={t("signal")} value={historyLatestSignal(points)} />
            <TrendMeta label={t("downloadRate")} value={historyLatestRxRate(points)} />
            <TrendMeta label={t("uploadRate")} value={historyLatestTxRate(points)} />
            <TrendMeta label={t("timeRange")} value={historyTimeRange(points)} />
          </div>
        </>
      ) : (
        <EmptyState
          icon={TrendingUpIcon}
          title={t("noHistoryYetKeepThisPageOpenToAccumulateData")}
        />
      )}
    </Panel>
  );
}
