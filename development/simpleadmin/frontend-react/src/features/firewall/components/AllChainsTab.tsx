// Tab5 全部链:status 响应的 chains 折叠面板(命中计数/字节人性化/line-numbers),
// 与 Tab1 共享 ["firewall","status"] 查询缓存;工具区提供手动刷新入口(对齐旧版刷新按钮)。
import { LayersIcon, RefreshCwIcon } from "lucide-react";

import { EmptyState, ErrorRetry } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";

import { useFirewallStatus } from "../hooks";
import { ChainPanel } from "./ChainPanel";
import { StaggerGroup, StaggerItem } from "./Stagger";

export function AllChainsTab() {
  const { t } = useT("firewall");
  const statusQuery = useFirewallStatus();
  const chains = statusQuery.data?.chains ?? [];
  return (
    <StaggerGroup>
      <StaggerItem>
        <Panel
          domain="security"
          icon={LayersIcon}
          title={t("allFirewallRules")}
          tools={
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void statusQuery.refetch()}
              disabled={statusQuery.isFetching}
            >
              <RefreshCwIcon className={statusQuery.isFetching ? "animate-spin" : undefined} />
              {t("common:refresh")}
            </Button>
          }
        >
          {statusQuery.isPending ? (
            <div className="flex flex-col gap-3" aria-busy="true">
              <Skeleton className="h-11 w-full" />
              <Skeleton className="h-11 w-full" />
            </div>
          ) : statusQuery.isError ? (
            <ErrorRetry
              message={t("failedToLoadFirewallStatus")}
              onRetry={() => void statusQuery.refetch()}
              retrying={statusQuery.isFetching}
            />
          ) : chains.length === 0 ? (
            <EmptyState title={t("noRuleDataAvailable")} />
          ) : (
            <div className="flex flex-col gap-3">
              {chains.map((chain, index) => (
                <ChainPanel key={chain.name ?? index} chain={chain} />
              ))}
            </div>
          )}
        </Panel>
      </StaggerItem>
    </StaggerGroup>
  );
}
