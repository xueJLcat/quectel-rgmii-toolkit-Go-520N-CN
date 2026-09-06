// 链折叠面板(IPv6 链 / 全部链两个 Tab 共用):链名/策略/包数/字节人性化 + 逐链规则表(line-numbers)。
// 折叠 = 本地 state + aria-expanded,内容仅做 opacity 动效(§5.1 纪律:不动画布局属性)。
import { AnimatePresence, motion } from "motion/react";
import { ChevronDownIcon } from "lucide-react";
import { useId, useState } from "react";

import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { FirewallChainInfo } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

import { formatBytes, formatCount } from "../lib";

export interface ChainPanelProps {
  chain: FirewallChainInfo;
  defaultOpen?: boolean;
}

export function ChainPanel({ chain, defaultOpen = false }: ChainPanelProps) {
  const { t } = useT("firewall");
  const [open, setOpen] = useState(defaultOpen);
  const contentId = useId();
  const rules = chain.rules ?? [];
  return (
    <div className="overflow-hidden rounded-md border border-line bg-surface">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={contentId}
        onClick={() => setOpen((value) => !value)}
        className="flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors duration-[var(--sa-dur-fast)] ease-sa hover:bg-surface-2"
      >
        <ChevronDownIcon
          aria-hidden="true"
          className={cn(
            "size-4 shrink-0 text-muted transition-transform duration-[var(--sa-dur-fast)] ease-sa",
            !open && "-rotate-90",
          )}
        />
        <span className="truncate font-mono text-sm font-semibold text-ink">{chain.name}</span>
        {chain.policy && <Badge variant="muted">{`${t("defaultPolicy")}: ${chain.policy}`}</Badge>}
        <span className="ml-auto flex shrink-0 items-center gap-3 text-xs text-muted tabular-nums">
          <span>{`${t("packets")} ${formatCount(chain.pkts ?? 0)}`}</span>
          <span>{`${t("bytes")} ${formatBytes(chain.bytes ?? 0)}`}</span>
          <span>{t("chainRuleCount", { n: rules.length })}</span>
        </span>
      </button>
      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            id={contentId}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18 }}
            className="border-t border-line p-3"
          >
            {rules.length === 0 ? (
              <p className="text-sm text-muted">{t("noRules")}</p>
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>#</TableHead>
                      <TableHead>{t("packets")}</TableHead>
                      <TableHead>{t("bytes")}</TableHead>
                      <TableHead>{t("target")}</TableHead>
                      <TableHead>{t("protocol")}</TableHead>
                      <TableHead>{t("in")}</TableHead>
                      <TableHead>{t("out")}</TableHead>
                      <TableHead>{t("source")}</TableHead>
                      <TableHead>{t("destination")}</TableHead>
                      <TableHead>{t("details")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rules.map((rule, index) => (
                      <TableRow key={rule.num ?? index}>
                        <TableCell className="font-mono text-muted tabular-nums">
                          {rule.num ?? index + 1}
                        </TableCell>
                        <TableCell className="font-mono tabular-nums">
                          {formatCount(rule.pkts ?? 0)}
                        </TableCell>
                        <TableCell className="font-mono tabular-nums">
                          {formatBytes(rule.bytes ?? 0)}
                        </TableCell>
                        <TableCell>{rule.target}</TableCell>
                        <TableCell>{rule.proto}</TableCell>
                        <TableCell>{rule.in}</TableCell>
                        <TableCell>{rule.out}</TableCell>
                        <TableCell className="font-mono">{rule.source}</TableCell>
                        <TableCell className="font-mono">{rule.destination}</TableCell>
                        <TableCell className="font-mono text-muted">{rule.extra}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
