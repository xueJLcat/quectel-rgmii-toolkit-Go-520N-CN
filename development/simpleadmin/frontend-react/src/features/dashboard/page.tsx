// 总览页(设计方案 §6.2,域色 monitor):①连接状态英雄条 ②4×GaugeChart ③信号质量卡 ④趋势图
// ⑤网络信息卡。行为对齐旧版 www/js/pages/index.js:主轮询由刷新频率驱动(2–60s,localStorage
// "refreshRate")且 document.hidden 暂停;historyData 60s;at_cache_updated → 去抖静默刷新;
// pending 保留旧值 + StatusChip(common:getting),绝不假空态;首屏骨架、无数据错误走 ErrorRetry。
// 面板进场 stagger 40ms(§5.1)。
import { motion } from "motion/react";
import { useState } from "react";

import { ErrorRetry, StatusChip } from "@/components/common";
import { PageHeader } from "@/components/layout/page-header";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";
import { ConnectionHero } from "./components/ConnectionHero";
import { GaugeGrid } from "./components/GaugeGrid";
import { NetworkInfoCard } from "./components/NetworkInfoCard";
import { RefreshRateControl } from "./components/RefreshRateControl";
import { SignalQualityCard } from "./components/SignalQualityCard";
import { TrendCard } from "./components/TrendCard";
import { STAGGER_CONTAINER, STAGGER_ITEM } from "./components/stagger";
import {
  useAtCacheSilentRefresh,
  useDashboardStream,
  useHistoryStream,
  useNetworkCompact,
  useRefreshRate,
  useTrafficView,
} from "./hooks";
import { describeActiveSim, describeAssessment, formatUptimeParts } from "./lib";

function DashboardSkeleton() {
  return (
    <div data-slot="dashboard-skeleton" aria-busy="true" className="space-y-4">
      <Skeleton className="h-40 w-full rounded-md" />
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <Skeleton className="h-48 rounded-md" />
        <Skeleton className="h-48 rounded-md" />
        <Skeleton className="h-48 rounded-md" />
        <Skeleton className="h-48 rounded-md" />
      </div>
      <Skeleton className="h-80 w-full rounded-md" />
    </div>
  );
}

export default function DashboardPage() {
  const { t, i18n } = useT("dashboard");
  const { t: tc } = useT();
  const { rate, applyRate } = useRefreshRate();
  const [draft, setDraft] = useState("");
  const { view, pending, errorText, failed, stale, isFetching, refetch } = useDashboardStream(rate);
  const points = useHistoryStream();
  const traffic = useTrafficView(pending ? undefined : view);
  const { compact, toggle } = useNetworkCompact();
  useAtCacheSilentRefresh();

  const en = i18n.language.toLowerCase().startsWith("en");
  const uptimeText = formatUptimeParts(
    view?.uptimeParts,
    { days: tc("days"), hours: tc("hours"), minutes: tc("minutes") },
    t("unknownTime"),
    en,
  );
  const sim = describeActiveSim(view?.sim, view?.active_sim);
  const activeSimText =
    sim.kind === "activeSlot"
      ? tc("simActive", { index: sim.index })
      : sim.kind === "active"
        ? t("active")
        : sim.kind === "inactiveSlot"
          ? tc("simInactive", { index: sim.index })
          : sim.kind === "inactive"
            ? t("inactive")
            : sim.index
              ? `${sim.text} ${sim.index}`
              : sim.text;
  const assessment = describeAssessment(view?.signalAssessment);
  const assessmentText = assessment.key ? t(assessment.key) : (assessment.text ?? tc("none"));
  const apply = (): void => {
    if (applyRate(draft)) setDraft("");
  };

  return (
    <>
      <PageHeader
        domain="monitor"
        title={t("nav:overview")}
        actions={
          <RefreshRateControl
            rate={rate}
            draft={draft}
            onDraftChange={setDraft}
            onApply={apply}
            lastUpdate={view?.lastUpdate ?? ""}
          />
        }
      />
      {failed ? (
        <ErrorRetry message={tc("dataUpdateFailed")} onRetry={refetch} retrying={isFetching} />
      ) : !view ? (
        <DashboardSkeleton />
      ) : (
        <motion.div
          variants={STAGGER_CONTAINER}
          initial="hidden"
          animate="show"
          className="space-y-4"
        >
          <motion.div variants={STAGGER_ITEM}>
            <ConnectionHero
              data={view}
              traffic={traffic}
              pending={pending}
              activeSimText={activeSimText}
              uptimeText={uptimeText}
            />
          </motion.div>
          {errorText || stale ? (
            <motion.div variants={STAGGER_ITEM}>
              <StatusChip tone="danger" className="w-fit">
                {errorText || tc("dataUpdateFailed")}
              </StatusChip>
            </motion.div>
          ) : null}
          <motion.div variants={STAGGER_ITEM}>
            <GaugeGrid data={view} assessmentText={assessmentText} />
          </motion.div>
          <motion.div variants={STAGGER_ITEM}>
            <SignalQualityCard data={view} />
          </motion.div>
          <motion.div variants={STAGGER_ITEM}>
            <TrendCard points={points} />
          </motion.div>
          <motion.div variants={STAGGER_ITEM}>
            <NetworkInfoCard
              data={view}
              compact={compact}
              onToggleCompact={toggle}
              traffic={traffic}
              activeSimText={activeSimText}
              uptimeText={uptimeText}
              assessmentText={assessmentText}
            />
          </motion.div>
        </motion.div>
      )}
    </>
  );
}
