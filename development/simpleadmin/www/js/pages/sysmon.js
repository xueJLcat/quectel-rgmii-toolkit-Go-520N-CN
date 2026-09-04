function simpleSysmon() {
  return {
    cpuUsagePercent: 0,
    ramUsagePercent: 0,
    ramUsedHuman: '-',
    ramTotalHuman: '-',
    loadAverage: '-',
    uptime: '-',
    processCount: 0,
    processes: [],
    sortBy: 'cpu',
    monitorLoaded: false,
    monitorLoadFailed: false,
    lastUpdate: '',

    t(key) {
      return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
    },

    watch: {
      sortBy() {
        this.fetchMonitor();
      }
    },

    clampPercent(value) {
      const number = Number(value);
      if (!Number.isFinite(number)) return 0;
      return Math.max(0, Math.min(100, Math.round(number)));
    },

    gaugeDashOffset(value) {
      const arcLength = 301.6;
      return String((arcLength * (100 - this.clampPercent(value)) / 100).toFixed(1));
    },

    formatRamUsage() {
      if (this.ramUsedHuman && this.ramUsedHuman !== '-' && this.ramTotalHuman && this.ramTotalHuman !== '-') {
        return `${this.ramUsedHuman} / ${this.ramTotalHuman}`;
      }
      return this.t('获取中...');
    },

    formatCpu(value) {
      const number = Number(value);
      if (!Number.isFinite(number)) return '-';
      return number.toFixed(1) + '%';
    },

    formatNum(value) {
      const number = Number(value);
      if (!Number.isFinite(number)) return '-';
      return number.toFixed(1);
    },

    isSysmonPageActive() {
      if (!window.SimpleAdminSpaMode) return true;
      const section = document.querySelector('.sa-page[data-page="sysmon"]');
      return !!(section && section.classList.contains('active'));
    },

    fetchMonitor() {
      // 仅当系统监控页正在显示时才请求数据：离开页面后任何路径都不再触发
      // /api/system_monitor，避免后台轮询无谓读取设备 /proc、增加 CPE 压力。
      if (!this.isSysmonPageActive()) return Promise.resolve(false);
      if (!SimpleAdmin.Api.systemMonitor) return Promise.resolve(false);
      if (this._monitorFetchInFlight) return this._monitorFetchPromise || Promise.resolve(false);
      this._monitorFetchInFlight = true;
      this._monitorFetchPromise = SimpleAdmin.Api.systemMonitor({ sort: this.sortBy })
        .then((data) => {
          if (!data) throw new Error('empty monitor data');
          this.cpuUsagePercent = Number(data.cpuUsagePercent) || 0;
          this.ramUsagePercent = Number(data.ramUsagePercent) || 0;
          this.ramUsedHuman = data.ramUsedHuman || '-';
          this.ramTotalHuman = data.ramTotalHuman || '-';
          this.loadAverage = data.loadAverage || '-';
          this.uptime = data.uptime || '-';
          this.processCount = Number(data.processCount) || 0;
          this.processes = Array.isArray(data.processes) ? data.processes : [];
          this.monitorLoaded = true;
          this.monitorLoadFailed = false;
          const now = new Date();
          const pad = (value) => String(value).padStart(2, '0');
          this.lastUpdate = `${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`;
          return true;
        })
        .catch((error) => {
          console.error('读取系统监控数据失败：', error);
          this.monitorLoaded = false;
          this.monitorLoadFailed = true;
          return false;
        })
        .finally(() => {
          this._monitorFetchInFlight = false;
          this._monitorFetchPromise = null;
        });
      return this._monitorFetchPromise;
    },

    retryMonitor() {
      this.monitorLoadFailed = false;
      this.fetchMonitor();
    },

    startPolling() {
      // 轮询器只在系统监控页可见期间运行；离开页面由页面切换事件停止。
      // 即便轮询器因竞态仍在运行，fetchMonitor 内部也会按页面可见性拦截请求。
      if (!this._monitorPoller && SimpleAdmin.Poll) {
        this._monitorPoller = SimpleAdmin.Poll.create({
          interval: 3000,
          tick: () => this.fetchMonitor()
        });
      }
      if (this._monitorPoller) this._monitorPoller.start();
    },

    stopPolling() {
      if (this._monitorPoller) this._monitorPoller.stop();
    },

    init() {
      if (!this._pageChangeBound) {
        this._pageChangeBound = true;
        window.addEventListener('simpleadmin:page-changed', (event) => {
          const detail = event && event.detail;
          if (detail && detail.page === 'sysmon') {
            this.fetchMonitor();
            this.startPolling();
          } else {
            this.stopPolling();
          }
        });
      }

      this.fetchMonitor();
      this.startPolling();
    },
  };
}

if (window.SimpleAdminSpaMode) {
  (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).sysmon = simpleSysmon;
} else {
  mountSimpleAdminVueApp(simpleSysmon);
}
