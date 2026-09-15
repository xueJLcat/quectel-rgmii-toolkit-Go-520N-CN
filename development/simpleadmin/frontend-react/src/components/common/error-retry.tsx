// 错误重试(设计方案 §3.3 三件套 / §5.2:错误态必须带"重试"按钮,继承现有失败重试入口模式):
// danger-soft 底卡 + 警示图标 + 错误文案 + 重试按钮;retrying 时按钮内联 spinner 并禁用
// (对齐按钮四级规范的"提交中内联 spinner + 禁用")。message 由调用方经 i18n 传入。
import { LoaderCircleIcon, RefreshCwIcon, TriangleAlertIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

export interface ErrorRetryProps {
  message: string;
  onRetry: () => void;
  /** 重试请求进行中:spinner + 禁用,防连点 */
  retrying?: boolean;
  className?: string;
}

export function ErrorRetry({ message, onRetry, retrying = false, className }: ErrorRetryProps) {
  const { t } = useT();
  return (
    <div
      role="alert"
      className={cn(
        "flex flex-col items-start gap-3 rounded-md border border-danger/30 bg-danger-soft p-4 sm:flex-row sm:items-center sm:justify-between",
        className,
      )}
    >
      <div className="flex items-start gap-2 text-sm text-danger">
        <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
        <span className="break-words">{message}</span>
      </div>
      <Button variant="danger" size="sm" onClick={onRetry} disabled={retrying}>
        {retrying ? <LoaderCircleIcon className="animate-spin" /> : <RefreshCwIcon />}
        {t("retry")}
      </Button>
    </div>
  );
}
