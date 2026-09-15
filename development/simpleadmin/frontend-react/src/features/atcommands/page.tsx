// AT 命令页(域色 tools,设计方案 §6.10):恒深色终端输出面板 + mono 输入行(↑/↓ 历史召回)+
// 快捷命令 chip 网格(7 预设)+ 命令历史侧卡 + 危险区(AT&F / 重启模块)。
// 面板进场 stagger 40ms/项(§5.1);atData in-flight 期间输入/快捷/危险按钮全部禁用。
import { motion } from "motion/react";
import type { ReactNode } from "react";

import { PageHeader } from "@/components/layout/page-header";
import { Panel } from "@/components/layout/panel";
import { useT } from "@/lib/i18n";

import { AtDangerZone } from "./components/AtDangerZone";
import { CommandInput } from "./components/CommandInput";
import { HistoryPanel } from "./components/HistoryPanel";
import { QuickCommands } from "./components/QuickCommands";
import { TerminalPanel } from "./components/TerminalPanel";
import { useAtSession } from "./hooks";

const STAGGER_STEP_S = 0.04;

function StaggerItem({
  index,
  className,
  children,
}: {
  index: number;
  className?: string;
  children: ReactNode;
}) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.22, ease: [0.22, 0.7, 0.28, 1], delay: index * STAGGER_STEP_S }}
      className={className}
    >
      {children}
    </motion.div>
  );
}

export default function AtcommandsPage() {
  const { t } = useT("atcommands");
  const session = useAtSession();
  const busy = session.sending || session.resetting;

  return (
    <>
      <PageHeader domain="tools" title={t("nav:atCommands")} description={t("pageDescription")} />
      <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <div className="flex flex-col gap-4">
          <StaggerItem index={0}>
            <TerminalPanel
              transcript={session.transcript}
              isClean={session.isClean}
              onClear={session.clearTranscript}
            />
          </StaggerItem>
          <StaggerItem index={1}>
            <Panel
              domain="tools"
              title={t("atTerminal")}
              footer={t(
                "separateMultipleCommandsWithSemicolonsTheModuleProcessesThemAsOneCompoundCommandExampleAtCfunCcid",
              )}
            >
              <CommandInput
                command={session.command}
                onChange={session.setCommand}
                onSend={() => void session.send()}
                onPrev={session.historyPrev}
                onNext={session.historyNext}
                disabled={busy}
              />
            </Panel>
          </StaggerItem>
        </div>
        <div className="flex flex-col gap-4">
          <StaggerItem index={2}>
            {/* 快捷命令:回填输入行并立即发送(对齐旧版 runQuickCommand) */}
            <QuickCommands
              onRun={(cmd) => {
                session.setCommand(cmd);
                void session.sendCommand(cmd);
              }}
              disabled={busy}
            />
          </StaggerItem>
          <StaggerItem index={3}>
            <HistoryPanel
              history={session.history}
              onReuse={session.reuseHistory}
              onRemove={session.removeHistory}
              onClear={session.clearHistory}
            />
          </StaggerItem>
          <StaggerItem index={4}>
            <AtDangerZone
              disabled={busy}
              onResetAt={() => void session.resetAt()}
              onReboot={() => void session.rebootModule()}
            />
          </StaggerItem>
        </div>
      </div>
    </>
  );
}
