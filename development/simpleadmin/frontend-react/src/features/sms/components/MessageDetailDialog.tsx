// 短信详情对话框(设计方案 §6.9):全文多行安全渲染(whitespace-pre-wrap)、分段信息、
// 发件人/时间/存储索引;"回复"按钮带号码进发送表单。
// 动效 §5.1 模态规范:内容 scale 0.96→1 + fade spring(400,30),遮罩 fade + backdrop-blur(2px)。
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { CornerDownLeftIcon } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import type { Transition } from "motion/react";
import { Fragment } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DialogDescription, DialogPortal, DialogTitle } from "@/components/ui/dialog";
import { useT } from "@/lib/i18n";

import { formatDateTime } from "../lib";
import type { SmsRow } from "../lib";

const SPRING: Transition = { type: "spring", stiffness: 400, damping: 30 };
const FADE: Transition = { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] };

export interface MessageDetailDialogProps {
  row: SmsRow | null;
  onClose: () => void;
  /** 回复:带号码进发送表单并关闭详情 */
  onReply: (number: string) => void;
}

export function MessageDetailDialog({ row, onClose, onReply }: MessageDetailDialogProps) {
  const { t } = useT("sms");
  const open = row !== null;
  return (
    <DialogPrimitive.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogPortal forceMount>
        <AnimatePresence>
          {open && row && (
            <Fragment key="sms-detail">
              <DialogPrimitive.Overlay asChild forceMount>
                <motion.div
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={{ opacity: 0 }}
                  transition={FADE}
                  className="fixed inset-0 z-50 bg-[var(--sa-backdrop)] backdrop-blur-[2px]"
                />
              </DialogPrimitive.Overlay>
              <DialogPrimitive.Content asChild forceMount>
                <motion.div
                  initial={{ opacity: 0, scale: 0.96, x: "-50%", y: "-50%" }}
                  animate={{ opacity: 1, scale: 1, x: "-50%", y: "-50%" }}
                  exit={{ opacity: 0, scale: 0.96, x: "-50%", y: "-50%" }}
                  transition={SPRING}
                  className="fixed top-1/2 left-1/2 z-50 grid max-h-[85svh] w-full max-w-[calc(100%-2rem)] gap-4 overflow-y-auto rounded-lg border border-line bg-surface p-6 shadow-xl outline-none sm:max-w-lg"
                >
                  <div className="flex flex-col gap-1 pr-8">
                    <DialogTitle>{t("smsDetails")}</DialogTitle>
                    <DialogDescription className="flex flex-wrap items-center gap-2 text-sm">
                      <span className="font-mono text-ink">{row.sender || "—"}</span>
                      <Badge variant={row.storage === "SM" ? "info" : "default"}>
                        {row.storage}
                      </Badge>
                      {row.concatTotal && row.concatTotal > 1 ? (
                        <Badge variant="muted">
                          {t("concatBadge", { have: row.indices.length, total: row.concatTotal })}
                        </Badge>
                      ) : null}
                    </DialogDescription>
                  </div>

                  {/* 全文安全渲染:纯文本 + pre-wrap,不做任何 HTML 注入 */}
                  <p className="rounded-md bg-surface-2 p-4 text-sm whitespace-pre-wrap break-words text-ink">
                    {row.text === "" ? t("noContentPlaceholder") : row.text}
                  </p>

                  <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
                    <dt className="text-muted">{t("dateTimeLabel")}</dt>
                    <dd className="tabular-nums text-ink">
                      {row.date ? formatDateTime(row.date) : row.dateText}
                    </dd>
                    <dt className="text-muted">{t("segmentInfoLabel")}</dt>
                    <dd className="text-ink">
                      {row.concatTotal && row.concatTotal > 1
                        ? t("segmentInfoConcat", {
                            have: row.indices.length,
                            total: row.concatTotal,
                            seq: row.concatSeq ?? 0,
                          })
                        : t("segmentInfoSingle")}
                    </dd>
                    <dt className="text-muted">{t("storageIndexLabel")}</dt>
                    <dd className="font-mono text-ink">
                      {row.indices.map((index) => `${row.storage}:${index}`).join(", ") || "—"}
                    </dd>
                  </dl>

                  <div className="flex justify-end gap-2">
                    <Button variant="secondary" onClick={onClose}>
                      {t("close", { ns: "common" })}
                    </Button>
                    <Button onClick={() => onReply(row.sender)}>
                      <CornerDownLeftIcon />
                      {t("reply")}
                    </Button>
                  </div>
                </motion.div>
              </DialogPrimitive.Content>
            </Fragment>
          )}
        </AnimatePresence>
      </DialogPortal>
    </DialogPrimitive.Root>
  );
}
