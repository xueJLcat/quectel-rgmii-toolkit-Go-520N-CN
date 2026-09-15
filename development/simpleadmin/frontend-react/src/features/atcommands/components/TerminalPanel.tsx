// 终端输出面板(设计方案 §6.10):恒深色——终端输出按惯例保持深色底以保证
// "> 命令 / ERROR / OK" 三色文本在亮色主题下同样可读,色值直写 tokens.css
// 暗色主题令牌值(bg #0d1320 / soft #d8dee9 / tools #22d3ee / system #fb7185 /
// network #34d399),不随 data-bs-theme 切换;等宽字体,200 行截断在 lib 完成,
// 复制/清空角标按钮,内容更新自动滚底。
import { EraserIcon, TerminalIcon } from "lucide-react";
import { useEffect, useRef } from "react";

import { CopyButton } from "@/components/common";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

import { classifyTranscriptLine } from "../lib";

// 恒深色令牌(见文件头注释,勿替换为主题自适应类)
const TERM_BG = "#0d1320";
const TERM_FG = "#d8dee9";
const TERM_MUTED = "#5c6a85";

const LINE_CLASS: Record<ReturnType<typeof classifyTranscriptLine>, string> = {
  command: "text-[#22d3ee]",
  error: "text-[#fb7185]",
  ok: "text-[#34d399]",
  plain: "",
};

export interface TerminalPanelProps {
  transcript: string;
  isClean: boolean;
  onClear: () => void;
}

export function TerminalPanel({ transcript, isClean, onClear }: TerminalPanelProps) {
  const { t } = useT("atcommands");
  const bodyRef = useRef<HTMLDivElement>(null);

  // 内容更新滚底(终端语义:最新输出可见)
  useEffect(() => {
    const body = bodyRef.current;
    if (body) body.scrollTop = body.scrollHeight;
  }, [transcript]);

  const lines = transcript.split("\n");
  return (
    <section
      data-slot="at-terminal"
      aria-label={t("atCommandOutput")}
      className="flex h-full flex-col overflow-hidden rounded-md border border-line"
      style={{ backgroundColor: TERM_BG }}
    >
      <header className="flex items-center gap-2 border-b border-white/10 px-4 py-2.5">
        <span
          aria-hidden="true"
          className="grid size-7 shrink-0 place-items-center rounded-sm bg-[image:var(--sa-grad-tools)] text-white"
        >
          <TerminalIcon className="size-3.5" />
        </span>
        <h3 className="text-base font-semibold" style={{ color: TERM_FG }}>
          {t("atCommandOutput")}
        </h3>
        <div className="ml-auto flex items-center gap-1">
          <CopyButton
            text={transcript}
            className="text-[#d8dee9] hover:bg-white/10 hover:text-white"
          />
          <Button
            variant="ghost"
            size="icon"
            onClick={onClear}
            disabled={isClean}
            aria-label={t("clearOutput")}
            className="text-[#d8dee9] hover:bg-white/10 hover:text-white disabled:opacity-40"
          >
            <EraserIcon />
          </Button>
        </div>
      </header>
      <div
        ref={bodyRef}
        className="min-h-72 flex-1 overflow-y-auto p-4 font-mono text-sm leading-6"
        style={{ color: TERM_FG }}
      >
        {isClean ? (
          <p className="text-sm" style={{ color: TERM_MUTED }}>
            {t("terminalEmptyHint")}
          </p>
        ) : (
          lines.map((line, index) => (
            // transcript 为纯文本逐行渲染,无 HTML 注入面;行序即 key
            <div
              key={index}
              className={cn(
                "break-words whitespace-pre-wrap",
                LINE_CLASS[classifyTranscriptLine(line)],
              )}
            >
              {line === "" ? "\u00A0" : line}
            </div>
          ))
        )}
      </div>
    </section>
  );
}
