// Tab1 IPv4 端口规则:暂存编辑器(行为对齐旧 firewall.js)+ 防火墙状态卡 + 脏状态 sticky 提示条。
// 暂存语义:draft=null 表示"跟随服务端";任何编辑把当前生效规则拷贝进 draft——服务端刷新
// (invalidate/refetch)绝不冲掉未应用的暂存;放弃=draft 归 null;应用成功后同样归 null 跟随新服务端状态。
// 应用 = confirm(diff 摘要:新增 N/删除 N)→ save(block_ports, accept_ports) 原子提交。
import { PlusIcon, ShieldBanIcon, Trash2Icon } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { EmptyState, ErrorRetry, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { useConfirmStore } from "@/stores/confirm";

import { useFirewallStatus, useSavePortRules } from "../hooks";
import {
  MAX_PORT_RULES,
  apiErrorMessage,
  computeRuleDiff,
  normalizeRules,
  ruleKey,
  rulesEqual,
  sortRulesByPort,
  validateAddRule,
} from "../lib";
import type { AddRuleError, PortRule, RuleAction } from "../lib";
import { DirtyBar } from "./DirtyBar";
import { StaggerGroup, StaggerItem } from "./Stagger";

export function PortRulesTab() {
  const { t } = useT("firewall");
  const confirm = useConfirmStore((state) => state.confirm);
  const statusQuery = useFirewallStatus();
  const saveRules = useSavePortRules();

  const serverRules = useMemo(() => normalizeRules(statusQuery.data?.rules), [statusQuery.data]);
  const [draft, setDraft] = useState<PortRule[] | null>(null);
  const rules = draft ?? serverRules;
  const sortedRules = useMemo(() => sortRulesByPort(rules), [rules]);
  const diff = useMemo(() => computeRuleDiff(serverRules, rules), [serverRules, rules]);
  const dirty = draft !== null && !rulesEqual(draft, serverRules);
  const changeCount = diff.added.length + diff.removed.length;

  const [newPort, setNewPort] = useState("");
  const [newAction, setNewAction] = useState<RuleAction>("block");
  const [addError, setAddError] = useState<AddRuleError | null>(null);

  function addErrorMessage(code: AddRuleError): string {
    switch (code) {
      case "invalidPort":
        return t("portMustBeANumberBetween1And65535");
      case "duplicate":
        return t("thisPortIsAlreadyInTheList");
      case "conflict":
        return t("portConflict");
      case "limit":
        return t("rulesLimitReached", { max: MAX_PORT_RULES });
    }
  }

  function handleAdd(): void {
    const error = validateAddRule(rules, newPort, newAction);
    setAddError(error);
    if (error) {
      toast.error(addErrorMessage(error));
      return;
    }
    setDraft([...rules, { port: String(Number(newPort.trim())), action: newAction }]);
    setNewPort("");
  }

  async function handleDelete(rule: PortRule): Promise<void> {
    const ok = await confirm({
      title: t("common:delete"),
      message: t("deleteThisRule"),
      confirmText: t("common:delete"),
      danger: true,
    });
    if (!ok) return;
    setDraft(rules.filter((item) => ruleKey(item) !== ruleKey(rule)));
  }

  async function handleClear(): Promise<void> {
    if (rules.length === 0) return;
    const ok = await confirm({
      title: t("common:clearAll"),
      message: t("clearAllRules"),
      confirmText: t("common:clearAll"),
      danger: true,
    });
    if (ok) setDraft([]);
  }

  async function handleApply(): Promise<void> {
    const ok = await confirm({
      title: t("applyConfirmTitle"),
      message: t("applyConfirmMessage", {
        added: diff.added.length,
        removed: diff.removed.length,
      }),
      confirmText: t("applyChanges"),
      danger: true,
    });
    if (!ok) return;
    saveRules.mutate(rules, {
      onSuccess: () => {
        toast.success(t("applied"));
        setDraft(null);
      },
      onError: (error) => {
        toast.error(apiErrorMessage(error, t("failedToApply")));
      },
    });
  }

  const data = statusQuery.data;

  return (
    <StaggerGroup>
      <StaggerItem>
        <Panel
          domain="security"
          icon={ShieldBanIcon}
          title={t("firewallStatus")}
          footer={t(
            "allowRulesApplyToAllInterfacesBlockRulesAllowTrafficOnTheBridge0Eth0AndTailscale0InterfacesAndMatchingInboundConnectionsOnAllOtherInterfacesAreBlocked",
          )}
        >
          {statusQuery.isPending ? (
            <div className="flex gap-2" aria-busy="true">
              <Skeleton className="h-6 w-28" />
              <Skeleton className="h-6 w-36" />
            </div>
          ) : statusQuery.isError ? (
            <ErrorRetry
              message={t("failedToLoadFirewallStatus")}
              onRetry={() => void statusQuery.refetch()}
              retrying={statusQuery.isFetching}
            />
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <StatusChip tone={data?.jumpInstalled ? "success" : "muted"}>
                {`${t("common:status")}: ${
                  data?.jumpInstalled ? t("common:enabled") : t("common:notEnabled")
                }`}
              </StatusChip>
              <StatusChip tone="info">{`${t("managedRules")}: ${data?.ruleCount ?? 0}`}</StatusChip>
            </div>
          )}
        </Panel>
      </StaggerItem>
      <StaggerItem>
        <Panel
          domain="security"
          icon={ShieldBanIcon}
          title={t("portRules")}
          tools={
            rules.length > 0 ? (
              <Button variant="danger" size="sm" onClick={() => void handleClear()}>
                {t("common:clearAll")}
              </Button>
            ) : undefined
          }
          footer={t(
            "changesTakeEffectImmediatelyAndPersistAcrossRebootsTheSamePortCannotBeBothBlockedAndAllowed",
          )}
        >
          {statusQuery.isPending ? (
            <Skeleton className="h-40 w-full" aria-busy="true" />
          ) : sortedRules.length === 0 ? (
            <EmptyState title={t("noPortRulesConfigured")} />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("port")}</TableHead>
                  <TableHead>{t("type")}</TableHead>
                  <TableHead className="text-right">{t("common:actions")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedRules.map((rule) => (
                  <TableRow key={ruleKey(rule)}>
                    <TableCell className="font-mono tabular-nums">{rule.port}</TableCell>
                    <TableCell>
                      <Badge variant={rule.action === "accept" ? "success" : "danger"}>
                        {rule.action === "accept" ? t("allow") : t("block")}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="danger"
                        size="sm"
                        aria-label={`${t("common:delete")} ${rule.port}`}
                        onClick={() => void handleDelete(rule)}
                      >
                        <Trash2Icon />
                        {t("common:delete")}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          <div className="mt-4 flex flex-wrap items-center gap-2">
            <Input
              className="w-36"
              inputMode="numeric"
              aria-label={t("port")}
              placeholder={t("eG8080")}
              value={newPort}
              invalid={addError !== null}
              disabled={saveRules.isPending}
              onChange={(event) => {
                setNewPort(event.target.value);
                setAddError(null);
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  handleAdd();
                }
              }}
            />
            <Select
              value={newAction}
              onValueChange={(value) => setNewAction(value === "accept" ? "accept" : "block")}
            >
              <SelectTrigger aria-label={t("ruleType")} className="w-28">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="block">{t("block")}</SelectItem>
                <SelectItem value="accept">{t("allow")}</SelectItem>
              </SelectContent>
            </Select>
            <Button onClick={handleAdd} disabled={saveRules.isPending}>
              <PlusIcon />
              {t("common:add")}
            </Button>
          </div>
          <p
            aria-live="polite"
            className={cn("mt-2 min-h-5 text-sm", addError ? "text-danger" : "text-muted")}
          >
            {addError ? addErrorMessage(addError) : ""}
          </p>
        </Panel>
      </StaggerItem>
      <DirtyBar
        changes={dirty ? changeCount : 0}
        applying={saveRules.isPending}
        onApply={() => void handleApply()}
        onDiscard={() => setDraft(null)}
      />
    </StaggerGroup>
  );
}
