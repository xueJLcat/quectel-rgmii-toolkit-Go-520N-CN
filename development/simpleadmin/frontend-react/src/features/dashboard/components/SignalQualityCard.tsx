// 信号质量卡(设计方案 §6.2 区块③):RSRQ/RSRP/SINR 渐变进度条,按当前驻留制式过滤
// (network_mode 含 NR5G 只显示 5G 三行,含 LTE 只显示 4G 三行,未就绪/未知保留六行诚实显示"无"),
// 填充色按全站质量四档色阶(charts signalQualityColor,随明暗主题);头部 signalAssessment
// 徽章(服务端中文枚举 → dashboard ns 词条 + StatusChip tone)。旧版行格式:"值 / 百分比%"。
import { signalQualityColor, useChartTheme } from "@/components/charts";
import { StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import type { DashboardData } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { describeAssessment, toNumber, visibleSignalRats } from "../lib";
import type { AssessmentView } from "../lib";

interface QualityRowProps {
  label: string;
  value: string | undefined;
  percent: number;
}

function QualityRow({ label, value, percent }: QualityRowProps) {
  const { t } = useT();
  const theme = useChartTheme();
  const clamped = Math.max(0, Math.min(100, percent));
  const color = signalQualityColor(clamped, theme.mode);
  const hasValue = value !== undefined && value !== "" && value !== "-";
  return (
    <div data-slot="quality-row" className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2 text-sm">
        <span className="text-muted">{label}</span>
        <span className="font-medium text-ink tabular-nums">
          {hasValue ? `${value} / ${Math.round(clamped)}%` : t("none")}
        </span>
      </div>
      <div
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(clamped)}
        className="h-2 w-full overflow-hidden rounded-full bg-bg-soft"
      >
        <div
          className={cn(
            "h-full rounded-full",
            "transition-[width] duration-[var(--sa-dur-slow)] ease-sa",
          )}
          style={{
            width: `${clamped}%`,
            backgroundImage: `linear-gradient(90deg, ${color}99, ${color})`,
          }}
        />
      </div>
    </div>
  );
}

export interface SignalQualityCardProps {
  data: DashboardData | undefined;
}

export function SignalQualityCard({ data }: SignalQualityCardProps) {
  const { t } = useT("dashboard");
  const { t: tc } = useT();
  const assessment: AssessmentView = describeAssessment(data?.signalAssessment);
  const assessmentText = assessment.key ? t(assessment.key) : (assessment.text ?? tc("none"));
  const rows = [
    {
      label: `RSRQ ${t("lte")}`,
      rat: "lte" as const,
      value: data?.rsrqLTE,
      percent: toNumber(data?.rsrqLTEPercentage),
    },
    {
      label: `RSRQ ${t("nr")}`,
      rat: "nr" as const,
      value: data?.rsrqNR,
      percent: toNumber(data?.rsrqNRPercentage),
    },
    {
      label: `RSRP ${t("lte")}`,
      rat: "lte" as const,
      value: data?.rsrpLTE,
      percent: toNumber(data?.rsrpLTEPercentage),
    },
    {
      label: `RSRP ${t("nr")}`,
      rat: "nr" as const,
      value: data?.rsrpNR,
      percent: toNumber(data?.rsrpNRPercentage),
    },
    {
      label: `SINR ${t("lte")}`,
      rat: "lte" as const,
      value: data?.sinrLTE,
      percent: toNumber(data?.sinrLTEPercentage),
    },
    {
      label: `SINR ${t("nr")}`,
      rat: "nr" as const,
      value: data?.sinrNR,
      percent: toNumber(data?.sinrNRPercentage),
    },
  ];
  // 按当前驻留制式过滤:4G 驻留只显示 4G 三行,5G 只显示 5G 三行,未就绪保留六行
  const rats = visibleSignalRats(data?.network_mode);
  const visibleRows = rows.filter((row) => rats.includes(row.rat));
  return (
    <Panel
      domain="monitor"
      title={t("signalQuality")}
      tools={<StatusChip tone={assessment.tone}>{assessmentText}</StatusChip>}
    >
      {/* 单列:每个参数(RSRQ/RSRP/SINR)各占满整行 */}
      <div className="grid gap-y-3">
        {visibleRows.map((row) => (
          <QualityRow key={row.label} {...row} />
        ))}
      </div>
    </Panel>
  );
}
