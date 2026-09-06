// 存储可视化条(设计方案 §6.9 新增点):ME x/255、SM x/40 双渐变占用条 + SMSC 号码。
// 数据源 AT+CPMS?/AT+CSCA? 组合命令经 at_data manual_at 低频拉取(useSmsStorage,60s),
// 占用 >80% 转 amber、>90% 转 rose 预警;渐变端色取 charts DOMAIN_GRADIENTS(域色纪律)。
import { DatabaseIcon, LoaderCircleIcon, RefreshCwIcon } from "lucide-react";

import { DOMAIN_GRADIENTS } from "@/components/charts";
import { CopyButton, EmptyState } from "@/components/common";
import { Button } from "@/components/ui/button";
import { Panel } from "@/components/layout/panel";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";

import { storagePercent, storageTone } from "../lib";
import type { SmsStorageInfo, SmsStorageStatus, StorageTone } from "../lib";

const TONE_GRADIENT: Record<StorageTone, [string, string]> = {
  domain: DOMAIN_GRADIENTS.comm,
  warning: DOMAIN_GRADIENTS.security,
  danger: DOMAIN_GRADIENTS.system,
};

function StorageBar({ label, info }: { label: string; info: SmsStorageInfo | null }) {
  const { t } = useT("sms");
  if (!info) {
    return (
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between gap-2 text-sm">
          <span className="text-muted">{label}</span>
          <span className="text-muted">—</span>
        </div>
        <Skeleton className="h-1.5 w-full rounded-full" />
      </div>
    );
  }
  const [from, to] = TONE_GRADIENT[storageTone(info.used, info.total)];
  const percent = storagePercent(info);
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2 text-sm">
        <span className="text-soft">{label}</span>
        <span className="font-semibold tabular-nums text-ink">
          {t("storageUsageValue", { used: info.used, total: info.total })}
          <span className="ml-1 text-xs font-normal text-muted">({Math.round(percent)}%)</span>
        </span>
      </div>
      {/* 渐变占用条按域色纪律自绘(轨道/过渡口径与 charts MetricBar 一致) */}
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-surface-3">
        <div
          className="h-full rounded-full transition-[width] duration-[var(--sa-dur-slow)] ease-sa"
          style={{
            width: `${percent}%`,
            backgroundImage: `linear-gradient(135deg, ${from} 0%, ${to} 100%)`,
          }}
        />
      </div>
    </div>
  );
}

export interface StoragePanelProps {
  status: SmsStorageStatus | undefined;
  isLoading: boolean;
  isError: boolean;
  onRefresh: () => void;
}

export function StoragePanel({ status, isLoading, isError, onRefresh }: StoragePanelProps) {
  const { t } = useT("sms");
  return (
    <Panel
      domain="comm"
      icon={DatabaseIcon}
      title={t("storageTitle")}
      tools={
        <Button variant="ghost" size="icon" onClick={onRefresh} aria-label={t("refreshStorage")}>
          {isLoading ? <LoaderCircleIcon className="animate-spin" /> : <RefreshCwIcon />}
        </Button>
      }
      footer={t("storageFooterHint")}
    >
      {isError && !status ? (
        <EmptyState title={t("storageUnavailable")} description={t("storageLowFrequencyHint")} />
      ) : (
        // 卡片被网格拉高时:占用条组贴顶、SMSC 行贴底,消除卡内大片空白
        <div className="flex flex-1 flex-col justify-between gap-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <StorageBar label={t("storageMeLabel")} info={status?.me ?? null} />
            <StorageBar label={t("storageSmLabel")} info={status?.sm ?? null} />
          </div>
          <div className="flex flex-wrap items-center gap-2 border-t border-line pt-3 text-sm">
            <span className="text-muted">{t("smscNumber")}</span>
            {status?.smsc ? (
              <>
                <span className="font-mono text-ink">{status.smsc}</span>
                <CopyButton text={status.smsc} />
              </>
            ) : (
              <span className="text-muted">—</span>
            )}
          </div>
        </div>
      )}
    </Panel>
  );
}
