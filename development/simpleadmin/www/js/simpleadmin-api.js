(function (global) {
  'use strict';

  const root = global.SimpleAdmin || {};

  root.Api = root.Api || (function () {
    const wsPath = '/api/ws';
    // 小区扫描后端最长约 182 秒(命令超时 180s+等待余量),前端请求超时必须大于它,否则扫描临近完成时前端会先超时并断开整个 WS 连接。
    const REQUEST_TIMEOUT_MS = 240 * 1000;
    let ws = null;
    let connectPromise = null;
    let nextId = 1;
    const pending = new Map();
    let sessionRedirecting = false;

    function redirectToLogin() {
      if (sessionRedirecting) return;
      sessionRedirecting = true;
      try {
        global.sessionStorage.setItem('simpleadmin.postLoginHash', global.location.hash || '');
      } catch (_) { /* ignore */ }
      try { global.location.replace('/login.html'); } catch (_) { /* ignore */ }
    }

    function probeSessionOnHandshakeFailure() {
      if (sessionRedirecting || typeof global.fetch !== 'function') return;
      try {
        global.fetch('/api/get_language', { cache: 'no-store' })
          .then((response) => {
            if (response && response.status === 401) redirectToLogin();
          })
          .catch(() => { /* probe failed: treat as network fault, keep original reject */ });
      } catch (_) { /* probe unavailable: treat as network fault */ }
    }

    function wsUrl() {
      const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
      return `${scheme}://${location.host}${wsPath}`;
    }

    function rejectAll(error) {
      pending.forEach((item) => item.reject(error));
      pending.clear();
    }

    function makeResponse(message) {
      const status = Number(message.status) || 0;
      const body = message.body || '';
      return {
        ok: status >= 200 && status < 300,
        status,
        headers: message.headers || {},
        text() {
          return Promise.resolve(body);
        },
        json() {
          return Promise.resolve().then(() => (body ? JSON.parse(body) : null));
        }
      };
    }


    function dispatchServerEvent(message) {
      if (typeof global.dispatchEvent !== 'function' || typeof global.CustomEvent !== 'function') return;
      global.dispatchEvent(new CustomEvent('simpleadmin:server-event', { detail: message }));
      global.dispatchEvent(new CustomEvent(`simpleadmin:${message.event}`, { detail: message.data || {} }));
    }

    function connect() {
      if (ws && ws.readyState === WebSocket.OPEN) return Promise.resolve(ws);
      if (connectPromise) return connectPromise;

      connectPromise = new Promise((resolve, reject) => {
        const socket = new WebSocket(wsUrl());
        ws = socket;
        let opened = false;

        socket.onopen = () => {
          opened = true;
          connectPromise = null;
          resolve(socket);
        };
        socket.onerror = () => {
          if (!opened) probeSessionOnHandshakeFailure();
          const error = new Error('WebSocket connection failed');
          connectPromise = null;
          reject(error);
          rejectAll(error);
        };
        socket.onclose = () => {
          if (ws !== socket) return;
          ws = null;
          connectPromise = null;
          rejectAll(new Error('WebSocket connection closed'));
        };
        socket.onmessage = (event) => {
          let message;
          try {
            message = JSON.parse(String(event.data || '{}'));
          } catch (error) {
            console.error('Invalid WebSocket API response:', error);
            return;
          }
          if (message.type === 'event' && message.event) {
            dispatchServerEvent(message);
            return;
          }
          const item = pending.get(message.id);
          if (!item) return;
          pending.delete(message.id);
          if (message.error && !message.status) {
            item.reject(new Error(message.error));
            return;
          }
          item.resolve(makeResponse(message));
        };
      });

      return connectPromise;
    }

    function normalizeHeaders(headers) {
      const normalized = {};
      if (!headers) return normalized;
      if (headers instanceof Headers) {
        headers.forEach((value, key) => { normalized[key] = value; });
        return normalized;
      }
      Object.keys(headers).forEach((key) => {
        if (headers[key] !== undefined && headers[key] !== null) {
          normalized[key] = String(headers[key]);
        }
      });
      return normalized;
    }

    function serializeBody(body) {
      if (body === undefined || body === null) return '';
      if (body instanceof URLSearchParams) return body.toString();
      if (typeof body === 'string') return body;
      return String(body);
    }

    function request(path, options = {}) {
      if (!String(path || '').startsWith('/api/')) {
        return fetch(path, options);
      }
      const method = String(options.method || 'GET').toUpperCase();
      const headers = normalizeHeaders(options.headers);
      const body = serializeBody(options.body);
      if (body && !Object.keys(headers).some((key) => key.toLowerCase() === 'content-type')) {
        headers['Content-Type'] = 'application/x-www-form-urlencoded; charset=UTF-8';
      }

      return connect().then((socket) => new Promise((resolve, reject) => {
        const id = String(nextId++);
        const timer = setTimeout(() => {
          if (!pending.has(id)) return;
          pending.delete(id);
          rejectAll(new Error('request timeout'));
          try { if (socket && socket.readyState <= 1) socket.close(); } catch (_) {}
          if (ws === socket) { ws = null; }
          reject(new Error('request timeout'));
        }, REQUEST_TIMEOUT_MS);
        pending.set(id, {
          resolve: (value) => {
            clearTimeout(timer);
            if (value && value.status === 401) redirectToLogin();
            resolve(value);
          },
          reject: (error) => {
            clearTimeout(timer);
            reject(error);
          }
        });
        try {
          socket.send(JSON.stringify({ id, method, path, headers, body }));
        } catch (error) {
          pending.delete(id);
          clearTimeout(timer);
          reject(error);
        }
      }));
    }

    function postForm(path, params = {}) {
      const body = params instanceof URLSearchParams ? params : new URLSearchParams(params);
      return request(path, {
        method: 'POST',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8' },
        body
      });
    }

    return {
      request,
      text(url, options) {
        return request(url, options).then((response) => response.text());
      },
      json(url, options) {
        return request(url, options).then((response) => response.json());
      },
      url(path, params) {
        const query = params ? new URLSearchParams(params).toString() : '';
        return query ? `${path}?${query}` : path;
      },
      postForm,
      postJSON(path, params = {}) {
        return postForm(path, params).then((response) => response.json());
      },
      getDashboardData(params = {}) {
        return this.postJSON('/api/dashboard_data', Object.assign({ action: 'get' }, params));
      },
      historyData() {
        return this.postJSON('/api/history_data', {});
      },
      getNetworkDetail(params = {}) {
        return this.postJSON('/api/network_detail', params);
      },
      getWatchdog() {
        return this.postJSON('/api/get_watchdog', {});
      },
      setWatchdog(params = {}) {
        return this.postJSON('/api/set_watchdog', params);
      },
      getScheduler() {
        return this.postJSON('/api/get_scheduler', {});
      },
      setScheduler(params = {}) {
        return this.postJSON('/api/set_scheduler', params);
      },
      getTimeSync() {
        return this.postJSON('/api/get_timesync', {});
      },
      setTimeSync(params = {}) {
        return this.postJSON('/api/set_timesync', params);
      },
      timeSyncNow() {
        return this.postJSON('/api/timesync_now', {});
      },
      systemMonitor(params = {}) {
        return this.postJSON('/api/system_monitor', params);
      },
      getSMSWebhook() {
        return this.postJSON('/api/get_sms_webhook', {});
      },
      setSMSWebhook(params = {}) {
        return this.postJSON('/api/set_sms_webhook', params);
      },
      mockAT(params = {}) {
        return this.postJSON('/api/mock_at', params);
      },
      getDeviceInfo(params = {}) {
        return this.postJSON('/api/device_info_data', Object.assign({ action: 'get' }, params));
      },
      setDeviceImei(imei) {
        return this.postJSON('/api/system_data', { action: 'set_imei', imei });
      },
      networkData(params = {}) {
        return this.postJSON('/api/network_data', params);
      },
      atData(params = {}) {
        return this.postJSON('/api/at_data', params);
      },
      networkConfigData(params = {}) {
        return this.postJSON('/api/network_config_data', params);
      },
      firewallData(params = {}) {
        return this.postJSON('/api/firewall_data', params);
      },
      systemData(params = {}) {
        return this.postJSON('/api/system_data', params);
      },
      smsData(params = {}) {
        return this.postJSON('/api/sms_data', params);
      },
      signalData(params = {}) {
        return this.postJSON('/api/signal_data', params);
      },
      getAT(atcmd, options = {}) {
        const params = new URLSearchParams({ atcmd });
        if (options.force) params.set('force', '1');
        if (options.wait !== undefined) params.set('wait', options.wait ? '1' : '0');
        return request('/api/get_atcache', {
          method: 'POST',
          cache: 'no-store',
          headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8' },
          body: params
        });
      },
      refreshAT(atcmd) {
        return this.getAT(atcmd, { force: true, wait: true });
      },
      getATText(atcmd, options = {}) {
        return this.getAT(atcmd, options).then((response) => response.text());
      },
      getUptime() {
        return request('/api/get_uptime');
      },
      getPing() {
        return request('/api/get_ping');
      },
      getTTLStatus() {
        return request('/api/get_ttl_status');
      },
      setTTL(ttlvalue) {
        return request(this.url('/api/set_ttl', { ttlvalue }));
      },
      getLanguage() {
        return request('/api/get_language', { cache: 'no-store' });
      },
      setLanguage(language) {
        return request(this.url('/api/set_language', { language }), { method: 'POST' });
      },
      getTheme() {
        return request('/api/get_theme', { cache: 'no-store' });
      },
      setTheme(theme) {
        return request(this.url('/api/set_theme', { theme }), { method: 'POST' });
      },
      setPassword(currentPassword, newPassword, confirmPassword) {
        const params = new URLSearchParams({
          current_password: currentPassword || '',
          new_password: newPassword || '',
          confirm_password: confirmPassword || ''
        });
        return request('/api/set_password', {
          method: 'POST',
          cache: 'no-store',
          headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8' },
          body: params
        });
      }
    };
  })();

  // 会话保活:业务请求全部经 WS 网关,浏览器 Cookie 不会随 WS 帧续期。
  // 定期发一个普通 HTTP 请求穿过服务端会话中间件,触发滑动续期重签
  // Cookie,避免"页面持续活跃却在登录 24h 后整点被登出"。静默执行,
  // 忽略状态与错误(未登录时返回 401 无副作用)。
  const SESSION_KEEPALIVE_INTERVAL_MS = 15 * 60 * 1000;
  function startSessionKeepalive() {
    if (global.__simpleadminKeepaliveStarted || typeof global.setInterval !== 'function') return;
    global.__simpleadminKeepaliveStarted = true;
    global.setInterval(() => {
      if (typeof document !== 'undefined' && document.hidden) return;
      if (typeof global.fetch !== 'function') return;
      try {
        global.fetch('/api/get_uptime', { cache: 'no-store', credentials: 'same-origin' })
          .catch(() => { /* keepalive best-effort */ });
      } catch (_) { /* ignore */ }
    }, SESSION_KEEPALIVE_INTERVAL_MS);
  }
  startSessionKeepalive();

  global.SimpleAdmin = root;
})(window);
