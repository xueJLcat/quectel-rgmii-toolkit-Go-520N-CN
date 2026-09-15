// DHCP 静态绑定卡(设计方案 §6.7⑦):mac_bind_list/set/del,≤10 条,MAC 宽松格式校验
// (连字符/紧凑式,后端归一,见 lib.validateMacBind),增删即时生效;删除走 danger confirm。
import { LinkIcon, LoaderCircleIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import type { KeyboardEvent } from "react";
import { toast } from "sonner";

import { EmptyState, ErrorRetry } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { MacBinding } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import type { useMacBindMutations, useMacBindQuery } from "../hooks";
import { validateMacBind } from "../lib";

export interface MacBindCardProps {
  query: ReturnType<typeof useMacBindQuery>;
  actions: ReturnType<typeof useMacBindMutations>;
}

export function MacBindCard({ query, actions }: MacBindCardProps) {
  const { t } = useT("netconfig");
  const confirm = useConfirmStore((state) => state.confirm);
  const [mac, setMac] = useState("");
  const [ip, setIp] = useState("");
  const bindings = query.data ?? [];
  const busy = actions.add.isPending || actions.remove.isPending;

  const onAdd = () => {
    const result = validateMacBind(mac, ip, bindings);
    if (!result.valid) {
      toast.error(t(result.errorKey));
      return;
    }
    actions.add.mutate(
      { mac: result.mac, ip: result.ip },
      {
        onSuccess: () => {
          setMac("");
          setIp("");
        },
      },
    );
  };

  const onRemove = async (entry: MacBinding) => {
    const ok = await confirm({
      title: t("deleteStaticBinding"),
      message: t("theDeviceWillRevertToADynamicallyAssignedIpAfterDeletion"),
      confirmText: t("common:delete"),
      danger: true,
    });
    if (ok) actions.remove.mutate(String(entry.mac ?? ""));
  };

  const onAddKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") {
      event.preventDefault();
      onAdd();
    }
  };

  return (
    <Panel
      domain="network"
      icon={LinkIcon}
      title={t("staticAddressBinding")}
      footer={t(
        "assignsAFixedIpToADeviceDhcpStaticLeaseTakesEffectImmediatelyAfterSavingPersistedInFirmwareAndRetainedAfterRebootUpTo10Entries",
      )}
    >
      <div className="flex flex-col gap-3">
        {query.isError ? (
          <ErrorRetry
            message={query.error?.message || t("common:failedToLoadStatus")}
            onRetry={() => void query.refetch()}
            retrying={query.isFetching}
          />
        ) : query.isPending && !query.data ? (
          <p className="flex items-center gap-2 text-sm text-muted" aria-busy="true">
            <LoaderCircleIcon className="size-4 animate-spin" />
            {t("common:loadingStatus")}
          </p>
        ) : bindings.length === 0 ? (
          <EmptyState title={t("noStaticBindingsConfigured")} className="py-6" />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("macAddress")}</TableHead>
                <TableHead>{t("ipAddress")}</TableHead>
                <TableHead>{t("common:actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {bindings.map((entry, index) => (
                <TableRow key={`${entry.mac ?? ""}-${index}`}>
                  <TableCell className="font-mono text-sm">{entry.mac || "-"}</TableCell>
                  <TableCell className="font-mono text-sm">{entry.ip || "-"}</TableCell>
                  <TableCell>
                    <Button
                      variant="danger"
                      size="sm"
                      disabled={busy}
                      onClick={() => void onRemove(entry)}
                    >
                      {actions.remove.isPending && <LoaderCircleIcon className="animate-spin" />}
                      {t("common:delete")}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <div className="flex flex-col gap-2 sm:flex-row">
          <Input
            value={mac}
            disabled={busy}
            onChange={(event) => setMac(event.target.value)}
            onKeyDown={onAddKeyDown}
            placeholder={t("macAddressEGAaBbCcDdEeFf")}
            aria-label={t("macAddress")}
            className="font-mono sm:max-w-64"
          />
          <Input
            value={ip}
            disabled={busy}
            onChange={(event) => setIp(event.target.value)}
            onKeyDown={onAddKeyDown}
            placeholder={t("ipAddressEG192168520")}
            aria-label={t("ipAddress")}
            className="font-mono sm:max-w-56"
          />
          <Button className="w-fit" disabled={busy} onClick={onAdd}>
            {actions.add.isPending ? <LoaderCircleIcon className="animate-spin" /> : <PlusIcon />}
            {t("common:add")}
          </Button>
        </div>
      </div>
    </Panel>
  );
}
