// 系统监控仪表盘组(设计方案 §6.14):CPU/RAM/负载/在线时长 4×GaugeChart(monitor 域色渐变描边)
// + 进程总数 MetricCard(count-up)。负载按 LOAD_FULL_SCALE 核满载折算百分比、在线时长按 30 天量程
// 折算天数(后端仅提供文本,原始串在 caption 保留);RAM caption = 已用/总量(旧版 formatRamUsage)。
import { ListIcon } from "lucide-react";

import { GaugeChart } from "@/components/charts";
import { MetricCard } from "@/components/common";
import { Card } from "@/components/ui/card";
import type { SystemMonitor } from "@/lib/api";
import { useT } from "@/lib/i18n";
import {
  UPTIME_GAUGE_MAX_DAYS,
  formatRamUsage,
  loadGaugePercent,
  parseUptimeDays,
  toGaugeNumber,
} from "../lib";

export interface MonitorGaugesProps {
  data: SystemMonitor | undefined;
}

export function MonitorGauges({ data }: MonitorGaugesProps) {
  const { t } = useT("sysmon");
  const { t: tc } = useT();
  const uptimeDays = parseUptimeDays(data?.uptime);
  return (
    <div className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-5">
      <Card className="p-4">
        <GaugeChart
          value={toGaugeNumber(data?.cpuUsagePercent)}
          label="%"
          title={t("cpuUsage")}
          domain="monitor"
          className="h-36 w-full"
        />
        <p className="mt-1 text-center text-sm text-muted">{t("liveLoad")}</p>
      </Card>
      <Card className="p-4">
        <GaugeChart
          value={toGaugeNumber(data?.ramUsagePercent)}
          label="%"
          title={t("memory")}
          domain="monitor"
          className="h-36 w-full"
        />
        <p className="mt-1 text-center text-sm text-muted">
          {formatRamUsage(data?.ramUsedHuman, data?.ramTotalHuman, tc("fetching"))}
        </p>
      </Card>
      <Card className="p-4">
        <GaugeChart
          value={loadGaugePercent(data?.loadAverage)}
          label="%"
          title={t("loadAverage")}
          domain="monitor"
          className="h-36 w-full"
        />
        <p className="mt-1 text-center font-mono text-sm text-muted" title={t("loadAverage")}>
          {data?.loadAverage || "-"}
        </p>
      </Card>
      <Card className="p-4">
        <GaugeChart
          value={uptimeDays ?? 0}
          max={UPTIME_GAUGE_MAX_DAYS}
          label="d"
          title={t("uptime")}
          domain="monitor"
          className="h-36 w-full"
        />
        <p className="mt-1 text-center text-sm text-muted">{data?.uptime || "-"}</p>
      </Card>
      <MetricCard
        label={t("processes")}
        value={toGaugeNumber(data?.processCount)}
        icon={ListIcon}
        domain="monitor"
        className="h-full"
      />
    </div>
  );
}
