// e2e 共享常量:鉴权 storageState 路径 + 15 页导航表(与 src/app/routes.tsx 一一对应,
// 标题取 locales/zh-CN/nav.json 词条,默认语言 zh-CN)。
export const AUTH_STATE_PATH = "e2e/.auth/user.json";

export interface NavPage {
  /** HashRouter 路由 id(同 features 目录名) */
  id: string;
  /** 侧边栏链接与 PageHeader h1 的中文标题(nav.json) */
  title: string;
}

export const NAV_PAGES: readonly NavPage[] = [
  { id: "dashboard", title: "总览" },
  { id: "sysmon", title: "系统监控" },
  { id: "signal", title: "信号详情" },
  { id: "network", title: "蜂窝网络" },
  { id: "celllock", title: "小区锁定" },
  { id: "netdetail", title: "网络详情" },
  { id: "netconfig", title: "网络设置" },
  { id: "firewall", title: "防火墙" },
  { id: "sms", title: "短信服务" },
  { id: "atcommands", title: "AT 命令" },
  { id: "console", title: "控制台" },
  { id: "diag", title: "网络诊断" },
  { id: "automation", title: "自动化" },
  { id: "settings", title: "系统设置" },
  { id: "deviceinfo", title: "设备信息" },
];
