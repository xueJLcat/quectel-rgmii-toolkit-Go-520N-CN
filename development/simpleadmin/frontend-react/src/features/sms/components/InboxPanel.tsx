// 收件箱面板(设计方案 §6.9):会话式列表——发件人头像(名字哈希取域内色相圆形)、单行摘要、
// 时间(simpleadmin-time 解析口径)、存储位置徽章 ME/SM、长短信 concat 合并标记 n/m;
// 多选工具栏(全选/反选/删除选中/清空全部,均经 ConfirmDialog 二次确认);
// pending 保留旧值 + "获取中…" chip(§5.2 绝不显示假空态);点击行开详情对话框。
import { InboxIcon, LoaderCircleIcon, RefreshCwIcon, Trash2Icon } from "lucide-react";

import { EmptyState, ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

import type { UseSmsInbox } from "../hooks";
import { avatarHue, avatarInitial, messagePreview } from "../lib";
import type { SmsRow } from "../lib";
import { MessageDetailDialog } from "./MessageDetailDialog";

function SenderAvatar({ name }: { name: string }) {
  const hue = avatarHue(name);
  return (
    <span
      aria-hidden="true"
      className="grid size-9 shrink-0 place-items-center rounded-full text-sm font-semibold text-white"
      style={{
        backgroundImage: `linear-gradient(135deg, hsl(${hue} 70% 55%), hsl(${hue + 30} 75% 50%))`,
      }}
    >
      {avatarInitial(name)}
    </span>
  );
}

function MessageRow({
  row,
  index,
  checked,
  onToggle,
  onOpen,
}: {
  row: SmsRow;
  index: number;
  checked: boolean;
  onToggle: () => void;
  onOpen: () => void;
}) {
  const { t } = useT("sms");
  const preview = messagePreview(row);
  const multiPart = (row.concatTotal ?? 0) > 1;
  return (
    <li
      className={cn(
        "flex items-center gap-3 border-b border-line px-2 py-2.5 last:border-b-0",
        checked && "bg-comm-soft",
      )}
    >
      <Checkbox
        checked={checked}
        onCheckedChange={onToggle}
        aria-label={t("selectSms", { ns: "common", index: index + 1 })}
      />
      <button
        type="button"
        onClick={onOpen}
        className="flex min-w-0 flex-1 items-center gap-3 rounded-sm text-left outline-none focus-visible:ring-[3px] focus-visible:ring-accent/40"
      >
        <SenderAvatar name={row.sender} />
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-2">
            <span className="truncate text-sm font-semibold text-ink">
              {row.sender || t("unknownSender")}
            </span>
            <Badge variant={row.storage === "SM" ? "info" : "default"}>{row.storage}</Badge>
            {multiPart && (
              <Badge variant="muted">
                {t("concatBadge", { have: row.indices.length, total: row.concatTotal ?? 0 })}
              </Badge>
            )}
            <span className="ml-auto shrink-0 text-xs tabular-nums text-muted">{row.dateText}</span>
          </span>
          <span className="mt-0.5 block truncate text-sm text-muted">
            {preview === "" ? t("noContentPlaceholder") : preview}
          </span>
        </span>
      </button>
    </li>
  );
}

export interface InboxPanelProps {
  inbox: UseSmsInbox;
  /** 详情"回复":带号码进发送表单(页面级联动) */
  onReply: (number: string) => void;
}

export function InboxPanel({ inbox, onReply }: InboxPanelProps) {
  const { t } = useT("sms");
  const {
    rows,
    pending,
    loadFailed,
    initialLoading,
    selected,
    allSelected,
    detailIndex,
    toggleRow,
    toggleAll,
    invertSelection,
    openDetail,
    closeDetail,
    refresh,
    deleteSelected,
    deleteAll,
  } = inbox;

  const selectedCount = selected.size;
  const detailRow = detailIndex !== null ? (rows[detailIndex] ?? null) : null;

  return (
    <Panel
      domain="comm"
      icon={InboxIcon}
      title={t("inbox")}
      tools={
        <>
          {pending && (
            <StatusChip tone="warning" pulse>
              {t("fetching", { ns: "common" })}
            </StatusChip>
          )}
          <Button
            variant="ghost"
            size="icon"
            onClick={() => void refresh()}
            aria-label={t("refreshInbox")}
          >
            <RefreshCwIcon />
          </Button>
        </>
      }
      footer={t("selectedCount", { count: selectedCount })}
    >
      {/* 多选工具栏(破坏性操作全部经 confirm,§5.2) */}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Button variant="secondary" size="sm" onClick={toggleAll} disabled={rows.length === 0}>
          {allSelected ? t("cancelSelectAll") : t("selectAll")}
        </Button>
        <Button
          variant="secondary"
          size="sm"
          onClick={invertSelection}
          disabled={rows.length === 0}
        >
          {t("invertSelection")}
        </Button>
        <Button
          variant="danger"
          size="sm"
          onClick={() => void deleteSelected()}
          disabled={selectedCount === 0}
        >
          <Trash2Icon />
          {t("deleteSelected")}
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void deleteAll()}
          disabled={rows.length === 0}
        >
          {t("clearSms")}
        </Button>
        {initialLoading && <LoaderCircleIcon className="ml-auto size-4 animate-spin text-muted" />}
      </div>

      {loadFailed && (
        <ErrorRetry
          className="mb-3"
          message={t("failedToReadSms")}
          onRetry={() => void refresh()}
        />
      )}

      {initialLoading ? (
        <div className="flex flex-col gap-3" aria-busy="true">
          {[0, 1, 2].map((key) => (
            <div key={key} className="flex items-center gap-3">
              <Skeleton className="size-9 rounded-full" />
              <div className="flex-1 space-y-2">
                <Skeleton className="h-3.5 w-1/3" />
                <Skeleton className="h-3 w-2/3" />
              </div>
            </div>
          ))}
        </div>
      ) : rows.length === 0 && !pending ? (
        // pending(开机保护期)绝不显示假空态:仅确定无短信时才给空态
        <EmptyState icon={InboxIcon} title={t("noSms")} description={t("noSmsHint")} />
      ) : (
        // 桌面端长列表卡内滚动(口径同 atcommands HistoryPanel):行高不随消息数暴涨,避免把同行 Webhook 卡拉出巨大空白
        <ul className="-mx-2 xl:max-h-[30rem] xl:overflow-y-auto">
          {rows.map((row, index) => (
            <MessageRow
              key={`${row.storage}:${row.indices.join(",")}:${index}`}
              row={row}
              index={index}
              checked={selected.has(index)}
              onToggle={() => toggleRow(index)}
              onOpen={() => openDetail(index)}
            />
          ))}
        </ul>
      )}

      <MessageDetailDialog
        row={detailRow}
        onClose={closeDetail}
        onReply={(number) => {
          closeDetail();
          onReply(number);
        }}
      />
    </Panel>
  );
}
