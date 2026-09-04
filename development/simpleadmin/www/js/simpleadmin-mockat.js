(function (global) {
  'use strict';

  const root = global.SimpleAdmin || {};

  root.MockAT = root.MockAT || (function () {
    function normalizePayload(payload) {
      if (Array.isArray(payload)) return payload.join('\n');
      return String(payload == null ? '' : payload);
    }

    function logResult(label, result) {
      if (label === 'parse') {
        console.log('[SimpleAdmin MockAT parse]', result);
      } else {
        console.log('[SimpleAdmin MockAT]', result);
      }
      return result;
    }

    function request(params) {
      return root.Api.mockAT(params).then((result) => {
        if (result && result.ok === false) {
          console.warn('[SimpleAdmin MockAT]', result.error || result);
        }
        return result;
      });
    }

    function refreshDashboard() {
      const apps = root.Vue && root.Vue.apps;
      const app = (apps && apps['#dashboardApp']) || (root.Vue && root.Vue.currentApp);
      if (app && typeof app.fetchNetworkInfo === 'function') {
        app.fetchNetworkInfo();
      }
    }

    const api = {
      help() {
        const text = [
          'SimpleAdmin MockAT browser console commands:',
          '  SimpleAdmin.MockAT.at(`paste full dashboard AT response here`)',
          '  SimpleAdmin.MockAT.qca(`paste QCAINFO response here`)',
          '  SimpleAdmin.MockAT.qeng(`paste QENG response here`)',
          '  SimpleAdmin.MockAT.parse()   // print backend parsed dashboard JSON',
          '  SimpleAdmin.MockAT.show()    // show current mock payload status',
          '  SimpleAdmin.MockAT.clear()   // clear all; or clear("at"/"qca"/"qeng")',
          'Aliases: saAt(`...`), saQca(`...`), saQeng(`...`), saParseAT(), saShowAT(), saClearAT()'
        ].join('\n');
        console.log(text);
        return text;
      },
      set(kind, payload) {
        return request({ action: 'set', kind, payload: normalizePayload(payload) })
          .then((result) => {
            refreshDashboard();
            return logResult('set', result);
          });
      },
      at(payload) { return this.set('at', payload); },
      dashboard(payload) { return this.set('dashboard', payload); },
      qca(payload) { return this.set('qca', payload); },
      qcainfo(payload) { return this.set('qcainfo', payload); },
      qeng(payload) { return this.set('qeng', payload); },
      clear(kind = '') {
        return request({ action: 'clear', kind })
          .then((result) => {
            refreshDashboard();
            return logResult('clear', result);
          });
      },
      show() {
        return request({ action: 'show' }).then((result) => logResult('show', result));
      },
      parse(options = {}) {
        const params = Object.assign({ action: 'parse' }, options || {});
        return request(params).then((result) => logResult('parse', result));
      }
    };

    return api;
  })();

  global.saAt = function (payload) { return root.MockAT.at(payload); };
  global.saQca = function (payload) { return root.MockAT.qca(payload); };
  global.saQeng = function (payload) { return root.MockAT.qeng(payload); };
  global.saParseAT = function (options) { return root.MockAT.parse(options); };
  global.saShowAT = function () { return root.MockAT.show(); };
  global.saClearAT = function (kind) { return root.MockAT.clear(kind); };

  global.SimpleAdmin = root;
})(window);
