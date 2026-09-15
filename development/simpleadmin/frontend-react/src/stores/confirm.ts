// 确认框状态(zustand):命令式 confirm() 返回挂起的 Promise<boolean>,resolve 存在 store 里,
// 由 <ConfirmDialog /> 消费 options 并在确认/取消/ESC/点击遮罩时 settle。
// 语义对齐 Vue 版 SimpleAdmin.UI.confirm:同一时刻只有一代确认框——重复调用 confirm() 时
// 先把上一代按"取消"结算,避免 promise 悬挂与破坏性操作被双份回调执行两次。
import { create } from "zustand";

export interface ConfirmOptions {
  title: string;
  message?: string;
  confirmText?: string;
  cancelText?: string;
  /** danger 时确认按钮用红色 danger 变体 */
  danger?: boolean;
  /** 需输入指定确认词才允许确认(关机/恢复出厂等高危场景) */
  requireWord?: string;
}

export interface ConfirmState {
  options: ConfirmOptions | null;
  resolve: ((confirmed: boolean) => void) | null;
  confirm: (options: ConfirmOptions) => Promise<boolean>;
  settle: (confirmed: boolean) => void;
}

export const useConfirmStore = create<ConfirmState>()((set, get) => ({
  options: null,
  resolve: null,
  confirm: (options) => {
    const pending = get().resolve;
    if (pending) {
      set({ options: null, resolve: null });
      pending(false);
    }
    return new Promise<boolean>((resolve) => {
      set({ options, resolve });
    });
  },
  settle: (confirmed) => {
    const { resolve } = get();
    set({ options: null, resolve: null });
    resolve?.(confirmed);
  },
}));
