// 设备信息页(设计方案 §6.16,域色 system):定义列表卡(值 mono + CopyButton)+ 页脚版本条
// (meta[name="sa-version"] 回退空 + 代码库链接)。行为对齐旧版 www/js/pages/deviceinfo.js:
// 5s 轮询且 document.hidden 暂停;pending(开机/重启保护期)保留旧值 + StatusChip,绝不按失败处理;
// data.error/传输失败 → ErrorRetry 横幅(retry 退避),已有旧值时横幅与数据并存。面板进场 stagger 40ms(§5.1)。
import { SmartphoneIcon } from "lucide-react";
import { motion } from "motion/react";

import { ErrorRetry, StatusChip } from "@/components/common";
import { PageHeader } from "@/components/layout/page-header";
import { Panel } from "@/components/layout/panel";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";
import { FooterBar } from "./components/FooterBar";
import { InfoList } from "./components/InfoList";
import { STAGGER_CONTAINER, STAGGER_ITEM } from "./components/stagger";
import { useDeviceInfoStream } from "./hooks";

export default function DeviceinfoPage() {
  const { t } = useT("deviceinfo");
  const { t: tc } = useT();
  const { view, pending, failed, isFetching, refetch } = useDeviceInfoStream();
  return (
    <>
      <PageHeader domain="system" title={t("nav:deviceInfo")} />
      <motion.div
        variants={STAGGER_CONTAINER}
        initial="hidden"
        animate="show"
        className="space-y-4"
      >
        <motion.div variants={STAGGER_ITEM}>
          <Panel
            domain="system"
            icon={SmartphoneIcon}
            title={t("nav:deviceInfo")}
            tools={
              pending ? (
                <StatusChip tone="info" pulse>
                  {tc("moduleWarmingUp")}
                </StatusChip>
              ) : undefined
            }
          >
            {failed && !view ? (
              <ErrorRetry
                message={t("failedToLoadDeviceInfo")}
                onRetry={refetch}
                retrying={isFetching}
              />
            ) : !view ? (
              <div aria-busy="true" className="space-y-2">
                {Array.from({ length: 8 }, (_, index) => (
                  <Skeleton key={index} className="h-9 w-full" />
                ))}
              </div>
            ) : (
              <>
                {failed && (
                  <ErrorRetry
                    className="mb-3"
                    message={t("failedToLoadDeviceInfo")}
                    onRetry={refetch}
                    retrying={isFetching}
                  />
                )}
                <InfoList data={view} />
              </>
            )}
          </Panel>
        </motion.div>
        <motion.div variants={STAGGER_ITEM}>
          <FooterBar />
        </motion.div>
      </motion.div>
    </>
  );
}
