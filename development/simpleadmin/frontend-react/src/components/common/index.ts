// common 通用交互组件统一出口:页面/壳层只从这里 import。
// ConfirmDialog/RebootOverlay 为全局单例,由壳层挂载一次,经 stores/confirm、stores/reboot 驱动。
export * from "./confirm-dialog";
export * from "./copy-button";
export * from "./danger-zone";
export * from "./empty-state";
export * from "./error-retry";
export * from "./metric-card";
export * from "./reboot-overlay";
export * from "./status-chip";
