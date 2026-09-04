const NETWORK_COMPACT_KEY = 'simpleadmin.dashboard.compactNetworkInfo';

function readNetworkCompactState() {
  try {
    return localStorage.getItem(NETWORK_COMPACT_KEY) === '1';
  } catch (err) {
    return false;
  }
}

function saveNetworkCompactState(enabled) {
  try {
    localStorage.setItem(NETWORK_COMPACT_KEY, enabled ? '1' : '0');
  } catch (err) {
    // Ignore unavailable localStorage so the page can still work in restricted browsers.
  }
}

function getStaticNetworkInfo() {
  return {
    sim: '未激活',
    temperature: '0',
    active_sim: '-',
    network_provider: '-',
    mccmnc: '-',
    apn: '-',
    network_mode: '-',
    ipv4: '-',
    ipv6: '-',
    bands: '-',
    bandwidth: '-',
    prxqrsrp: '-',
    drxqrsrp: '-',
    rx2qrsrp: '-',
    rx3qrsrp: '-',
    earfcns: '-',
    pcc_pci: '-',
    scc_pci: '-',
    signalAssessment: '-',
    csq: '-',
    rssi: '-',
    cellID: '-',
    eNBID: '-',
    tac: '-',
    rsrqLTE: '-',
    rsrqNR: '-',
    rsrqLTEPercentage: 0,
    rsrqNRPercentage: 0,
    rsrpLTE: '-',
    rsrpNR: '-',
    rsrpLTEPercentage: 0,
    rsrpNRPercentage: 0,
    sinrLTE: '-',
    sinrNR: '-',
    sinrLTEPercentage: 0,
    sinrNRPercentage: 0,
    signalPercentage: 0,
    cpuUsagePercent: 0,
    ramUsagePercent: 0,
    ramUsedHuman: '-',
    ramTotalHuman: '-',
    internetConnection: '未连接',
    lastUpdate: new Date().toLocaleString(),
    newRefreshRate: null,
    refreshRate: 2,
    uptime: '未知',
    _uptimeParts: null,
    nr_rx_bytes: 0,
    nr_tx_bytes: 0,
    nr_rx_human: '-',
    nr_tx_human: '-',
    nr_dl_speed: '-',
    nr_ul_speed: '-',
    _prev_nr_rx: null,
    _prev_nr_tx: null,
    _prev_nr_t: null,
    compactNetworkInfo: readNetworkCompactState(),
    dashboardLoaded: false,
    updateFailed: false,
    dataError: '',
    dataPending: false,
    historyPoints: [],
    _dashboardActive: false,
    _dashboardFetchInFlight: false,
    _dashboardFetchSeq: 0,
    _dashboardFetchPromise: null,
    _mainPoll: null,
    _historyPoll: null,
    _pushRefreshTimer: null,
    _dashboardPageChangeHandler: null,
    _atCacheUpdatedHandler: null,

    toggleNetworkCompact() {
      this.compactNetworkInfo = !this.compactNetworkInfo;
      saveNetworkCompactState(this.compactNetworkInfo);
    },

    formatActiveSimStatus() {
      const simState = String(this.sim || '').trim();
      const slot = String(this.active_sim || '').trim();
      if (simState === '已激活') {
        if (slot && slot !== '-') {
          return `已激活卡${slot.replace(/^卡/, '')}`;
        }
        return '已激活';
      }
      if (slot && slot !== '-' && simState && simState !== '-') {
        return `${simState}卡${slot.replace(/^卡/, '')}`;
      }
      return simState || '-';
    },

    getProgressBarClass(percentage) {
      const value = Number(percentage);
      if (value >= 60) return 'progress-bar bg-success';
      if (value >= 40) return 'progress-bar bg-warning';
      return 'progress-bar bg-danger';
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

    formatPercent(value) {
      return `${this.clampPercent(value)}%`;
    },

    formatRamUsage() {
      if (this.ramUsedHuman && this.ramUsedHuman !== '-' && this.ramTotalHuman && this.ramTotalHuman !== '-') {
        return `${this.ramUsedHuman} / ${this.ramTotalHuman}`;
      }
      return this.t('获取中...');
    },

    isDashboardPageActive() {
      if (!window.SimpleAdminSpaMode) return true;
      const section = document.querySelector('.sa-page[data-page="dashboard"]');
      return !!(section && section.classList.contains('active'));
    },

    fetchNetworkInfo() {
      if (!this._dashboardActive || !this.isDashboardPageActive()) {
        this.stopDashboardRefresh();
        return Promise.resolve();
      }
      if (this._dashboardFetchInFlight) return this._dashboardFetchPromise || Promise.resolve();
      this._dashboardFetchInFlight = true;
      this._dashboardFetchSeq += 1;
      const seq = this._dashboardFetchSeq;
      this._dashboardFetchPromise = SimpleAdmin.Api.getDashboardData({})
        .then((data) => {
          this._dashboardFetchInFlight = false;
          this._dashboardFetchPromise = null;
          if (seq !== this._dashboardFetchSeq) return;
          if (!this._dashboardActive || !this.isDashboardPageActive()) return;
          this.updateFailed = false;
          this.applyDashboardData(data || {});
          if (data && !data.pending && data.uptimeParts) {
            this.setUptimeParts(data.uptimeParts);
          }
        })
        .catch((err) => {
          this._dashboardFetchInFlight = false;
          this._dashboardFetchPromise = null;
          if (seq !== this._dashboardFetchSeq) return;
          if (!this._dashboardActive || !this.isDashboardPageActive()) return;
          console.error('dashboard_data error:', err);
          if (!this.updateFailed) {
            this.updateFailed = true;
            SimpleAdmin.UI.notify('danger', this.t('数据更新失败'));
          }
          throw err;
        });
      return this._dashboardFetchPromise;
    },

    fetchHistory() {
      if (!SimpleAdmin.Api.historyData) return Promise.resolve();
      return SimpleAdmin.Api.historyData()
        .then((data) => {
          if (!this._dashboardActive) return;
          this.historyPoints = Array.isArray(data && data.points) ? data.points : [];
        })
        .catch((err) => {
          if (!this._dashboardActive) return;
          console.error('history_data error:', err);
          throw err;
        });
    },

    createMainPoll(immediate) {
      return SimpleAdmin.Poll.create({
        tick: () => this.fetchNetworkInfo(),
        interval: this.refreshRate * 1000,
        immediate: immediate,
        pauseWhenHidden: true
      });
    },

    startDashboardRefresh() {
      if (!this.isDashboardPageActive()) return;
      const wasActive = this._dashboardActive;
      this._dashboardActive = true;
      if (wasActive) return;
      if (this._mainPoll) this._mainPoll.stop();
      this._mainPoll = this.createMainPoll(true);
      this._mainPoll.start();
      if (!this._historyPoll) {
        this._historyPoll = SimpleAdmin.Poll.create({
          tick: () => this.fetchHistory(),
          interval: 60 * 1000,
          immediate: true,
          pauseWhenHidden: true
        });
      }
      this._historyPoll.start();
    },

    stopDashboardRefresh() {
      this._dashboardActive = false;
      if (this._mainPoll) this._mainPoll.stop();
      if (this._historyPoll) this._historyPoll.stop();
    },

    historySignalPoints() {
      const points = this.historyPoints;
      if (points.length < 2) return '';
      const width = 600;
      const height = 120;
      const pad = 6;
      const step = (width - pad * 2) / (points.length - 1);
      const coords = points.map((point, index) => {
        const value = Math.max(0, Math.min(100, Number(point.sig) || 0));
        const x = pad + index * step;
        const y = height - pad - (value / 100) * (height - pad * 2);
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      });
      return coords.join(' ');
    },

    historyLatestSignal() {
      const points = this.historyPoints;
      if (!points.length) return '-';
      const value = Number(points[points.length - 1].sig) || 0;
      return `${value}%`;
    },

    historyLatestRxRate() {
      const points = this.historyPoints;
      if (!points.length) return '-';
      return this.humanBytesPerSec(Number(points[points.length - 1].rxr) || 0);
    },

    historyLatestTxRate() {
      const points = this.historyPoints;
      if (!points.length) return '-';
      return this.humanBytesPerSec(Number(points[points.length - 1].txr) || 0);
    },

    historyTimeRange() {
      const points = this.historyPoints;
      if (points.length < 2) return '-';
      const fmt = (ts) => {
        const date = new Date(ts * 1000);
        return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
      };
      return `${fmt(points[0].t)} - ${fmt(points[points.length - 1].t)}`;
    },

    applyDashboardData(data) {
      this.dataPending = !!data.pending;
      this.dataError = (!data.pending && data.error) ? String(data.error) : '';
      if (data.pending === true) return;
      if (this.dataError) return;
      const textKeys = [
        'sim', 'temperature', 'active_sim', 'network_provider', 'mccmnc', 'apn',
        'network_mode', 'ipv4', 'ipv6', 'bands', 'bandwidth', 'prxqrsrp',
        'drxqrsrp', 'rx2qrsrp', 'rx3qrsrp', 'earfcns', 'pcc_pci', 'scc_pci',
        'signalAssessment', 'csq', 'rssi', 'cellID', 'eNBID', 'tac',
        'rsrqLTE', 'rsrqNR', 'rsrpLTE', 'rsrpNR', 'sinrLTE', 'sinrNR',
        'internetConnection', 'lastUpdate', 'nr_rx_human', 'nr_tx_human',
        'ramUsedHuman', 'ramTotalHuman'
      ];

      textKeys.forEach((key) => {
        if (Object.prototype.hasOwnProperty.call(data, key)) {
          this[key] = data[key];
        }
      });

      const percentKeys = [
        'rsrqLTEPercentage', 'rsrqNRPercentage', 'rsrpLTEPercentage',
        'rsrpNRPercentage', 'sinrLTEPercentage', 'sinrNRPercentage',
        'signalPercentage', 'cpuUsagePercent', 'ramUsagePercent'
      ];
      percentKeys.forEach((key) => {
        if (Object.prototype.hasOwnProperty.call(data, key)) {
          this[key] = Number(data[key]) || 0;
        }
      });

      this.updateTraffic(data);
      this.lastUpdate = data.lastUpdate || new Date().toLocaleString();
      SimpleAdmin.UI.setText('#dashboardLastUpdate', this.lastUpdate);
      this.dashboardLoaded = true;
    },

    updateTraffic(data) {
      const rx = Number(data.nr_rx_bytes);
      const tx = Number(data.nr_tx_bytes);
      if (!Number.isFinite(rx) || !Number.isFinite(tx)) return;

      const now = Date.now();
      if (this._prev_nr_rx !== null && this._prev_nr_tx !== null && this._prev_nr_t !== null) {
        const dt = Math.max(0.001, (now - this._prev_nr_t) / 1000);
        const dRx = rx >= this._prev_nr_rx ? (rx - this._prev_nr_rx) : 0;
        const dTx = tx >= this._prev_nr_tx ? (tx - this._prev_nr_tx) : 0;
        this.nr_dl_speed = data.nr_dl_speed && data.nr_dl_speed !== '-' ? data.nr_dl_speed : this.humanBytesPerSec(dRx / dt);
        this.nr_ul_speed = data.nr_ul_speed && data.nr_ul_speed !== '-' ? data.nr_ul_speed : this.humanBytesPerSec(dTx / dt);
      } else {
        this.nr_dl_speed = data.nr_dl_speed || '-';
        this.nr_ul_speed = data.nr_ul_speed || '-';
      }

      this.nr_rx_bytes = rx;
      this.nr_tx_bytes = tx;
      if (!data.nr_rx_human) this.nr_rx_human = this.humanBytes(rx);
      if (!data.nr_tx_human) this.nr_tx_human = this.humanBytes(tx);
      this._prev_nr_rx = rx;
      this._prev_nr_tx = tx;
      this._prev_nr_t = now;
    },

    humanBytes(bytes) {
      const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
      let value = Number(bytes);
      if (!Number.isFinite(value) || value < 0) return '-';
      let i = 0;
      while (value >= 1024 && i < units.length - 1) {
        value /= 1024;
        i += 1;
      }
      const n = (i > 0 && value < 10) ? value.toFixed(1) : Math.round(value);
      return `${n} ${units[i]}`;
    },

    humanBytesPerSec(bytesPerSecond) {
      const value = this.humanBytes(bytesPerSecond);
      return value === '-' ? '-' : `${value}/s`;
    },

    t(key) {
      return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
    },

    parseUptimeParts(data) {
      const text = String(data || '');
      const days = text.match(/(\d+)\s+day/);
      const hours = text.match(/(\d+)\s+hour/);
      const minutes = text.match(/(\d+)\s+min/);
      const hm = text.match(/(\d+):(\d+),/);
      if (!days && !hours && !minutes && !hm) return null;
      return {
        days: days ? Number(days[1]) : 0,
        hours: hm ? Number(hm[1]) : (hours ? Number(hours[1]) : 0),
        minutes: hm ? Number(hm[2]) : (minutes ? Number(minutes[1]) : 0)
      };
    },

    formatUptimeUnit(value, unit, language) {
      const amount = Number(value);
      if (!Number.isFinite(amount) || amount < 0) return '';
      if (amount === 0 && unit !== '分钟') return '';
      if (language === 'en') {
        const unitMap = { 天: 'day', 小时: 'hour', 分钟: 'minute' };
        const label = unitMap[unit] || unit;
        return `${amount} ${label}${amount === 1 ? '' : 's'}`;
      }
      return `${amount} ${unit}`;
    },

    formatUptimeParts(parts) {
      if (!parts) return this.t('未知时间');
      const language = SimpleAdmin.Lang ? SimpleAdmin.Lang.getCurrentLanguage() : 'zh-CN';
      const items = [
        this.formatUptimeUnit(parts.days, '天', language),
        this.formatUptimeUnit(parts.hours, '小时', language),
        this.formatUptimeUnit(parts.minutes, '分钟', language)
      ].filter(Boolean);
      return items.length ? items.join(' ') : this.formatUptimeUnit(0, '分钟', language);
    },

    setUptimeParts(parts) {
      this._uptimeParts = parts;
      this.uptime = this.formatUptimeParts(parts);
    },

    readRefreshRateInput() {
      const input = document.getElementById('dashboardRefreshRateInput');
      if (input) return input.value;
      return this.newRefreshRate;
    },

    updateRefreshRate() {
      const raw = this.readRefreshRateInput();
      if (raw === null || String(raw).trim() === '') return;
      const value = Number(raw);
      if (!Number.isFinite(value)) return;
      this.refreshRate = Math.max(2, Math.min(60, value));
      this.newRefreshRate = null;
      try {
        localStorage.setItem('refreshRate', String(this.refreshRate));
      } catch (err) {
        // Ignore unavailable localStorage so the page can still work in restricted browsers.
      }
      const input = document.getElementById('dashboardRefreshRateInput');
      if (input) {
        input.value = '';
        input.placeholder = `${this.refreshRate}s`;
      }
      if (this._dashboardActive && this.isDashboardPageActive()) {
        if (this._mainPoll) this._mainPoll.stop();
        this._mainPoll = this.createMainPoll(false);
        this._mainPoll.start();
      }
    },

    bindTitlebarControls() {
      const input = document.getElementById('dashboardRefreshRateInput');
      const applyButton = document.getElementById('dashboardRefreshRateApply');
      if (input) {
        input.placeholder = `${this.refreshRate}s`;
        input.addEventListener('keydown', (event) => {
          if (event.key === 'Enter') {
            event.preventDefault();
            this.updateRefreshRate();
          }
        });
      }
      if (applyButton) {
        applyButton.addEventListener('click', () => this.updateRefreshRate());
      }
      SimpleAdmin.UI.setText('#dashboardLastUpdate', this.lastUpdate);
    },

    init() {
      let stored = NaN;
      try {
        stored = Number(localStorage.getItem('refreshRate'));
      } catch (err) {
        // Ignore unavailable localStorage so the page can still work in restricted browsers.
      }
      if (Number.isFinite(stored) && stored >= 2) {
        // 与 updateRefreshRate 的保存钳制保持一致,防止手改 localStorage
        // 留下超范围值导致轮询间隔异常。
        this.refreshRate = Math.max(2, Math.min(60, stored));
      }
      this.bindTitlebarControls();
      window.addEventListener('simpleadmin:language-changed', () => {
        this.uptime = this._uptimeParts ? this.formatUptimeParts(this._uptimeParts) : this.t('未知时间');
      });
      SimpleAdmin.Poll.onPageReturn('dashboard', () => this.startDashboardRefresh());
      this._dashboardPageChangeHandler = (event) => {
        if (!(event && event.detail && event.detail.page === 'dashboard')) {
          this.stopDashboardRefresh();
        }
      };
      if (window.SimpleAdminSpaMode) {
        window.addEventListener('simpleadmin:page-changed', this._dashboardPageChangeHandler);
      }
      this._atCacheUpdatedHandler = () => {
        if (!this._dashboardActive || !this.isDashboardPageActive()) return;
        if (this._pushRefreshTimer) return;
        this._pushRefreshTimer = setTimeout(() => {
          this._pushRefreshTimer = null;
          this.fetchNetworkInfo();
        }, 800);
      };
      window.addEventListener('simpleadmin:at_cache_updated', this._atCacheUpdatedHandler);
      this.startDashboardRefresh();
      window.addEventListener('beforeunload', () => {
        this.stopDashboardRefresh();
        window.removeEventListener('simpleadmin:at_cache_updated', this._atCacheUpdatedHandler);
        if (this._pushRefreshTimer) {
          clearTimeout(this._pushRefreshTimer);
          this._pushRefreshTimer = null;
        }
      }, { once: true });
    }
  };
}

if (window.SimpleAdminSpaMode) {
  (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).dashboard = getStaticNetworkInfo;
} else {
  mountSimpleAdminVueApp(getStaticNetworkInfo);
}
