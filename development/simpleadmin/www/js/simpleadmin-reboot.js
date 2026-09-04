(function (global) {
  'use strict';

  const root = global.SimpleAdmin || (global.SimpleAdmin = {});

  root.Reboot = root.Reboot || (function () {
    const DEFAULT_SECONDS = 40;
    let confirmBound = false;
    let timer = null;
    let pendingOnDone = null;

    function t(text) {
      return root.Lang && typeof root.Lang.t === 'function' ? root.Lang.t(text) : text;
    }

    function ui() {
      return root.UI;
    }

    // 用于后端广播操作失败时中止"重启中"倒计时(如 IP 透传禁用失败)。
    // 无活动倒计时时为空操作;否则清除定时器、隐藏倒计时遮罩并复位内部状态。
    function cancelCountdown() {
      if (!timer) return;
      global.clearInterval(timer);
      timer = null;
      pendingOnDone = null;
      const overlay = document.getElementById('sa-reboot-progress-overlay');
      if (overlay && ui()) ui().modal.close(overlay);
    }

    function finishCountdown() {
      const overlay = document.getElementById('sa-reboot-progress-overlay');
      if (overlay && ui()) ui().modal.close(overlay);
      if (ui()) ui().notify('success', '重启倒计时结束');
      const onDone = pendingOnDone;
      pendingOnDone = null;
      if (typeof onDone === 'function') onDone();
      if (typeof global.dispatchEvent === 'function' && typeof global.CustomEvent === 'function') {
        global.dispatchEvent(new CustomEvent('simpleadmin:reboot-done'));
      }
    }

    function countdown(seconds, onDone, message) {
      const overlay = document.getElementById('sa-reboot-progress-overlay');
      if (!overlay || !ui()) return;
      if (typeof onDone === 'function') pendingOnDone = onDone;
      if (timer) {
        global.clearInterval(timer);
        timer = null;
      }
      const messageEl = overlay.querySelector('.sa-modal p');
      if (messageEl) {
        const key = message || '重启中…请稍候,请勿关闭页面';
        // 同步 i18n 键属性,否则语言观察器会用静态默认文案覆盖自定义文案。
        messageEl.setAttribute('data-simpleadmin-i18n-key', key);
        messageEl.textContent = t(key);
      }
      const number = document.getElementById('saRebootCountdown');
      const total = Number(seconds) > 0 ? Math.floor(Number(seconds)) : DEFAULT_SECONDS;
      if (number) number.textContent = String(total);
      // 倒计时为不可中断流程:传入空 onEscape 禁用 simpleadmin-ui.js 默认的 Esc 关闭,
      // 避免倒计时中按 Esc 遮罩消失而定时器仍在运行、到点回调照常执行,误导用户以为重启已取消。
      ui().modal.open(overlay, { onEscape: () => {} });
      const targetTs = Date.now() + total * 1000;
      timer = global.setInterval(() => {
        const remaining = Math.max(0, Math.ceil((targetTs - Date.now()) / 1000));
        if (number) number.textContent = String(remaining);
        if (remaining <= 0) {
          global.clearInterval(timer);
          timer = null;
          finishCountdown();
        }
      }, 1000);
    }

    function startReboot() {
      countdown(DEFAULT_SECONDS);
      root.Api.systemData({ action: 'reboot' })
        .then((data) => {
          // 后端 reboot 只返回 {ok, response},没有 error 字段:
          // ok:false 即模块明确拒绝重启,必须中止倒计时并提示,
          // 否则倒计时照常走完,用户被误导以为重启成功。
          if (data && data.ok === false) {
            cancelCountdown();
            if (ui()) ui().notify('danger', t('重启失败'));
          }
        })
        .catch(() => {
          // 请求未送达(传输失败)同样不能假装重启成功。
          cancelCountdown();
          if (ui()) ui().notify('danger', t('重启失败'));
        });
    }

    function bindConfirmOverlay(overlay) {
      if (confirmBound) return;
      confirmBound = true;
      const okBtn = document.getElementById('saRebootOk');
      const cancelBtn = document.getElementById('saRebootCancel');
      const cancel = () => {
        // 取消确认即放弃本次重启请求,清除回调避免残留到下次流程。
        pendingOnDone = null;
        ui().modal.close(overlay);
      };
      if (okBtn) {
        okBtn.addEventListener('click', () => {
          ui().modal.close(overlay);
          startReboot();
        });
      }
      if (cancelBtn) cancelBtn.addEventListener('click', cancel);
      overlay.addEventListener('click', (event) => {
        if (event.target === overlay) cancel();
      });
    }

    function request(options) {
      const overlay = document.getElementById('sa-reboot-overlay');
      if (!overlay || !ui()) return;
      const opts = options || {};
      pendingOnDone = typeof opts.onDone === 'function' ? opts.onDone : null;
      const title = document.getElementById('saRebootTitle');
      const message = document.getElementById('saRebootMessage');
      const okBtn = document.getElementById('saRebootOk');
      const cancelBtn = document.getElementById('saRebootCancel');
      // 写入自定义文案时同步更新 i18n 键属性:元素上的静态
      // data-simpleadmin-i18n-key 否则会被语言观察器下一轮重译覆盖回默认文案。
      const setI18nText = (el, key) => {
        if (!el) return;
        el.setAttribute('data-simpleadmin-i18n-key', key);
        el.textContent = t(key);
      };
      setI18nText(title, opts.title || '重启调制解调器');
      setI18nText(message, opts.message || '这将重启调制解调器。继续吗?');
      setI18nText(okBtn, opts.confirmText || '重启');
      setI18nText(cancelBtn, '取消');
      bindConfirmOverlay(overlay);
      ui().modal.open(overlay, { focus: okBtn });
    }

    return {
      request,
      countdown,
      cancelCountdown
    };
  })();

  global.SimpleAdmin = root;
})(window);
