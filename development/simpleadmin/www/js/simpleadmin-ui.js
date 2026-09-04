(function (global) {
  'use strict';

  const root = global.SimpleAdmin || {};

  const TOAST_TYPES = ['success', 'danger', 'warning', 'info'];
  const TOAST_ROOT_ID = 'sa-toast-root';
  const TOAST_LIMIT = 3;
  const modalStates = new Map();

  function t(text) {
    return root.Lang && typeof root.Lang.t === 'function' ? root.Lang.t(text) : text;
  }

  const THEME_STORAGE_KEY = 'theme';

  function normalizeTheme(theme) {
    return theme === 'dark' ? 'dark' : 'light';
  }

  function isValidTheme(theme) {
    return theme === 'dark' || theme === 'light';
  }

  function readStoredTheme() {
    try {
      return global.localStorage.getItem(THEME_STORAGE_KEY);
    } catch (e) {
      return null;
    }
  }

  function persistTheme(theme) {
    try {
      global.localStorage.setItem(THEME_STORAGE_KEY, theme);
    } catch (e) { /* 存储不可用时仅当前会话生效 */ }
  }

  // 主题写回仅更新 data-bs-theme 与 colorScheme;只有显式传入
  // { persist: true } 才写入 localStorage(顶部切换按钮、设置页保存默认主题)。
  // 首屏加载不写存储,保证设备端默认主题对新浏览器持续生效。
  function applyTheme(theme, options) {
    const html = document.documentElement;
    const normalized = normalizeTheme(theme);
    if (!html) return normalized;
    html.setAttribute('data-bs-theme', normalized);
    html.style.colorScheme = normalized;
    if (options && options.persist) persistTheme(normalized);
    return normalized;
  }

  function currentTheme() {
    const html = document.documentElement;
    return normalizeTheme(html ? html.getAttribute('data-bs-theme') : 'light');
  }

  function collectFocusable(container) {
    return Array.from(container.querySelectorAll(
      'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
    )).filter((element) => element.getClientRects().length > 0);
  }

  function moveFocusIn(overlay, preferred) {
    if (preferred && typeof preferred.focus === 'function') {
      preferred.focus();
      return;
    }
    const focusables = collectFocusable(overlay);
    if (focusables.length > 0) {
      focusables[0].focus();
      return;
    }
    const target = overlay.querySelector('[role="dialog"]') || overlay.firstElementChild || overlay;
    if (!target.hasAttribute('tabindex')) target.setAttribute('tabindex', '-1');
    target.focus();
  }

  function trapTab(overlay, event) {
    const focusables = collectFocusable(overlay);
    if (focusables.length === 0) {
      event.preventDefault();
      return;
    }
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    const active = document.activeElement;
    if (event.shiftKey) {
      if (active === first || !overlay.contains(active)) {
        event.preventDefault();
        last.focus();
      }
      return;
    }
    if (active === last || !overlay.contains(active)) {
      event.preventDefault();
      first.focus();
    }
  }

  function openModal(el, options) {
    if (!el || modalStates.has(el)) return;
    const opts = options || {};
    const handler = (event) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        if (typeof opts.onEscape === 'function') opts.onEscape();
        else closeModal(el);
        return;
      }
      if (event.key === 'Tab') trapTab(el, event);
    };
    el.addEventListener('keydown', handler);
    el.style.display = 'flex';
    el.setAttribute('aria-hidden', 'false');
    modalStates.set(el, {
      previousFocus: document.activeElement,
      handler
    });
    moveFocusIn(el, opts.focus);
  }

  function closeModal(el) {
    if (!el) return;
    const state = modalStates.get(el);
    el.style.display = 'none';
    el.setAttribute('aria-hidden', 'true');
    if (!state) return;
    modalStates.delete(el);
    el.removeEventListener('keydown', state.handler);
    if (state.previousFocus && typeof state.previousFocus.focus === 'function') {
      state.previousFocus.focus();
    }
  }

  function ensureToastRoot() {
    let toastRoot = document.getElementById(TOAST_ROOT_ID);
    if (!toastRoot) {
      toastRoot = document.createElement('div');
      toastRoot.id = TOAST_ROOT_ID;
      toastRoot.className = 'sa-toast-root';
      document.body.appendChild(toastRoot);
    }
    return toastRoot;
  }

  function removeToast(toast) {
    if (!toast || !toast.isConnected) return;
    toast.remove();
  }

  function fadeOutToast(toast) {
    if (!toast || !toast.isConnected || toast.dataset.simpleadminToastLeaving === '1') return;
    toast.dataset.simpleadminToastLeaving = '1';
    toast.style.transition = 'opacity .3s ease';
    toast.style.opacity = '0';
    global.setTimeout(() => removeToast(toast), 300);
  }

  function notify(type, text) {
    if (!document.body) return;
    const kind = TOAST_TYPES.includes(type) ? type : 'info';
    const toastRoot = ensureToastRoot();
    while (toastRoot.childElementCount >= TOAST_LIMIT) {
      removeToast(toastRoot.firstElementChild);
    }
    const toast = document.createElement('div');
    toast.className = `sa-toast sa-toast-${kind}`;
    toast.setAttribute('role', 'status');
    toast.textContent = t(text);
    toast.addEventListener('click', () => removeToast(toast));
    toastRoot.appendChild(toast);
    global.setTimeout(() => fadeOutToast(toast), kind === 'danger' ? 6000 : 4000);
  }

  function confirmDialog(options) {
    const opts = options || {};
    const overlay = document.getElementById('sa-confirm-overlay');
    if (!overlay) return;
    // 上一个确认框仍打开时直接忽略:叠加绑定会让一次点击触发两代回调,
    // 破坏性操作可能被执行两次。
    if (modalStates.has(overlay)) return;
    const titleEl = document.getElementById('saConfirmTitle');
    const messageEl = document.getElementById('saConfirmMessage');
    const cancelBtn = document.getElementById('saConfirmCancel');
    const okBtn = document.getElementById('saConfirmOk');

    // 写入自定义文案时同步更新 i18n 键属性:元素上的静态
    // data-simpleadmin-i18n-key 会被语言观察器下一轮重译覆盖回默认文案。
    const setI18nText = (el, key) => {
      if (!el) return;
      if (key) {
        el.setAttribute('data-simpleadmin-i18n-key', key);
        el.textContent = t(key);
      } else {
        el.removeAttribute('data-simpleadmin-i18n-key');
        el.textContent = '';
      }
    };
    setI18nText(titleEl, opts.title || '');
    setI18nText(messageEl, opts.message || '');
    if (okBtn) {
      okBtn.classList.toggle('btn-danger', !!opts.danger);
      okBtn.classList.toggle('btn-primary', !opts.danger);
    }
    setI18nText(okBtn, opts.confirmText || '确认');
    setI18nText(cancelBtn, opts.cancelText || '取消');

    let finished = false;
    const finish = (confirmed) => {
      if (finished) return;
      finished = true;
      overlay.removeEventListener('click', onOverlayClick);
      if (okBtn) okBtn.removeEventListener('click', onOkClick);
      if (cancelBtn) cancelBtn.removeEventListener('click', onCancelClick);
      closeModal(overlay);
      if (confirmed && typeof opts.onConfirm === 'function') opts.onConfirm();
    };
    const onOkClick = () => finish(true);
    const onCancelClick = () => finish(false);
    const onOverlayClick = (event) => {
      if (event.target === overlay) finish(false);
    };

    overlay.addEventListener('click', onOverlayClick);
    if (okBtn) okBtn.addEventListener('click', onOkClick);
    if (cancelBtn) cancelBtn.addEventListener('click', onCancelClick);
    openModal(overlay, {
      focus: okBtn,
      onEscape: () => finish(false)
    });
  }

  root.UI = {
    setText(selectorOrElement, value) {
      const element = typeof selectorOrElement === 'string'
        ? document.querySelector(selectorOrElement)
        : selectorOrElement;
      if (element) element.textContent = value;
    },
    initTheme(selector) {
      const html = document.documentElement;
      if (!html) return;
      const toggleSelector = selector || '#sa-theme-toggle';
      const updateToggleLabel = (theme) => {
        const label = theme === 'dark' ? '浅色模式' : '暗夜模式';
        document.querySelectorAll(toggleSelector).forEach((toggle) => {
          toggle.dataset.simpleadminI18nKey = label;
          toggle.textContent = t(label);
        });
      };

      const stored = readStoredTheme();
      const injected = html.getAttribute('data-bs-theme');
      const initial = isValidTheme(stored) ? stored : (isValidTheme(injected) ? injected : 'light');
      updateToggleLabel(applyTheme(initial));

      document.querySelectorAll(toggleSelector).forEach((toggle) => {
        if (toggle.dataset.simpleadminThemeBound === '1') return;
        toggle.dataset.simpleadminThemeBound = '1';
        toggle.addEventListener('click', () => {
          const next = applyTheme(currentTheme() === 'dark' ? 'light' : 'dark', { persist: true });
          updateToggleLabel(next);
        });
      });
    },
    getTheme() {
      return currentTheme();
    },
    // 主题设置页保存默认主题时调用:立即应用并写入本地,
    // 使当前浏览器与刚保存的默认值保持一致。
    setTheme(theme, options) {
      const normalized = applyTheme(theme, options);
      const label = normalized === 'dark' ? '浅色模式' : '暗夜模式';
      document.querySelectorAll('#sa-theme-toggle').forEach((toggle) => {
        toggle.dataset.simpleadminI18nKey = label;
        toggle.textContent = t(label);
      });
      return normalized;
    },
    notify,
    confirm: confirmDialog,
    modal: {
      open: openModal,
      close: closeModal
    }
  };

  global.SimpleAdmin = root;

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => root.UI.initTheme(), { once: true });
  } else {
    root.UI.initTheme();
  }
})(window);
