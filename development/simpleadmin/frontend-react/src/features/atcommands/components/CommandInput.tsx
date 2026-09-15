// 命令输入行(设计方案 §6.10):mono Input + 发送按钮;↑/↓ 历史召回(含草稿恢复,
// 召回逻辑在 hooks/lib);Enter 发送;空输入不发;分号组合命令原样透传(不改写);
// in-flight 期间输入与按钮禁用(atData mutation 禁用纪律)。
import { LoaderCircleIcon, SendIcon } from "lucide-react";
import type { KeyboardEvent } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useT } from "@/lib/i18n";

export interface CommandInputProps {
  command: string;
  onChange: (value: string) => void;
  onSend: () => void;
  onPrev: () => void;
  onNext: () => void;
  disabled: boolean;
}

export function CommandInput({
  command,
  onChange,
  onSend,
  onPrev,
  onNext,
  disabled,
}: CommandInputProps) {
  const { t } = useT("atcommands");

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") {
      event.preventDefault();
      if (!disabled) onSend();
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      onPrev();
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      onNext();
    }
  };

  return (
    <div className="flex flex-col gap-1.5">
      <form
        className="flex items-center gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          if (!disabled) onSend();
        }}
      >
        <Input
          aria-label={t("atCommandInputAria")}
          className="font-mono"
          value={command}
          placeholder="ATI"
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={onKeyDown}
        />
        <Button type="submit" disabled={disabled || command.trim() === ""}>
          {disabled ? <LoaderCircleIcon className="animate-spin" /> : <SendIcon />}
          {t("send")}
        </Button>
      </form>
      <p className="text-xs text-muted">{t("useTheKeysToBrowseCommandHistory")}</p>
    </div>
  );
}
