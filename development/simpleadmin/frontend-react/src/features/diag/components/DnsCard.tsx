// DNS 查询卡:域名 + 可选服务器(留空 = 系统解析器)→ dns_query;
// 结果 A/AAAA 地址列表(mono + CopyButton)+ 解析耗时;运行时失败展示为失败条目(非页面级错误)。
import { LoaderCircleIcon, SearchIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { CopyButton, StatusChip } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/lib/i18n";

import { useDnsQuery } from "../hooks";
import { isValidDnsServer, isValidDomain } from "../lib";

type DnsOutcome =
  | { state: "running"; domain: string }
  | {
      state: "done";
      ok: true;
      domain: string;
      addresses: string[];
      latencyMs?: number;
      server?: string;
    }
  | { state: "done"; ok: false; domain: string; error: string; latencyMs?: number };

export function DnsCard() {
  const { t } = useT("diag");
  const dnsQuery = useDnsQuery();
  const [domain, setDomain] = useState("");
  const [server, setServer] = useState("");
  const [outcome, setOutcome] = useState<DnsOutcome | null>(null);
  const busy = outcome?.state === "running";

  async function handleQuery(): Promise<void> {
    const domainValue = domain.trim();
    if (!isValidDomain(domainValue)) {
      toast.error(t("invalidDomain"));
      return;
    }
    const serverValue = server.trim();
    if (!isValidDnsServer(serverValue)) {
      toast.error(t("invalidServer"));
      return;
    }
    setOutcome({ state: "running", domain: domainValue });
    try {
      const data = await dnsQuery.mutateAsync({
        domain: domainValue,
        server: serverValue || undefined,
      });
      if (data?.ok === false) {
        setOutcome({
          state: "done",
          ok: false,
          domain: domainValue,
          error: String(data.error ?? ""),
          latencyMs: data.latencyMs,
        });
        return;
      }
      setOutcome({
        state: "done",
        ok: true,
        domain: domainValue,
        addresses: Array.isArray(data?.addresses) ? data.addresses : [],
        latencyMs: data?.latencyMs,
        server: typeof data?.server === "string" ? data.server : "",
      });
    } catch (err) {
      const message = err instanceof Error && err.message ? err.message : t("common:unknownError");
      setOutcome({ state: "done", ok: false, domain: domainValue, error: message });
    }
  }

  return (
    <Panel domain="tools" icon={SearchIcon} title={t("dnsQuery")} footer={t("dnsQueryDescription")}>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="diag-domain">{t("domainLabel")}</Label>
          <Input
            id="diag-domain"
            type="text"
            placeholder={t("domainPlaceholder")}
            aria-label={t("domainLabel")}
            value={domain}
            disabled={busy}
            onChange={(event) => setDomain(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                void handleQuery();
              }
            }}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="diag-server">{t("serverLabel")}</Label>
          <Input
            id="diag-server"
            type="text"
            placeholder={t("serverPlaceholder")}
            aria-label={t("serverLabel")}
            value={server}
            disabled={busy}
            onChange={(event) => setServer(event.target.value)}
          />
        </div>
        <div>
          <Button onClick={() => void handleQuery()} disabled={busy}>
            {busy && <LoaderCircleIcon className="animate-spin" />}
            {busy ? t("querying") : t("query")}
          </Button>
        </div>
        <div className="flex flex-col gap-2" data-slot="dns-result">
          <Label>{t("dnsResults")}</Label>
          {outcome === null ? (
            <p className="text-sm text-muted">{t("noDnsResults")}</p>
          ) : outcome.state === "running" ? (
            <p className="flex items-center gap-2 text-sm text-muted">
              <LoaderCircleIcon className="size-4 animate-spin" aria-hidden="true" />
              {t("querying")}
            </p>
          ) : outcome.ok ? (
            <div className="flex flex-col gap-2">
              <div className="flex flex-wrap items-center gap-2">
                <StatusChip tone="success">
                  {typeof outcome.latencyMs === "number"
                    ? t("dnsLatency", { ms: outcome.latencyMs })
                    : t("reachable")}
                </StatusChip>
                <span className="text-xs text-muted">
                  {t("dnsServerUsed", { server: outcome.server || t("systemResolver") })}
                </span>
              </div>
              {outcome.addresses.length === 0 ? (
                <p className="text-sm text-muted">{t("noDnsResults")}</p>
              ) : (
                <ul className="flex flex-col gap-1.5">
                  {outcome.addresses.map((address) => (
                    <li key={address} className="flex items-center gap-1">
                      <span className="font-mono text-sm text-ink">{address}</span>
                      <CopyButton text={address} />
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ) : (
            <div className="flex flex-col gap-1">
              <StatusChip tone="danger">{t("unreachable")}</StatusChip>
              <p className="text-xs text-danger">
                {t("dnsFailed", { error: outcome.error || t("common:unknownError") })}
              </p>
            </div>
          )}
        </div>
      </div>
    </Panel>
  );
}
