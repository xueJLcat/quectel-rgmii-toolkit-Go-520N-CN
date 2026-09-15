// 4×天线 RSRP 仪表阵列(设计方案 §6.3):GaugeChart 不传 domain → 描边按全站信号质量色阶(§3.2)取色;
// 仪表中心为百分比,下方副标题为 RSRP dBm("-" → common:none 占位)。
import { GaugeChart } from "@/components/charts";
import type { SignalAntenna } from "@/lib/api";
import { useT } from "@/lib/i18n";

import { antennaLabel, antennaRsrpText } from "../lib";

export interface AntennaGaugesProps {
  antennas: readonly SignalAntenna[];
}

export function AntennaGauges({ antennas }: AntennaGaugesProps) {
  const { t } = useT("signal");
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {antennas.map((antenna, index) => (
        <div
          key={antenna.id ?? index}
          className="flex flex-col items-center gap-1 rounded-md border border-line bg-surface-2 p-3"
        >
          <GaugeChart
            value={Number(antenna.percent) || 0}
            max={100}
            label="%"
            title={antennaLabel(antenna, t)}
            className="h-36 w-full"
          />
          <span className="text-sm text-muted tabular-nums">
            {antennaRsrpText(antenna, t("common:none"))}
          </span>
        </div>
      ))}
    </div>
  );
}
