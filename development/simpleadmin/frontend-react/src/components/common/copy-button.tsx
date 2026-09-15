// CopyButton(设计方案 §5.2 复制微交互):IP/IMEI/ICCID 等 mono 字段旁的一键复制。
// navigator.clipboard 优先,拒绝/不可用(非安全上下文、旧浏览器)时降级 textarea + execCommand;
// 成功 → 图标变对勾 1.2s + sonner toast,失败 → danger toast(上限/时长由壳层 Toaster 统一配置)。
import { CheckIcon, CopyIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

const CHECK_RESET_MS = 1200;

async function writeClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // clipboard API 拒绝(权限/上下文)时落入传统路径
  }
  try {
    const area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.top = "-1000px";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand("copy");
    area.remove();
    return ok;
  } catch {
    return false;
  }
}

export interface CopyButtonProps {
  text: string;
  /** 可见文案;缺省仅图标(aria-label 走 i18n copy 词条) */
  label?: string;
  className?: string;
}

export function CopyButton({ text, label, className }: CopyButtonProps) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timerRef.current !== null) clearTimeout(timerRef.current);
    },
    [],
  );

  const onCopy = async () => {
    const ok = await writeClipboard(text);
    if (!ok) {
      toast.error(t("copyFailed"));
      return;
    }
    toast.success(t("copied"));
    setCopied(true);
    if (timerRef.current !== null) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      setCopied(false);
    }, CHECK_RESET_MS);
  };

  return (
    <Button
      type="button"
      variant="ghost"
      size={label ? "sm" : "icon"}
      aria-label={label ? undefined : t("copy")}
      className={cn("text-muted hover:text-ink", className)}
      onClick={() => void onCopy()}
    >
      {copied ? <CheckIcon className="text-success" /> : <CopyIcon />}
      {label && <span>{label}</span>}
    </Button>
  );
}
