// 导航单一来源(设计方案 §4.2):16 路由 + 6 域分组。
// 菜单 = 页标题 = 浏览器标题 = 路由 id 全部取自本表;feature 页经 lazy 从 @/features/<id>/page 分包加载。
// titleKey 为 nav 命名空间键(locales/*/nav.json);document.title 拼接格式在 app/App.tsx
// (对齐旧版 SimpleAdmin.Brand.setPageTitle:"页面标题 - 型号")。
import {
  ActivityIcon,
  BotIcon,
  CrosshairIcon,
  ForwardIcon,
  GaugeIcon,
  ListTreeIcon,
  MessageSquareIcon,
  NetworkIcon,
  RadioIcon,
  ShieldIcon,
  SlidersHorizontalIcon,
  SmartphoneIcon,
  SquareTerminalIcon,
  StethoscopeIcon,
  TerminalIcon,
  WrenchIcon,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { ComponentType } from "react";

export type DomainName = "monitor" | "network" | "security" | "comm" | "tools" | "system";

export interface PageDef {
  /** 路由 id,同 features 目录名 */
  id: string;
  /** HashRouter 路径,形如 /<id> */
  path: string;
  /** 所属域分组(决定域色与归组) */
  groupId: DomainName;
  icon: LucideIcon;
  /** nav 命名空间页面名词条键 */
  titleKey: string;
  /** 页面组件懒加载入口(vite 按页分 chunk) */
  lazy: () => Promise<{ default: ComponentType }>;
}

export const NAV_GROUPS: { id: DomainName; titleKey: string }[] = [
  { id: "monitor", titleKey: "monitoring" },
  { id: "network", titleKey: "network" },
  { id: "security", titleKey: "security" },
  { id: "comm", titleKey: "communication" },
  { id: "tools", titleKey: "tools" },
  { id: "system", titleKey: "system" },
];

export const PAGES: PageDef[] = [
  {
    id: "dashboard",
    path: "/dashboard",
    groupId: "monitor",
    icon: GaugeIcon,
    titleKey: "overview",
    lazy: () => import("@/features/dashboard/page"),
  },
  {
    id: "sysmon",
    path: "/sysmon",
    groupId: "monitor",
    icon: ActivityIcon,
    titleKey: "systemMonitor",
    lazy: () => import("@/features/sysmon/page"),
  },
  {
    id: "signal",
    path: "/signal",
    groupId: "network",
    icon: RadioIcon,
    titleKey: "signalDetails",
    lazy: () => import("@/features/signal/page"),
  },
  {
    id: "network",
    path: "/network",
    groupId: "network",
    icon: NetworkIcon,
    titleKey: "cellularNetwork",
    lazy: () => import("@/features/network/page"),
  },
  {
    id: "celllock",
    path: "/celllock",
    groupId: "network",
    icon: CrosshairIcon,
    titleKey: "cellLock",
    lazy: () => import("@/features/celllock/page"),
  },
  {
    id: "netdetail",
    path: "/netdetail",
    groupId: "network",
    icon: ListTreeIcon,
    titleKey: "networkDetails",
    lazy: () => import("@/features/netdetail/page"),
  },
  {
    id: "netconfig",
    path: "/netconfig",
    groupId: "network",
    icon: SlidersHorizontalIcon,
    titleKey: "networkSettings",
    lazy: () => import("@/features/netconfig/page"),
  },
  {
    id: "firewall",
    path: "/firewall",
    groupId: "security",
    icon: ShieldIcon,
    titleKey: "firewall",
    lazy: () => import("@/features/firewall/page"),
  },
  {
    id: "sms",
    path: "/sms",
    groupId: "comm",
    icon: MessageSquareIcon,
    titleKey: "smsService",
    lazy: () => import("@/features/sms/page"),
  },
  {
    id: "smsforward",
    path: "/smsforward",
    groupId: "comm",
    icon: ForwardIcon,
    titleKey: "smsForwarding",
    lazy: () => import("@/features/smsforward/page"),
  },
  {
    id: "atcommands",
    path: "/atcommands",
    groupId: "tools",
    icon: TerminalIcon,
    titleKey: "atCommands",
    lazy: () => import("@/features/atcommands/page"),
  },
  {
    id: "console",
    path: "/console",
    groupId: "tools",
    icon: SquareTerminalIcon,
    titleKey: "console",
    lazy: () => import("@/features/console/page"),
  },
  {
    id: "diag",
    path: "/diag",
    groupId: "tools",
    icon: StethoscopeIcon,
    titleKey: "diag",
    lazy: () => import("@/features/diag/page"),
  },
  {
    id: "automation",
    path: "/automation",
    groupId: "system",
    icon: BotIcon,
    titleKey: "automation",
    lazy: () => import("@/features/automation/page"),
  },
  {
    id: "settings",
    path: "/settings",
    groupId: "system",
    icon: WrenchIcon,
    titleKey: "systemSettings",
    lazy: () => import("@/features/settings/page"),
  },
  {
    id: "deviceinfo",
    path: "/deviceinfo",
    groupId: "system",
    icon: SmartphoneIcon,
    titleKey: "deviceInfo",
    lazy: () => import("@/features/deviceinfo/page"),
  },
];
