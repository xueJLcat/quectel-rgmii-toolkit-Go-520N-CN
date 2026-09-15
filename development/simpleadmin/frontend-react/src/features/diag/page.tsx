// 网络诊断页(设计方案 §6.12,全新,域色 tools/cyan):连通探测(HTTP 探测,ICMP 常被运营商屏蔽)
// + DNS 查询 + 延迟对比图;探测结果状态提升到本页,时间线与图表共享同一轮数据。
// 一键全测串行执行(await 上一目标结果后才发起下一个),带进度指示;面板 stagger 40ms 进场。
import { motion } from "motion/react";
import type { Variants } from "motion/react";
import { useState } from "react";

import { PageHeader } from "@/components/layout/page-header";
import { useT } from "@/lib/i18n";

import { DnsCard } from "./components/DnsCard";
import { LatencyChartCard } from "./components/LatencyChartCard";
import { ProbeCard } from "./components/ProbeCard";
import type { ProbeProgress } from "./components/ProbeCard";
import { useHttpProbe } from "./hooks";
import type { ProbeOutcome } from "./lib";

const STAGGER_PARENT: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.04 } },
};

const STAGGER_CHILD: Variants = {
  hidden: { opacity: 0, y: 12 },
  show: { opacity: 1, y: 0, transition: { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] } },
};

export default function DiagPage() {
  const { t } = useT("diag");
  const { t: tn } = useT("nav");
  const probe = useHttpProbe();
  const [results, setResults] = useState<ProbeOutcome[]>([]);
  const [progress, setProgress] = useState<ProbeProgress | null>(null);

  function upsertResult(outcome: ProbeOutcome): void {
    setResults((previous) => {
      const index = previous.findIndex((item) => item.target === outcome.target);
      if (index === -1) return [...previous, outcome];
      const next = previous.slice();
      next[index] = outcome;
      return next;
    });
  }

  async function probeOne(rawTarget: string): Promise<void> {
    const target = rawTarget.trim();
    upsertResult({ target, state: "running" });
    try {
      const data = await probe.mutateAsync(target);
      if (data?.ok === false) {
        upsertResult({
          target,
          state: "done",
          ok: false,
          latencyMs: data.latencyMs,
          error: String(data.error ?? ""),
        });
        return;
      }
      upsertResult({
        target,
        state: "done",
        ok: true,
        statusCode: data?.statusCode,
        latencyMs: data?.latencyMs,
      });
    } catch (err) {
      // 传输层失败同样落为该目标的失败条目,不做页面级错误(任务契约)
      const message = err instanceof Error && err.message ? err.message : t("common:unknownError");
      upsertResult({ target, state: "done", ok: false, error: message });
    }
  }

  async function probeAll(targets: string[]): Promise<void> {
    if (progress !== null || targets.length === 0) return;
    for (let index = 0; index < targets.length; index++) {
      setProgress({ current: index + 1, total: targets.length, target: targets[index] });
      // 串行:等上一个目标出结果再探测下一个
      await probeOne(targets[index]);
    }
    setProgress(null);
  }

  return (
    <>
      <PageHeader domain="tools" title={tn("diag")} description={t("pageDescription")} />
      <motion.div
        variants={STAGGER_PARENT}
        initial="hidden"
        animate="show"
        className="grid grid-cols-1 gap-4 xl:grid-cols-2"
      >
        <motion.div variants={STAGGER_CHILD} className="xl:col-span-2">
          <ProbeCard
            results={results}
            progress={progress}
            onProbe={(target) => void probeOne(target)}
            onProbeAll={(targets) => void probeAll(targets)}
          />
        </motion.div>
        <motion.div variants={STAGGER_CHILD}>
          <DnsCard />
        </motion.div>
        <motion.div variants={STAGGER_CHILD}>
          <LatencyChartCard results={results} />
        </motion.div>
      </motion.div>
    </>
  );
}
