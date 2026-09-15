// Tab3 端口转发 DNAT(新):fwd_list 读取 → 暂存编辑(新增/启停 Switch/删除)→ fwd_save 原子应用 + confirm。
// 校验矩阵对齐后端 parseFirewallFwdRules:端口 1-65535、合法 IPv4、proto tcp|udp、
// (extPort,proto) 唯一、总数 ≤32;顶部危险语义提示卡(暴露内网服务)。
import {
  ArrowRightLeftIcon,
  LoaderCircleIcon,
  PlusIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { EmptyState, ErrorRetry } from "@/components/common";
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
import { Switch } from "@/components/ui/switch";
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

import { useFwdList, useSaveFwdRules } from "../hooks";
import {
  MAX_FWD_RULES,
  apiErrorMessage,
  computeFwdDiff,
  fwdKey,
  normalizeFwdRules,
  toFwdRule,
  validateAddFwd,
} from "../lib";
import type { AddFwdError, FwdRule } from "../lib";
import { DirtyBar } from "./DirtyBar";
import { StaggerGroup, StaggerItem } from "./Stagger";

const EMPTY_FORM = { extPort: "", intIP: "", intPort: "", proto: "tcp" };

export function ForwardTab() {
  const { t } = useT("firewall");
  const confirm = useConfirmStore((state) => state.confirm);
  const fwdQuery = useFwdList();
  const saveFwd = useSaveFwdRules();

  const serverRules = useMemo(() => normalizeFwdRules(fwdQuery.data?.forwarded), [fwdQuery.data]);
  const [draft, setDraft] = useState<FwdRule[] | null>(null);
  const rules = draft ?? serverRules;
  const diff = useMemo(() => computeFwdDiff(serverRules, rules), [serverRules, rules]);
  const changeCount = diff.added.length + diff.removed.length + diff.changed.length;
  const dirty = draft !== null && changeCount > 0;

  const [form, setForm] = useState({ ...EMPTY_FORM });
  const [addError, setAddError] = useState<AddFwdError | null>(null);

  function addErrorMessage(code: AddFwdError): string {
    switch (code) {
      case "invalidExtPort":
      case "invalidIntPort":
        return t("portMustBeANumberBetween1And65535");
      case "invalidIp":
        return t("invalidInternalIp");
      case "invalidProto":
        return t("common:operationFailed");
      case "duplicate":
        return t("forwardDuplicate");
      case "limit":
        return t("forwardLimitReached", { max: MAX_FWD_RULES });
    }
  }

  function handleAdd(): void {
    const error = validateAddFwd(rules, form);
    setAddError(error);
    if (error) {
      toast.error(addErrorMessage(error));
      return;
    }
    setDraft([...rules, toFwdRule(form)]);
    setForm({ ...EMPTY_FORM });
  }

  function toggleEnabled(rule: FwdRule, enabled: boolean): void {
    setDraft(rules.map((item) => (fwdKey(item) === fwdKey(rule) ? { ...item, enabled } : item)));
  }

  async function handleDelete(rule: FwdRule): Promise<void> {
    const ok = await confirm({
      title: t("common:delete"),
      message: t("deleteForwardRule"),
      confirmText: t("common:delete"),
      danger: true,
    });
    if (!ok) return;
    setDraft(rules.filter((item) => fwdKey(item) !== fwdKey(rule)));
  }

  async function handleApply(): Promise<void> {
    const ok = await confirm({
      title: t("forwardApplyConfirmTitle"),
      message: t("forwardApplyConfirmMessage", {
        added: diff.added.length,
        removed: diff.removed.length,
        changed: diff.changed.length,
      }),
      confirmText: t("applyChanges"),
      danger: true,
    });
    if (!ok) return;
    saveFwd.mutate(rules, {
      onSuccess: () => {
        toast.success(t("applied"));
        setDraft(null);
      },
      onError: (error) => {
        toast.error(apiErrorMessage(error, t("failedToApply")));
      },
    });
  }

  return (
    <StaggerGroup>
      <StaggerItem>
        <div
          role="note"
          className="flex items-start gap-2 rounded-md border border-danger/30 bg-danger-soft p-3 text-sm text-danger"
        >
          <TriangleAlertIcon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
          <span>{t("forwardDangerNote")}</span>
        </div>
      </StaggerItem>
      <StaggerItem>
        <Panel domain="security" icon={ArrowRightLeftIcon} title={t("forwardRules")}>
          {fwdQuery.isPending ? (
            <Skeleton className="h-40 w-full" aria-busy="true" />
          ) : fwdQuery.isError ? (
            <ErrorRetry
              message={apiErrorMessage(fwdQuery.error, t("common:failedToLoadStatus"))}
              onRetry={() => void fwdQuery.refetch()}
              retrying={fwdQuery.isFetching}
            />
          ) : rules.length === 0 ? (
            <EmptyState title={t("forwardEmpty")} />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("extPort")}</TableHead>
                  <TableHead>{t("protocol")}</TableHead>
                  <TableHead>{t("forwardTarget")}</TableHead>
                  <TableHead>{t("common:enable")}</TableHead>
                  <TableHead className="text-right">{t("common:actions")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((rule) => (
                  <TableRow key={fwdKey(rule)}>
                    <TableCell className="font-mono tabular-nums">{rule.extPort}</TableCell>
                    <TableCell>
                      <Badge variant={rule.proto === "tcp" ? "info" : "muted"}>
                        {rule.proto.toUpperCase()}
                      </Badge>
                    </TableCell>
                    <TableCell className="font-mono">{`${rule.intIP}:${rule.intPort}`}</TableCell>
                    <TableCell>
                      <Switch
                        aria-label={`${t("common:enable")} ${rule.proto}/${rule.extPort}`}
                        checked={rule.enabled}
                        onCheckedChange={(next) => toggleEnabled(rule, next)}
                      />
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="danger"
                        size="sm"
                        aria-label={`${t("common:delete")} ${rule.extPort}`}
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
              className="w-32"
              inputMode="numeric"
              aria-label={t("extPort")}
              placeholder={t("extPort")}
              value={form.extPort}
              invalid={addError === "invalidExtPort"}
              onChange={(event) => {
                setForm({ ...form, extPort: event.target.value });
                setAddError(null);
              }}
            />
            <Input
              className="w-40"
              aria-label={t("intIp")}
              placeholder={t("intIp")}
              value={form.intIP}
              invalid={addError === "invalidIp"}
              onChange={(event) => {
                setForm({ ...form, intIP: event.target.value });
                setAddError(null);
              }}
            />
            <Input
              className="w-32"
              inputMode="numeric"
              aria-label={t("intPort")}
              placeholder={t("intPort")}
              value={form.intPort}
              invalid={addError === "invalidIntPort"}
              onChange={(event) => {
                setForm({ ...form, intPort: event.target.value });
                setAddError(null);
              }}
            />
            <Select
              value={form.proto}
              onValueChange={(value) => setForm({ ...form, proto: value })}
            >
              <SelectTrigger aria-label={t("protocol")} className="w-24">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="tcp">TCP</SelectItem>
                <SelectItem value="udp">UDP</SelectItem>
              </SelectContent>
            </Select>
            <Button onClick={handleAdd} disabled={saveFwd.isPending}>
              <PlusIcon />
              {t("addForwardRule")}
            </Button>
          </div>
          <p
            aria-live="polite"
            className={cn("mt-2 min-h-5 text-sm", addError ? "text-danger" : "text-muted")}
          >
            {addError ? addErrorMessage(addError) : ""}
          </p>
          {saveFwd.isPending && (
            <p className="flex items-center gap-2 text-sm text-muted">
              <LoaderCircleIcon className="animate-spin" aria-hidden="true" />
              {t("common:loading")}
            </p>
          )}
        </Panel>
      </StaggerItem>
      <DirtyBar
        changes={dirty ? changeCount : 0}
        applying={saveFwd.isPending}
        onApply={() => void handleApply()}
        onDiscard={() => setDraft(null)}
      />
    </StaggerGroup>
  );
}
