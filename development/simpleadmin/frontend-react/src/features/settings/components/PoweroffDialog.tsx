// 关机后的"黑屏提示"(设计方案 §6.15:关机=送达探测后黑屏提示,对齐旧 sa-poweroff-overlay):
// 设备已断电、管理界面不再可用,需手动通电开机。恒暗色呈现(与主题无关的断电语义)。
import { PowerOffIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useT } from "@/lib/i18n";

export interface PoweroffDialogProps {
  open: boolean;
  onClose: () => void;
}

export function PoweroffDialog({ open, onClose }: PoweroffDialogProps) {
  const { t } = useT("settings");
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent
        showCloseButton={false}
        data-slot="poweroff-notice"
        className="border-white/10 bg-black/95 text-white sm:max-w-md"
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-white">
            <PowerOffIcon className="size-5" aria-hidden="true" />
            {t("devicePoweredOff")}
          </DialogTitle>
          <DialogDescription className="text-white/70">
            {t(
              "theDeviceHasBeenPoweredOffAndTheAdminInterfaceIsNoLongerAvailableToContinuePleasePowerOnTheDeviceManually",
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose} autoFocus>
            {t("gotIt")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
