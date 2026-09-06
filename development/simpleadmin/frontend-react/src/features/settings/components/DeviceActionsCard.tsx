// 设备操作 DangerZone 卡(行为对齐旧 settings.js 三入口):
// - 重启模块(AT+CFUN=1,1):confirm → 倒计时遮罩 40s → systemData("reboot");
//   ok:false 或传输失败 = 模块拒绝/未送达 → 取消倒计时 + 失败提示(旧 startReboot)。
// - 重启设备(reboot):confirm → 乐观倒计时 60s → systemData("reboot_device");
//   仅后端明确返回失败才取消倒计时,WS 中断属停机常态(旧 startSystemReboot)。
// - 关机(poweroff):confirm requireWord"关机" → 倒计时遮罩(覆盖探测预算)→ 送达探测:
//   成功路径轮询 /login.html 直到不可达 → 黑屏提示;传输失败延时单探,仍可达 = 未送达提示(旧 startPoweroff)。
import { PowerOffIcon, RefreshCwIcon, RotateCwIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { DangerZone } from "@/components/common";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";
import { useRebootStore } from "@/stores/reboot";

import { useSystemAction } from "../hooks";
import {
  POWER_OFF_PROBE,
  confirmPoweroffByProbe,
  probeDeviceAlive,
  waitForDevicePoweredOff,
} from "../lib";
import { PoweroffDialog } from "./PoweroffDialog";

/** 旧 Reboot.request → startReboot:countdown(DEFAULT_SECONDS=40) */
const MODULE_REBOOT_SECONDS = 40;
/** 旧 startSystemReboot:countdown(60) */
const DEVICE_REBOOT_SECONDS = 60;

export function DeviceActionsCard() {
  const { t } = useT("settings");
  const confirm = useConfirmStore((state) => state.confirm);
  const startCountdown = useRebootStore((state) => state.startCountdown);
  const closeCountdown = useRebootStore((state) => state.closeCountdown);
  const systemAction = useSystemAction();
  const [poweroffNoticeOpen, setPoweroffNoticeOpen] = useState(false);
  const cancelledRef = useRef(false);

  useEffect(() => {
    cancelledRef.current = false;
    return () => {
      // 卸载终止关机探测循环,避免卸载后 setState
      cancelledRef.current = true;
    };
  }, []);

  async function handleModuleReboot(): Promise<void> {
    const ok = await confirm({
      title: t("atCommandReboot"),
      message: t(
        "thisWillRebootTheCellularModuleViaAtCommandAtCfun11TakesAbout40SecondsToRecoverContinue",
      ),
      confirmText: t("reboot"),
      danger: true,
    });
    if (!ok) return;
    startCountdown({
      seconds: MODULE_REBOOT_SECONDS,
      label: t("rebootingPleaseWaitDoNotCloseThisPage"),
    });
    try {
      const data = await systemAction.mutateAsync({ action: "reboot" });
      // 后端 reboot 只返回 {ok,response}:ok:false 即模块明确拒绝重启,必须中止倒计时并提示
      if (data?.ok === false) {
        closeCountdown();
        toast.error(t("rebootFailed"));
      }
    } catch {
      // 请求未送达(传输失败)同样不能假装重启成功
      closeCountdown();
      toast.error(t("rebootFailed"));
    }
  }

  async function handleDeviceReboot(): Promise<void> {
    const ok = await confirm({
      title: t("deviceReboot"),
      message: t(
        "thisWillRebootTheEntireDeviceTheAdminInterfaceIsUnavailableForAbout60SecondsContinue",
      ),
      confirmText: t("rebootDevice"),
      danger: true,
    });
    if (!ok) return;
    // 乐观倒计时:命令发出后设备随即开始停机,WS 中断属常态,
    // 仅当后端明确返回失败(命令无法启动)时取消倒计时
    startCountdown({
      seconds: DEVICE_REBOOT_SECONDS,
      label: t("rebootingDevicePleaseWaitDoNotCloseThisPage"),
    });
    try {
      const data = await systemAction.mutateAsync({ action: "reboot_device" });
      if (data?.ok === false && data.error) {
        closeCountdown();
        toast.error(String(data.error));
      }
    } catch {
      // 传输中断不取消倒计时(设备大概率已在停机)
    }
  }

  async function handlePowerOff(): Promise<void> {
    const ok = await confirm({
      title: t("powerOff"),
      message: t(
        "thisWillPowerOffTheDeviceItCannotRecoverByItselfAndMustBePoweredOnManuallyContinue",
      ),
      confirmText: t("powerOff"),
      danger: true,
      // 高危操作:输入确认词"关机"才允许继续(设计方案 §5.2)
      requireWord: t("powerOff"),
    });
    if (!ok) return;
    startCountdown({
      seconds: POWER_OFF_PROBE.overlaySeconds,
      label: t("poweringOffPleaseWait"),
    });
    try {
      const data = await systemAction.mutateAsync({ action: "poweroff_device" });
      if (data?.ok === false) {
        closeCountdown();
        toast.error(String(data.error ?? "") || t("common:operationFailed"));
        return;
      }
      const outcome = await waitForDevicePoweredOff({
        probe: () => probeDeviceAlive(),
        isCancelled: () => cancelledRef.current,
      });
      if (outcome === "cancelled") return;
      closeCountdown();
      setPoweroffNoticeOpen(true);
    } catch {
      const outcome = await confirmPoweroffByProbe({
        probe: () => probeDeviceAlive(),
        isCancelled: () => cancelledRef.current,
      });
      if (outcome === "cancelled") return;
      closeCountdown();
      if (outcome === "alive") {
        toast.error(t("thePowerOffCommandWasNotDeliveredAndTheDeviceIsStillRunningPleaseRetry"));
        return;
      }
      setPoweroffNoticeOpen(true);
    }
  }

  return (
    <>
      <DangerZone title={t("deviceActions")}>
        <div className="grid gap-4 lg:grid-cols-3">
          <div className="flex flex-col items-start gap-2">
            <Button
              variant="danger"
              onClick={() => void handleModuleReboot()}
              disabled={systemAction.isPending}
            >
              <RotateCwIcon />
              {t("atCommandReboot")}
            </Button>
            <p className="text-sm text-muted">
              {t(
                "rebootsTheCellularModuleViaAtCommandAtCfun11TakesAbout40SecondsToRecoverTheDeviceItselfKeepsRunning",
              )}
            </p>
          </div>
          <div className="flex flex-col items-start gap-2">
            <Button
              variant="danger"
              onClick={() => void handleDeviceReboot()}
              disabled={systemAction.isPending}
            >
              <RefreshCwIcon />
              {t("deviceReboot")}
            </Button>
            <p className="text-sm text-muted">
              {t("rebootsTheEntireDeviceRebootTheAdminInterfaceIsUnavailableForAbout60Seconds")}
            </p>
          </div>
          <div className="flex flex-col items-start gap-2">
            <Button
              variant="danger"
              onClick={() => void handlePowerOff()}
              disabled={systemAction.isPending}
            >
              <PowerOffIcon />
              {t("powerOff")}
            </Button>
            <p className="text-sm text-muted">
              {t("powersOffTheDevicePoweroffManualPowerOnIsRequiredToRecover")}
            </p>
          </div>
        </div>
      </DangerZone>
      <PoweroffDialog open={poweroffNoticeOpen} onClose={() => setPoweroffNoticeOpen(false)} />
    </>
  );
}
