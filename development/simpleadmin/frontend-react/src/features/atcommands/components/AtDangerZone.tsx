// 危险区(设计方案 §6.10,DangerZone 组件 rose 边框):
// AT&F 恢复出厂——危险确认 → reset_at 成功才弹重启确认(对齐旧版);
// 重启模块按钮——共享 RebootOverlay(reboot store,真实时间戳倒计时)。
// in-flight(sending/resetting)期间全部禁用。
import { PowerIcon, RotateCcwIcon } from "lucide-react";

import { DangerZone } from "@/components/common";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";

export interface AtDangerZoneProps {
  disabled: boolean;
  onResetAt: () => void;
  onReboot: () => void;
}

export function AtDangerZone({ disabled, onResetAt, onReboot }: AtDangerZoneProps) {
  const { t } = useT("atcommands");
  return (
    <DangerZone title={t("dangerZone")} description={t("dangerZoneDescription")}>
      <div className="flex flex-wrap gap-2">
        <Button variant="danger" disabled={disabled} onClick={onResetAt}>
          <RotateCcwIcon />
          {t("resetAtF")}
        </Button>
        <Button variant="outline" disabled={disabled} onClick={onReboot}>
          <PowerIcon />
          {t("rebootModem")}
        </Button>
      </div>
    </DangerZone>
  );
}
