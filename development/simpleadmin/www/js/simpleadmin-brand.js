(function (global) {
  'use strict';

  // 品牌型号硬编码为 RG520N-CN，不再动态识别：
  // 不再请求 /api/module_model，也不再使用 localStorage 缓存与"获取中..."占位。
  // 导出 API 形状保持不变（init/refresh/apply/setPageTitle/getName），
  // simpleadmin-spa.js 等调用方无需改动。
  const MODEL_NAME = 'RG520N-CN';

  const root = global.SimpleAdmin || {};

  root.Brand = root.Brand || (function () {
    let currentName = MODEL_NAME;
    let currentPageTitle = '';

    function markFromName(name) {
      const model = String(name || '').trim();
      const chars = Array.from(model.replace(/[^0-9A-Za-z\u4e00-\u9fa5]/g, ''));
      if (chars.length === 0) return '--';
      return chars.slice(0, 2).join('').toUpperCase();
    }

    function setText(selector, value) {
      document.querySelectorAll(selector).forEach((element) => {
        element.textContent = value;
      });
    }

    function updateDocumentTitle() {
      const brandName = currentName || 'SimpleAdmin';
      if (currentPageTitle) {
        document.title = `${currentPageTitle} - ${brandName}`;
        return;
      }
      document.title = brandName;
    }

    function apply(name) {
      const model = String(name === undefined || name === null ? '' : name).trim() || MODEL_NAME;
      currentName = model;
      setText('[data-simpleadmin-brand-title]', model);
      setText('[data-simpleadmin-brand-mark]', markFromName(model));
      updateDocumentTitle();
      return model;
    }

    // 不再发请求，直接解析为硬编码型号，保持 refresh() 的 Promise 语义。
    function refresh() {
      apply(MODEL_NAME);
      return Promise.resolve(MODEL_NAME);
    }

    function init() {
      apply(MODEL_NAME);
    }

    function setPageTitle(title) {
      currentPageTitle = String(title || '').trim();
      updateDocumentTitle();
    }

    return {
      init,
      refresh,
      apply,
      setPageTitle,
      getName() {
        return currentName;
      }
    };
  })();

  global.SimpleAdmin = root;

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', root.Brand.init, { once: true });
  } else {
    root.Brand.init();
  }
})(window);
