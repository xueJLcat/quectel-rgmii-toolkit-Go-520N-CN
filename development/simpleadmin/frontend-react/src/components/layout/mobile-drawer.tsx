// MobileDrawer(设计方案 §4.1 移动端):Radix Dialog 左侧滑入抽屉(motion x:-100%→0 spring 400/30,§5.1),
// 内容复用 Sidebar 的 NavContent;遮罩点击/ESC 关闭(Radix dismiss → onOpenChange),开关读 ui store mobileNavOpen。
// 进出场结构与 ConfirmDialog 同款:Portal forceMount + AnimatePresence + asChild forceMount。
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { AnimatePresence, motion } from "motion/react";
import type { Transition } from "motion/react";
import { Fragment } from "react";

import { Dialog, DialogPortal, DialogTitle } from "@/components/ui/dialog";
import { useT } from "@/lib/i18n";
import { useUiStore } from "@/stores/ui";
import { NavContent } from "./sidebar";

const SPRING: Transition = { type: "spring", stiffness: 400, damping: 30 };
const FADE: Transition = { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] };

export function MobileDrawer() {
  const open = useUiStore((state) => state.mobileNavOpen);
  const setOpen = useUiStore((state) => state.setMobileNavOpen);
  const { t } = useT("nav");
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogPortal forceMount>
        <AnimatePresence>
          {open && (
            <Fragment key="mobile-drawer">
              <DialogPrimitive.Overlay asChild forceMount>
                <motion.div
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={{ opacity: 0 }}
                  transition={FADE}
                  className="fixed inset-0 z-50 bg-[var(--sa-backdrop)] backdrop-blur-[2px] lg:hidden"
                />
              </DialogPrimitive.Overlay>
              <DialogPrimitive.Content asChild forceMount aria-describedby={undefined}>
                <motion.div
                  data-slot="mobile-drawer"
                  initial={{ x: "-100%" }}
                  animate={{ x: 0 }}
                  exit={{ x: "-100%" }}
                  transition={SPRING}
                  className="fixed inset-y-0 left-0 z-50 flex w-64 max-w-[85vw] flex-col bg-surface shadow-xl lg:hidden"
                >
                  <DialogTitle className="sr-only">{t("mainNavigation")}</DialogTitle>
                  <NavContent onNavigate={() => setOpen(false)} />
                </motion.div>
              </DialogPrimitive.Content>
            </Fragment>
          )}
        </AnimatePresence>
      </DialogPortal>
    </Dialog>
  );
}
