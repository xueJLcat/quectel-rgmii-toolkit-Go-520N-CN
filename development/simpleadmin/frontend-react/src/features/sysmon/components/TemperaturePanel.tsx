// 温度传感器面板(设计方案 §6.14 新增):AT+QTEMP 经 parseQtemp 解析 → HeatBar 列表
// (阈值色阶 charts 内置);60s 低频轮询 + 手动刷新钮;footer 注明低频刷新原因(AT 通道独占)。
// 失败 → ErrorRetry;解析结果为空 → error 态(EmptyState 文案 + 重试入口)。
import { LoaderCircleIcon, RefreshCwIcon, ThermometerIcon } from "lucide-react";

import { HeatBar } from "@/components/charts";
import { EmptyState, ErrorRetry } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";
import type { QtempReading } from "../lib";

export interface TemperaturePanelProps {
  readings: QtempReading[] | undefined;
  failed: boolean;
  retrying: boolean;
  onRetry: () => void;
}

export function TemperaturePanel({ readings, failed, retrying, onRetry }: TemperaturePanelProps) {
  const { t } = useT("sysmon");
  const { t: tc } = useT();
  const empty = readings !== undefined && readings.length === 0;
  return (
    <Panel
      domain="monitor"
      icon={ThermometerIcon}
      title={t("temperatureSensors")}
      tools={
        <Button
          variant="ghost"
          size="sm"
          onClick={onRetry}
          disabled={retrying}
          aria-label={tc("refresh")}
        >
          {retrying ? <LoaderCircleIcon className="animate-spin" /> : <RefreshCwIcon />}
          {tc("refresh")}
        </Button>
      }
      footer={t("qtempLowFrequencyNote")}
    >
      {failed ? (
        <ErrorRetry message={t("qtempFailed")} onRetry={onRetry} retrying={retrying} />
      ) : !readings ? (
        <div aria-busy="true" className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 6 }, (_, index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : empty ? (
        <EmptyState
          icon={ThermometerIcon}
          title={t("qtempEmpty")}
          action={
            <Button variant="secondary" size="sm" onClick={onRetry}>
              {tc("retry")}
            </Button>
          }
        />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {readings.map((reading) => (
            <HeatBar key={reading.sensor} label={reading.sensor} value={reading.temperature} />
          ))}
        </div>
      )}
    </Panel>
  );
}
