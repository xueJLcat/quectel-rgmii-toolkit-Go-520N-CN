// 防火墙页(设计方案 §6.8,域色 security/amber):Tabs 五页签——
// IPv4 端口规则(暂存编辑)/ IPv6 链(只读·新)/ 端口转发 DNAT(新)/ DMZ / 全部链。
import {
  ArrowRightLeftIcon,
  GlobeIcon,
  LayersIcon,
  ShieldAlertIcon,
  ShieldBanIcon,
} from "lucide-react";

import { PageHeader } from "@/components/layout/page-header";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useT } from "@/lib/i18n";

import { AllChainsTab } from "./components/AllChainsTab";
import { DmzTab } from "./components/DmzTab";
import { ForwardTab } from "./components/ForwardTab";
import { Ipv6Tab } from "./components/Ipv6Tab";
import { PortRulesTab } from "./components/PortRulesTab";

export default function FirewallPage() {
  const { t } = useT("firewall");
  const { t: tn } = useT("nav");
  return (
    <>
      <PageHeader
        domain="security"
        title={tn("firewall")}
        description={t("firewallPageDescription")}
      />
      <Tabs defaultValue="rules" className="gap-4">
        <TabsList className="h-auto w-full flex-wrap justify-start">
          <TabsTrigger value="rules">
            <ShieldBanIcon />
            {t("tabPortRules")}
          </TabsTrigger>
          <TabsTrigger value="ipv6">
            <GlobeIcon />
            {t("tabIpv6Chains")}
          </TabsTrigger>
          <TabsTrigger value="forward">
            <ArrowRightLeftIcon />
            {t("tabPortForward")}
          </TabsTrigger>
          <TabsTrigger value="dmz">
            <ShieldAlertIcon />
            {t("tabDmz")}
          </TabsTrigger>
          <TabsTrigger value="chains">
            <LayersIcon />
            {t("tabAllChains")}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="rules">
          <PortRulesTab />
        </TabsContent>
        <TabsContent value="ipv6">
          <Ipv6Tab />
        </TabsContent>
        <TabsContent value="forward">
          <ForwardTab />
        </TabsContent>
        <TabsContent value="dmz">
          <DmzTab />
        </TabsContent>
        <TabsContent value="chains">
          <AllChainsTab />
        </TabsContent>
      </Tabs>
    </>
  );
}
