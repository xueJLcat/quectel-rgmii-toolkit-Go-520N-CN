// 连接状态英雄条(设计方案 §6.2 区块①):域色渐变卡,internetConnection(服务端固定中文枚举)
// 已连接 → emerald 渐变(--sa-grad-network)/未连接 → rose 渐变(--sa-grad-system);
// 左侧运营商/网络模式/激活 SIM/在线时长,右侧 DL/UL 实时速率 + 累计流量 count-up。
import { ArrowDownIcon, ArrowUpIcon, WifiIcon, WifiOffIcon } from "lucide-react";
import type { CSSProperties } from "react";

import { StatusChip } from "@/components/common";
import type { DashboardData } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { CountUpBytes } from "./CountUpBytes";
import type { TrafficView } from "../lib";

export interface ConnectionHeroProps {
  data: DashboardData | undefined;
  traffic: TrafficView;
  /** pending(模块就绪中)时叠加 StatusChip,旧值保留展示 */
  pending: boolean;
  activeSimText: string;
  uptimeText: string;
}

function HeroStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-white/70">{label}</div>
      <div className="truncate text-sm font-semibold text-white" title={value}>
        {value}
      </div>
    </div>
  );
}

function SpeedStat({
  icon: Icon,
  label,
  speed,
  total,
  bytes,
}: {
  icon: typeof ArrowDownIcon;
  label: string;
  speed: string;
  total: string;
  bytes: number | null;
}) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span
        aria-hidden="true"
        className="grid size-8 shrink-0 place-items-center rounded-sm bg-white/15 text-white"
      >
        <Icon className="size-4" />
      </span>
      <div className="min-w-0">
        <div className="text-xs text-white/70">{label}</div>
        <div className="truncate text-base font-semibold text-white tabular-nums" title={speed}>
          {speed}
        </div>
        <div className="text-xs text-white/70 tabular-nums">
          <CountUpBytes bytes={bytes} fallback={total} />
        </div>
      </div>
    </div>
  );
}

export function ConnectionHero({
  data,
  traffic,
  pending,
  activeSimText,
  uptimeText,
}: ConnectionHeroProps) {
  const { t } = useT("dashboard");
  const { t: tc } = useT();
  // 服务端 internetConnection 为固定中文枚举(page_dashboard.go:已连接/未连接,先例 Topbar VitalChips)
  const online = data?.internetConnection === "已连接";
  return (
    <section
      data-slot="connection-hero"
      data-online={online}
      className={cn("rounded-md p-4 shadow-md sm:p-5", "bg-[image:var(--hero-grad)] text-white")}
      style={
        {
          "--hero-grad": online ? "var(--sa-grad-network)" : "var(--sa-grad-system)",
        } as CSSProperties
      }
    >
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            {online ? (
              <WifiIcon className="size-6 shrink-0" aria-hidden="true" />
            ) : (
              <WifiOffIcon className="size-6 shrink-0" aria-hidden="true" />
            )}
            <span className="text-2xl font-semibold">
              {online ? t("connected") : t("disconnected")}
            </span>
            {pending && (
              <StatusChip tone="warning" pulse className="bg-white/20 text-white">
                {tc("getting", { thing: t("networkInfo") })}
              </StatusChip>
            )}
          </div>
          <div className="mt-4 grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4">
            <HeroStat label={t("carrier")} value={data?.network_provider || "-"} />
            <HeroStat label={t("networkMode")} value={data?.network_mode || "-"} />
            <HeroStat label={t("activeSim")} value={activeSimText} />
            <HeroStat label={t("uptime")} value={uptimeText} />
          </div>
        </div>
        <div className="grid shrink-0 grid-cols-2 gap-4 border-t border-white/20 pt-4 lg:border-t-0 lg:pt-0 lg:border-s lg:ps-6">
          <SpeedStat
            icon={ArrowDownIcon}
            label={t("downloadRate")}
            speed={traffic.dl}
            total={traffic.rxTotal}
            bytes={traffic.rxBytes}
          />
          <SpeedStat
            icon={ArrowUpIcon}
            label={t("uploadRate")}
            speed={traffic.ul}
            total={traffic.txTotal}
            bytes={traffic.txBytes}
          />
        </div>
      </div>
    </section>
  );
}
