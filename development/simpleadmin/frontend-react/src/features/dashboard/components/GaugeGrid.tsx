// 仪表盘 4 联卡(设计方案 §6.2 区块②):CPU/RAM/信号/温度 GaugeChart;
// CPU/RAM/温度用 monitor 域色渐变描边,信号用全站质量色阶(高=优,charts 缺省行为);
// 卡内 caption:RAM 显示 已用/总量(旧版 formatRamUsage),温度显示服务端原始值,信号显示评估词条。
import { GaugeChart } from "@/components/charts";
import { Card } from "@/components/ui/card";
import type { DashboardData } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { toNumber } from "../lib";

export interface GaugeGridProps {
  data: DashboardData | undefined;
  assessmentText: string;
}

export function GaugeGrid({ data, assessmentText }: GaugeGridProps) {
  const { t } = useT("dashboard");
  const { t: tc } = useT();
  const ramCaption =
    data?.ramUsedHuman &&
    data.ramUsedHuman !== "-" &&
    data?.ramTotalHuman &&
    data.ramTotalHuman !== "-"
      ? `${data.ramUsedHuman} / ${data.ramTotalHuman}`
      : tc("fetching");
  return (
    <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
      <Card className="p-4">
        <GaugeChart
          value={toNumber(data?.cpuUsagePercent)}
          label="%"
          title={t("cpuUsage")}
          domain="monitor"
          className="h-36 w-full"
        />
        <p className="mt-1 text-center text-sm text-muted">{t("liveLoad")}</p>
      </Card>
      <Card className="p-4">
        <GaugeChart
          value={toNumber(data?.ramUsagePercent)}
          label="%"
          title={t("ramUsage")}
          domain="monitor"
          className="h-36 w-full"
        />
        <p className="mt-1 text-center text-sm text-muted">{ramCaption}</p>
      </Card>
      <Card className="p-4">
        <GaugeChart
          value={toNumber(data?.signalPercentage)}
          label="%"
          title={t("signal")}
          className="h-36 w-full"
        />
        <p className="mt-1 text-center text-sm text-muted">{assessmentText}</p>
      </Card>
      <Card className="p-4">
        <GaugeChart
          value={toNumber(data?.temperature)}
          label="°C"
          title={t("temperature")}
          domain="monitor"
          className="h-36 w-full"
        />
        <p className="mt-1 text-center text-sm text-muted">{data?.temperature || "-"} °C</p>
      </Card>
    </div>
  );
}
