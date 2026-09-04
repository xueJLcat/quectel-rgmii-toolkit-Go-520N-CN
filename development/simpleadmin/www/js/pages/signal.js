function signalPage() {
  return {
    pending: false,
    loadFailed: false,
    rat: '-',
    updatedAt: '-',
    antennas: [],
    carriers: [],
    cellData: {},
    _poll: null,
    _pageChangeBound: false,

    t(key) {
      return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
    },

    notify(type, text) {
      if (SimpleAdmin.UI && typeof SimpleAdmin.UI.notify === 'function') {
        SimpleAdmin.UI.notify(type, text);
      }
    },

    isSignalPageActive() {
      const page = document.querySelector('.sa-page[data-page="signal"]');
      return !!page && page.classList.contains('active');
    },

    computed: {
      // 载波聚合状态:多载波即聚合生效,单车载明示无聚合。
      caStatus() {
        const count = this.carriers.length;
        if (count === 0) return '-';
        if (count === 1) return this.t('单载波(无聚合)');
        return this.t('载波聚合生效') + ` (${count} ${this.t('载波')})`;
      },

      // 服务小区表格行。注意必须放 computed:工厂里的普通函数会被
      // vue-app.js 归为 methods,模板 v-for="row in cellRows" 不加括号
      // 引用到的是函数本身,迭代不出任何行,表格整体空白。
      cellRows() {
        const cell = this.cellData || {};
        const rows = [
          ['网络制式', cell.network_mode],
          ['物理小区标识 (PCI)', cell.pcc_pci],
          ['SCC PCI', cell.scc_pci],
          ['频点', cell.earfcns],
          ['TAC', cell.tac],
          ['小区 ID', cell.cellID],
          ['eNB ID', cell.eNBID],
          ['RSRP (NR)', cell.rsrpNR],
          ['RSRQ (NR)', cell.rsrqNR],
          ['SINR (NR)', cell.sinrNR],
          ['RSRP (LTE)', cell.rsrpLTE],
          ['RSRQ (LTE)', cell.rsrqLTE],
          ['SINR (LTE)', cell.sinrLTE],
          ['RSSI', cell.rssi]
        ];
        return rows
          .filter(([key, value]) => {
            const empty = value === undefined || value === null || value === '-' || String(value).trim() === '';
            if (!empty) return true;
            // 网络制式/频点恒显,便于确认当前驻留状态;其余空值行隐藏。
            return key === '网络制式' || key === '频点';
          })
          .map(([key, value], index) => ({
            key: index,
            label: this.t(key),
            value: value === undefined || value === null || String(value).trim() === '' ? '-' : String(value)
          }));
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

    // 按信号质量着色仪表填充,与进度条阈值一致(>=60 优 / >=40 中 / 其余差)。
    gaugeColorClass(percent) {
      const value = this.clampPercent(percent);
      if (value >= 60) return 'sa-gauge-good';
      if (value >= 40) return 'sa-gauge-mid';
      return 'sa-gauge-bad';
    },

    carrierRoleText(role) {
      if (role === 'PCC') return this.t('主载波 (PCC)');
      if (role === 'SCC') return this.t('辅载波 (SCC)');
      return role || '-';
    },

    fetchSignal() {
      return SimpleAdmin.Api.signalData()
        .then((data) => {
          this.loadFailed = false;
          this.pending = !!(data && data.pending);
          if (this.pending || !data) return;
          this.rat = data.rat || '-';
          this.antennas = (data.antennas || []).map((antenna) => ({
            id: antenna.id || '',
            label: antenna.label || antenna.id || '-',
            rsrp: antenna.rsrp === undefined || antenna.rsrp === null ? '-' : antenna.rsrp,
            percent: Number(antenna.percent) || 0
          }));
          this.carriers = (data.carriers || []).map((carrier) => ({
            role: carrier.role || '-',
            band: carrier.band || '-',
            arfcn: carrier.arfcn || '-',
            bandwidth: carrier.bandwidth || '-',
            pci: carrier.pci || '-'
          }));
          this.cellData = data.cell || {};
          const lang = (window.SimpleAdmin && window.SimpleAdmin.Lang && typeof window.SimpleAdmin.Lang.getCurrentLanguage === 'function')
            ? window.SimpleAdmin.Lang.getCurrentLanguage()
            : 'zh-CN';
          const locale = String(lang || 'zh-CN').toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-GB';
          this.updatedAt = new Date().toLocaleTimeString(locale, { hour12: false });
        })
        .catch((error) => {
          console.error('信号详情读取失败', error);
          this.loadFailed = true;
        });
    },

    retryLoad() {
      this.loadFailed = false;
      this.fetchSignal();
    },

    startPolling() {
      if (!this.isSignalPageActive()) return;
      if (!this._poll) {
        this._poll = SimpleAdmin.Poll.create({
          tick: () => this.fetchSignal(),
          interval: 5000,
          immediate: true,
          pauseWhenHidden: true
        });
      }
      this._poll.start();
    },

    stopPolling() {
      if (this._poll) this._poll.stop();
    },

    init() {
      // 数据获取仅驻留在本页:页面激活才轮询,切走立即停止,
      // 与后端"仅页面请求才拉取"配合,降低模块常态 AT 压力。
      SimpleAdmin.Poll.onPageReturn('signal', () => this.startPolling());
      if (window.SimpleAdminSpaMode && !this._pageChangeBound) {
        this._pageChangeBound = true;
        window.addEventListener('simpleadmin:page-changed', (event) => {
          if (event && event.detail && event.detail.page === 'signal') {
            this.startPolling();
          } else {
            this.stopPolling();
          }
        });
      }
      this.startPolling();
    }
  };
}

if (window.SimpleAdminSpaMode) {
  (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).signal = signalPage;
} else {
  mountSimpleAdminVueApp(signalPage);
}
