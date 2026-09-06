// 进程 Top 20 表(设计方案 §6.14):排序切换 cpu|mem(旧版 select v-model sortBy → watch 重拉,
// 这里经 queryKey 变化触发);CPU%/内存% 单元格内嵌 MetricBar(阈值色阶 charts 内置);
// CPU% > 100% 时 Tooltip 说明单核口径;空列表 EmptyState、失败 ErrorRetry(旧版 retryMonitor)。
import { TableIcon } from "lucide-react";

import { MetricBar } from "@/components/charts";
import { EmptyState, ErrorRetry } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { ProcessInfo, SystemMonitor } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { PROCESS_TOP_N, formatCpu, formatNum, toGaugeNumber } from "../lib";
import type { ProcessSort } from "../hooks";

function PercentCell({
  text,
  percent,
  over,
  note,
}: {
  text: string;
  percent: number;
  over?: boolean;
  note?: string;
}) {
  const cell = (
    <div className="flex w-24 flex-col gap-1">
      <span className={cn("text-sm tabular-nums", over && "font-semibold text-warning")}>
        {text}
      </span>
      <MetricBar percent={percent} />
    </div>
  );
  if (!over || !note) return cell;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div tabIndex={0} className="cursor-help outline-none">
          {cell}
        </div>
      </TooltipTrigger>
      <TooltipContent>{note}</TooltipContent>
    </Tooltip>
  );
}

export interface ProcessTableProps {
  data: SystemMonitor | undefined;
  failed: boolean;
  retrying: boolean;
  sort: ProcessSort;
  onSortChange: (sort: ProcessSort) => void;
  onRetry: () => void;
  lastUpdate: string;
}

export function ProcessTable({
  data,
  failed,
  retrying,
  sort,
  onSortChange,
  onRetry,
  lastUpdate,
}: ProcessTableProps) {
  const { t } = useT("sysmon");
  const { t: tc } = useT();
  const processes = (data?.processes ?? []).slice(0, PROCESS_TOP_N);
  return (
    <Panel
      domain="monitor"
      icon={TableIcon}
      title={t("processTop20")}
      tools={
        <div className="flex items-center gap-2">
          <span className="hidden text-sm text-muted sm:inline">
            {tc("lastUpdated")} <span className="font-mono text-soft">{lastUpdate}</span>
          </span>
          <Tabs value={sort} onValueChange={(value) => onSortChange(value as ProcessSort)}>
            <TabsList aria-label={t("processSortOrder")}>
              <TabsTrigger value="cpu">{t("byCpu")}</TabsTrigger>
              <TabsTrigger value="mem">{t("byMemory")}</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
      }
    >
      {failed ? (
        <ErrorRetry
          message={t("failedToLoadSystemMonitorData")}
          onRetry={onRetry}
          retrying={retrying}
        />
      ) : !data ? (
        <div aria-busy="true" className="space-y-2">
          {Array.from({ length: 6 }, (_, index) => (
            <div key={index} className="h-9 animate-pulse rounded-sm bg-surface-3" />
          ))}
        </div>
      ) : processes.length === 0 ? (
        <EmptyState icon={TableIcon} title={t("noProcessData")} />
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("pid")}</TableHead>
                <TableHead>{t("user")}</TableHead>
                <TableHead>{t("process")}</TableHead>
                <TableHead>{t("cpuPercent")}</TableHead>
                <TableHead>{t("memPercent")}</TableHead>
                <TableHead>{t("memory")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {processes.map((process: ProcessInfo, index) => {
                const cpu = toGaugeNumber(process.cpuPercent);
                const mem = toGaugeNumber(process.memPercent);
                return (
                  <TableRow key={process.pid ?? index}>
                    <TableCell className="font-mono text-sm">{process.pid ?? "-"}</TableCell>
                    <TableCell>{process.user || "-"}</TableCell>
                    <TableCell className="max-w-48 truncate" title={process.name}>
                      {process.name || "-"}
                    </TableCell>
                    <TableCell>
                      <PercentCell
                        text={formatCpu(process.cpuPercent)}
                        percent={cpu}
                        over={cpu > 100}
                        note={t("cpuOver100Note")}
                      />
                    </TableCell>
                    <TableCell>
                      <PercentCell text={formatNum(process.memPercent)} percent={mem} />
                    </TableCell>
                    <TableCell className="text-sm tabular-nums">
                      {process.rssHuman || "-"}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </Panel>
  );
}
