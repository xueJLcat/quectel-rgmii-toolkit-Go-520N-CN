// Tab2 IPv6 链(新):status6 只读展示,链折叠面板 + "暂不支持 IPv6 规则编辑" 明示;
// 失败(ip6tables 缺失等)如实转 ErrorRetry + 说明,绝不把查询失败伪装成空链列表。
import { GlobeIcon } from "lucide-react";

import { EmptyState, ErrorRetry } from "@/components/common";
import { Panel } from "@/components/layout/panel";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/lib/i18n";

import { useFirewallStatus6 } from "../hooks";
import { apiErrorMessage } from "../lib";
import { ChainPanel } from "./ChainPanel";
import { StaggerGroup, StaggerItem } from "./Stagger";

export function Ipv6Tab() {
  const { t } = useT("firewall");
  const status6 = useFirewallStatus6();
  const chains = status6.data?.chains ?? [];
  const detail = apiErrorMessage(status6.error, "");
  return (
    <StaggerGroup>
      <StaggerItem>
        <Panel
          domain="security"
          icon={GlobeIcon}
          title={t("tabIpv6Chains")}
          footer={t("ipv6ReadOnlyNote")}
        >
          {status6.isPending ? (
            <div className="flex flex-col gap-3" aria-busy="true">
              <Skeleton className="h-11 w-full" />
              <Skeleton className="h-11 w-full" />
            </div>
          ) : status6.isError ? (
            <ErrorRetry
              message={detail ? `${t("ipv6LoadFailed")}: ${detail}` : t("ipv6LoadFailed")}
              onRetry={() => void status6.refetch()}
              retrying={status6.isFetching}
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
