// 全局 UI 状态(zustand):主题(应用+持久化)、桌面侧栏折叠、移动端抽屉开关。
// 主题初始值读 DOM(data-bs-theme)——首屏防闪脚本已把 localStorage/服务端默认写进 <html>,
// 因此 readCurrentTheme() 即为真实生效值;toggle/setTheme 时同步 applyTheme + storeTheme。
import { create } from "zustand";

import { applyTheme, readCurrentTheme, storeTheme } from "@/lib/theme";
import type { ThemeMode } from "@/lib/theme";

export interface UiState {
  theme: ThemeMode;
  setTheme: (mode: ThemeMode) => void;
  toggleTheme: () => void;
  sidebarCollapsed: boolean;
  toggleSidebar: () => void;
  mobileNavOpen: boolean;
  setMobileNavOpen: (open: boolean) => void;
}

export const useUiStore = create<UiState>()((set, get) => ({
  theme: readCurrentTheme(),
  setTheme: (mode) => {
    applyTheme(mode);
    storeTheme(mode);
    set({ theme: mode });
  },
  toggleTheme: () => {
    get().setTheme(get().theme === "dark" ? "light" : "dark");
  },
  sidebarCollapsed: false,
  toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
  mobileNavOpen: false,
  setMobileNavOpen: (open) => set({ mobileNavOpen: open }),
}));
