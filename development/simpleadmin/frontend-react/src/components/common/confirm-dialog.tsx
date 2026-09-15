// 确认框(全站单一实现,设计方案 §3.3:Radix Dialog 焦点陷阱/ESC/aria 全赠 + motion spring 进出):
// 由 stores/confirm 驱动——confirm() 挂起 promise;确认 → settle(true),取消/ESC/点遮罩 → settle(false),
// 语义对齐 Vue 版 SimpleAdmin.UI.confirm。danger 时确认按钮红色 danger 变体;
// requireWord 时内嵌 Input,输入匹配确认词才启用确认按钮(关机/恢复出厂级高危操作)。
// 动效 §5.1:内容 scale 0.96→1 + fade,spring(stiffness 400, damping 30);遮罩 fade + backdrop-blur(2px)。
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { AnimatePresence, motion } from "motion/react";
import type { Transition } from "motion/react";
import { Fragment, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogPortal, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";
import type { ConfirmOptions } from "@/stores/confirm";

const SPRING: Transition = { type: "spring", stiffness: 400, damping: 30 };
const FADE: Transition = { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] };

export function ConfirmDialog() {
  const options = useConfirmStore((state) => state.options);
  const settle = useConfirmStore((state) => state.settle);
  const { t } = useT();
  const open = options !== null;

  // 退场动画需要内容:options 结算清空后保留上一代快照供 AnimatePresence exit 渲染。
  const [lastOptions, setLastOptions] = useState<ConfirmOptions | null>(null);
  useEffect(() => {
    if (options) setLastOptions(options);
  }, [options]);
  const view = options ?? lastOptions;

  const [word, setWord] = useState("");
  useEffect(() => {
    if (open) setWord("");
  }, [open]);

  const okRef = useRef<HTMLButtonElement>(null);
  const wordRef = useRef<HTMLInputElement>(null);

  if (!view) return null;
  const wordConfirmed = !view.requireWord || word.trim() === view.requireWord;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // ESC / 点击遮罩等 Radix dismiss 路径统一按取消结算(对齐 Vue 版 finish(false))。
        if (!next) settle(false);
      }}
    >
      <DialogPortal forceMount>
        <AnimatePresence>
          {open && view && (
            <Fragment key="confirm-dialog">
              <DialogPrimitive.Overlay asChild forceMount>
                <motion.div
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={{ opacity: 0 }}
                  transition={FADE}
                  className="fixed inset-0 z-50 bg-[var(--sa-backdrop)] backdrop-blur-[2px]"
                />
              </DialogPrimitive.Overlay>
              <DialogPrimitive.Content
                asChild
                forceMount
                // 无 message 时显式解除 aria-describedby,免 Radix 缺 Description 告警
                {...(view.message ? {} : { "aria-describedby": undefined })}
                onOpenAutoFocus={(event) => {
                  // 焦点管理:有确认词先落输入框,否则直接落确认按钮(对齐 Vue 版 focus: okBtn)。
                  event.preventDefault();
                  if (view.requireWord) wordRef.current?.focus();
                  else okRef.current?.focus();
                }}
              >
                <motion.div
                  initial={{ opacity: 0, scale: 0.96, x: "-50%", y: "-50%" }}
                  animate={{ opacity: 1, scale: 1, x: "-50%", y: "-50%" }}
                  exit={{ opacity: 0, scale: 0.96, x: "-50%", y: "-50%" }}
                  transition={SPRING}
                  className="fixed top-1/2 left-1/2 z-50 grid w-full max-w-[calc(100%-2rem)] gap-4 rounded-lg border border-line bg-surface p-6 shadow-xl outline-none sm:max-w-md"
                >
                  <div className="flex flex-col gap-2">
                    <DialogTitle>{view.title}</DialogTitle>
                    {view.message && <DialogDescription>{view.message}</DialogDescription>}
                  </div>
                  {view.requireWord && (
                    <Input
                      ref={wordRef}
                      value={word}
                      placeholder={view.requireWord}
                      autoComplete="off"
                      invalid={word.length > 0 && !wordConfirmed}
                      onChange={(event) => setWord(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" && wordConfirmed) settle(true);
                      }}
                    />
                  )}
                  <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                    <Button variant="secondary" onClick={() => settle(false)}>
                      {view.cancelText ?? t("cancel")}
                    </Button>
                    <Button
                      ref={okRef}
                      variant={view.danger ? "danger" : "default"}
                      disabled={!wordConfirmed}
                      onClick={() => settle(true)}
                    >
                      {view.confirmText ?? t("confirm")}
                    </Button>
                  </div>
                </motion.div>
              </DialogPrimitive.Content>
            </Fragment>
          )}
        </AnimatePresence>
      </DialogPortal>
    </Dialog>
  );
}
