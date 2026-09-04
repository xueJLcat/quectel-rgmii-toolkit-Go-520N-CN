(function (global) {
  'use strict';

  const root = global.SimpleAdmin || (global.SimpleAdmin = {});

  root.Poll = root.Poll || (function () {
    function create(options) {
      const opts = options || {};
      const tick = typeof opts.tick === 'function' ? opts.tick : null;
      const interval = Math.max(1000, Number(opts.interval) || 30000);
      const immediate = opts.immediate !== false;
      const pauseWhenHidden = opts.pauseWhenHidden !== false;
      const maxBackoff = Math.max(1, Number(opts.maxBackoff) || 8);

      let running = false;
      let inFlight = false;
      let flight = null;
      let failureCount = 0;
      let timeout = null;
      let stoppedByPagehide = false;

      function backoffDelay() {
        if (failureCount === 0) return interval;
        const multiplier = Math.min(Math.pow(2, failureCount), maxBackoff);
        return interval * multiplier;
      }

      function schedule(delay) {
        if (timeout) global.clearTimeout(timeout);
        timeout = global.setTimeout(run, delay);
      }

      function pausedByVisibility() {
        return pauseWhenHidden && typeof document !== 'undefined' && document.hidden;
      }

      function execute() {
        if (!running || pausedByVisibility() || !tick) return null;
        if (inFlight) return flight;
        inFlight = true;
        flight = Promise.resolve()
          .then(() => tick())
          .then(() => {
            failureCount = 0;
          })
          .catch(() => {
            failureCount += 1;
          })
          .then(() => {
            inFlight = false;
            flight = null;
            if (running && !pausedByVisibility()) schedule(backoffDelay());
          });
        return flight;
      }

      function run() {
        timeout = null;
        if (!running || pausedByVisibility()) return;
        if (!execute()) schedule(interval);
      }

      function onVisibilityChange() {
        if (!running) return;
        if (document.hidden) {
          if (timeout) {
            global.clearTimeout(timeout);
            timeout = null;
          }
          return;
        }
        run();
      }

      function onPageHide() {
        // pagehide 既发生在页面销毁也发生在进入 bfcache。仅当轮询器当时
        // 在运行才记录,供 pageshow(bfcache 恢复)时重启;页面真销毁时
        // 监听器随文档一起消失,无泄漏。
        stoppedByPagehide = running;
        stop();
        if (stoppedByPagehide) global.addEventListener('pageshow', onPageShow);
      }

      function onPageShow(event) {
        global.removeEventListener('pageshow', onPageShow);
        if (event && event.persisted && stoppedByPagehide) {
          stoppedByPagehide = false;
          start();
        }
      }

      function start() {
        if (running) return api;
        running = true;
        if (pauseWhenHidden) {
          document.addEventListener('visibilitychange', onVisibilityChange);
        }
        global.addEventListener('pagehide', onPageHide);
        if (immediate) run();
        else schedule(interval);
        return api;
      }

      function stop() {
        if (!running) return api;
        running = false;
        if (timeout) {
          global.clearTimeout(timeout);
          timeout = null;
        }
        document.removeEventListener('visibilitychange', onVisibilityChange);
        global.removeEventListener('pagehide', onPageHide);
        return api;
      }

      function refresh() {
        return execute() || Promise.resolve();
      }

      const api = { start, stop, refresh };
      return api;
    }

    function onPageReturn(pageId, fn) {
      if (typeof fn !== 'function') return () => {};
      const handler = (event) => {
        const detail = event && event.detail;
        if (detail && detail.page === pageId) fn();
      };
      global.addEventListener('simpleadmin:page-changed', handler);
      return () => global.removeEventListener('simpleadmin:page-changed', handler);
    }

    return { create, onPageReturn };
  })();

  global.SimpleAdmin = root;
})(window);
