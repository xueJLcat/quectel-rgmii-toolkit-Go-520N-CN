// 延迟对比图:barCompareOption 汇总本轮探测各目标的 HTTP 延迟(成功 = tools 域色 cyan,
// 失败 = danger rose);无结果时显示空态。ECharts 经 charts 统一出口(懒加载 chunk 由壳层控制)。
import { ChartColumnIcon } from "lucide-react";
import { useMemo } from "react";

import { EmptyState } from "@/components/common";
import { DOMAIN_GRADIENTS, EChart, barCompareOption, useChartTheme } from "@/components/charts";
import { Panel } from "@/components/layout/panel";
import { useT } from "@/lib/i18n";

import type { ProbeOutcome } from "../lib";

/** 失败条目颜色:danger rose(与 tokens 语义色一致,图表 series 色按 charts/options 惯例用 hex)。 */
const FAIL_COLOR = "#f43f5e";

export interface LatencyChartCardProps {
  results: ProbeOutcome[];
}

export function LatencyChartCard({ results }: LatencyChartCardProps) {
  const { t } = useT("diag");
  const theme = useChartTheme();
  const items = useMemo(
    () =>
      results
        .filter((item) => item.state === "done" && typeof item.latencyMs === "number")
        .map((item) => ({
          label: item.target,
          value: item.latencyMs as number,
          color: item.ok ? DOMAIN_GRADIENTS.tools[0] : FAIL_COLOR,
        })),
    [results],
  );
  const option = useMemo(() => barCompareOption({ items, theme }), [items, theme]);
  return (
    <Panel
      domain="tools"
      icon={ChartColumnIcon}
      title={t("latencyCompare")}
      footer={t("latencyCompareDescription")}
    >
      {items.length === 0 ? (
        <EmptyState title={t("latencyCompareEmpty")} />
      ) : (
        <EChart option={option} className="min-h-56 w-full flex-1" />
      )}
    </Panel>
  );
}
