// 网络设置页(设计方案 §6.7,域色 network):①状态徽章行 ②IP 透传卡(禁用走后台重启流程 +
// ip_passthrough_result WS 事件收尾)③USB 网卡模式卡(切换 confirm + 可能重启倒计时)
// ④DNS 代理卡(v4 系统托管只读态/v6 开关即时生效)⑤上游 DNS 卡(≤4 行行内校验)
// ⑥LAN IP 卡(草稿保留 + 前端拦错)⑦DHCP 静态绑定表(≤10 条,增删即时生效)。
// 布局:[IP 透传|USB 协议] 两卡并排(网格),其余四卡各占整行
// [DNS 代理][上游 DNS][LAN IP][静态地址绑定],挂在 space-y-4 外层容器下。
// 写操作 mutation:in-flight 禁用、成功/失败 toast、成功后 invalidate status(hooks.ts)。
import { motion } from "motion/react";
import type { Transition } from "motion/react";
import type { ReactNode } from "react";

import { PageHeader } from "@/components/layout/page-header";
import { useT } from "@/lib/i18n";

import { DnsProxyCard } from "./components/DnsProxyCard";
import { DnsUpstreamCard } from "./components/DnsUpstreamCard";
import { IpPassthroughCard } from "./components/IpPassthroughCard";
import { LanIpCard } from "./components/LanIpCard";
import { MacBindCard } from "./components/MacBindCard";
import { StatusRow } from "./components/StatusRow";
import { UsbNetCard } from "./components/UsbNetCard";
import {
  useDnsProxy,
  useDnsUpstreamQuery,
  useDnsUpstreamSave,
  useIpPassthrough,
  useLanIpSave,
  useMacBindMutations,
  useMacBindQuery,
  useNetconfigStatus,
  useUsbNet,
} from "./hooks";

// 面板进场(§5.1):fade + y 12px→0,stagger 40ms/项
const PANEL_ENTER: Transition = { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] };

function Stagger({ index, children }: { index: number; children: ReactNode }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ ...PANEL_ENTER, delay: index * 0.04 }}
    >
      {children}
    </motion.div>
  );
}

export default function NetconfigPage() {
  const { t } = useT("netconfig");
  const status = useNetconfigStatus();
  const ipPassthrough = useIpPassthrough();
  const usbNet = useUsbNet(status.data);
  const dnsProxy = useDnsProxy();
  const dnsUpstreamQuery = useDnsUpstreamQuery();
  const dnsUpstream = useDnsUpstreamSave();
  const lanIp = useLanIpSave();
  const macBindQuery = useMacBindQuery();
  const macBind = useMacBindMutations();
  const ready = !!status.data;

  return (
    <>
      <PageHeader domain="network" title={t("nav:networkSettings")} />
      <div className="space-y-4">
        <Stagger index={0}>
          <StatusRow
            data={status.data}
            pending={status.pending}
            failed={status.failed}
            retrying={status.query.isFetching}
            onRetry={() => void status.query.refetch()}
            usbNetPendingMode={usbNet.pendingMode}
            onDismissUsbPending={usbNet.dismissPending}
          />
        </Stagger>
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <Stagger index={1}>
            <IpPassthroughCard
              ipPassStatus={status.data?.ipPassStatus === true}
              ready={ready}
              actions={ipPassthrough}
            />
          </Stagger>
          <Stagger index={2}>
            <UsbNetCard currentMode={status.data?.currentUsbNetMode} ready={ready} usb={usbNet} />
          </Stagger>
        </div>
        <Stagger index={3}>
          <DnsProxyCard data={status.data} ready={ready} dnsProxy={dnsProxy} />
        </Stagger>
        <Stagger index={4}>
          <DnsUpstreamCard
            query={dnsUpstreamQuery}
            actions={dnsUpstream}
            dnsV6Enabled={status.data?.DNSV6ProxyStatus === true}
            statusReady={ready}
          />
        </Stagger>
        <Stagger index={5}>
          <LanIpCard data={status.data} ready={ready} save={lanIp} />
        </Stagger>
        <Stagger index={6}>
          <MacBindCard query={macBindQuery} actions={macBind} />
        </Stagger>
      </div>
    </>
  );
}
