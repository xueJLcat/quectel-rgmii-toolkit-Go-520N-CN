// 连通探测卡:内置目标(223.5.5.5/1.1.1.1/8.8.8.8)逐个探测 + 自定义目标(http/https 校验)
// + 一键全测(串行执行,进度指示)+ 结果时间线(延迟 ms 横条 + 状态 chip)。
// 页面明示 ICMP 被运营商屏蔽,采用 HTTP 探测(§6.12)。
import { LoaderCircleIcon, PlayIcon, RadarIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/lib/i18n";

import { BUILTIN_PROBE_TARGETS, isValidProbeTarget, latencyBarPercent } from "../lib";
import type { ProbeOutcome } from "../lib";

export interface ProbeProgress {
  current: number;
  total: number;
  target: string;
}

export interface ProbeCardProps {
  results: ProbeOutcome[];
  progress: ProbeProgress | null;
  onProbe: (target: string) => void;
  onProbeAll: (targets: string[]) => void;
}

export function ProbeCard({ results, progress, onProbe, onProbeAll }: ProbeCardProps) {
  const { t } = useT("diag");
  const [customTarget, setCustomTarget] = useState("");
  const busy = progress !== null || results.some((item) => item.state === "running");
  const maxLatency = Math.max(...results.map((item) => item.latencyMs ?? 0), 1);

  function probeCustom(): void {
    const target = customTarget.trim();
    if (!isValidProbeTarget(target)) {
      toast.error(t("invalidTarget"));
      return;
    }
    onProbe(target);
  }

  function probeAll(): void {
    const targets: string[] = [...BUILTIN_PROBE_TARGETS];
    const custom = customTarget.trim();
    if (custom) {
      if (!isValidProbeTarget(custom)) {
        toast.error(t("invalidTarget"));
        return;
      }
      targets.push(custom);
    }
    onProbeAll(targets);
  }

  return (
    <Panel
      domain="tools"
      icon={RadarIcon}
      title={t("connectivityProbe")}
      footer={t("probeNote")}
      tools={
        <Button size="sm" onClick={probeAll} disabled={busy}>
          {busy ? <LoaderCircleIcon className="animate-spin" /> : <PlayIcon />}
          {t("probeAll")}
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        {progress && (
          <p role="status" className="flex items-center gap-2 text-sm text-tools">
            <LoaderCircleIcon className="size-4 animate-spin" aria-hidden="true" />
            {t("probeProgress", {
              current: progress.current,
              total: progress.total,
              target: progress.target,
            })}
          </p>
        )}
        <div className="flex flex-col gap-2">
          <Label>{t("builtinTargets")}</Label>
          <div className="flex flex-wrap gap-2">
            {BUILTIN_PROBE_TARGETS.map((target) => (
              <Button
                key={target}
                variant="secondary"
                size="sm"
                disabled={busy}
                onClick={() => onProbe(target)}
              >
                <span className="font-mono">{target}</span>
                {t("probe")}
              </Button>
            ))}
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="diag-custom-target">{t("customTarget")}</Label>
          <div className="flex flex-wrap items-center gap-2">
            <Input
              id="diag-custom-target"
              type="text"
              className="w-72"
              placeholder={t("customTargetPlaceholder")}
              aria-label={t("customTarget")}
              value={customTarget}
              disabled={busy}
              onChange={(event) => setCustomTarget(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  probeCustom();
                }
              }}
            />
            <Button variant="secondary" onClick={probeCustom} disabled={busy}>
              {t("probe")}
            </Button>
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <Label>{t("probeResults")}</Label>
          {results.length === 0 ? (
            <p className="text-sm text-muted">{t("noProbeResults")}</p>
          ) : (
            <ul data-slot="probe-timeline" className="flex flex-col gap-3">
              {results.map((item) => (
                <li key={item.target} className="flex flex-col gap-1.5">
                  <div className="flex flex-wrap items-center gap-2 text-sm">
                    <span className="font-mono text-ink">{item.target}</span>
                    {item.state === "running" ? (
                      <StatusChip tone="info" pulse>
                        {t("probing")}
                      </StatusChip>
                    ) : item.ok ? (
                      <>
                        <StatusChip tone="success">{t("reachable")}</StatusChip>
                        {typeof item.statusCode === "number" && (
                          <Badge variant="muted">
                            {t("httpStatus", { code: item.statusCode })}
                          </Badge>
                        )}
                      </>
                    ) : (
                      <StatusChip tone="danger">{t("unreachable")}</StatusChip>
                    )}
                    {item.state === "done" && typeof item.latencyMs === "number" && (
                      <span className="ml-auto text-xs text-muted tabular-nums">
                        {t("latencyMs", { ms: item.latencyMs })}
                      </span>
                    )}
                  </div>
                  {item.state === "done" && typeof item.latencyMs === "number" && (
                    <div
                      role="img"
                      aria-label={`${t("latency")} ${item.latencyMs} ms`}
                      className="h-2 w-full overflow-hidden rounded-full bg-surface-3"
                    >
                      <div
                        className="h-full rounded-full transition-[width] duration-[var(--sa-dur-slow)] ease-sa"
                        style={{
                          width: `${latencyBarPercent(item.latencyMs, maxLatency)}%`,
                          backgroundImage: item.ok ? "var(--sa-grad-tools)" : "var(--sa-danger)",
                        }}
                      />
                    </div>
                  )}
                  {item.state === "done" && !item.ok && item.error && (
                    <p className="text-xs text-danger">{item.error}</p>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </Panel>
  );
}
