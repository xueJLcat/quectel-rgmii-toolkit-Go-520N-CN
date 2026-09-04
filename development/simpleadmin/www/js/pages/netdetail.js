function simpleNetDetail() {
  return {
    detailLoaded: false,
    detailFailed: false,
    wanIPv4: '-',
    wanIPv6: '-',
    lanGateway: '-',
    interfaces: [],
    clients: [],

    t(key) {
      return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
    },

    isPageActive() {
      if (!window.SimpleAdminSpaMode) return true;
      const section = document.querySelector('.sa-page[data-page="netdetail"]');
      return !!(section && section.classList.contains('active'));
    },

    fetchDetail() {
      if (!SimpleAdmin.Api.getNetworkDetail) return Promise.resolve();
      return SimpleAdmin.Api.getNetworkDetail()
        .then((data) => {
          if (!this.isPageActive()) return;
          if (data && data.pending === true) return;
          this.wanIPv4 = (data && data.wanIPv4) || '-';
          this.wanIPv6 = (data && data.wanIPv6) || '-';
          this.lanGateway = (data && data.lanGateway) || '-';
          this.interfaces = (data && data.interfaces) || [];
          this.clients = (data && data.clients) || [];
          this.detailLoaded = true;
          this.detailFailed = false;
        })
        .catch((error) => {
          if (!this.isPageActive()) return;
          console.error('获取网络详情失败：', error);
          this.detailFailed = true;
        });
    },

    retryDetail() {
      this.detailFailed = false;
      this.fetchDetail();
    },

    formatBytes(value) {
      const bytes = Number(value) || 0;
      if (bytes <= 0) return '0 B';
      const units = ['B', 'KB', 'MB', 'GB', 'TB'];
      let size = bytes;
      let index = 0;
      while (size >= 1024 && index < units.length - 1) {
        size /= 1024;
        index += 1;
      }
      return size.toFixed(index === 0 ? 0 : 2) + ' ' + units[index];
    },

    formatRate(value) {
      return this.formatBytes(value) + '/s';
    },

    formatLease(seconds) {
      const value = Number(seconds);
      if (!Number.isFinite(value) || value < 0) return '—';
      const days = Math.floor(value / 86400);
      const hours = Math.floor((value % 86400) / 3600);
      const minutes = Math.floor((value % 3600) / 60);
      if (days > 0) return days + ' ' + this.t('天') + (hours > 0 ? ' ' + hours + ' ' + this.t('小时') : '');
      if (hours > 0) return hours + ' ' + this.t('小时') + (minutes > 0 ? ' ' + minutes + ' ' + this.t('分钟') : '');
      if (minutes > 0) return minutes + ' ' + this.t('分钟');
      return this.t('不足1分钟');
    },

    bindTitlebarControls() {
      if (this._titlebarBound) return;
      this._titlebarBound = true;
      const button = document.getElementById('netDetailRefreshButton');
      if (button) {
        button.addEventListener('click', () => this.retryDetail());
      }
    },

    init() {
      if (!this._pageReturnBound && SimpleAdmin.Poll) {
        this._pageReturnBound = true;
        SimpleAdmin.Poll.onPageReturn('netdetail', () => {
          if (!this._pageChangeReady) return;
          this.startPolling();
        });
      }
      this.bindTitlebarControls();
      this.fetchDetail();
      this.startPolling();
      setTimeout(() => { this._pageChangeReady = true; }, 0);
    },

    startPolling() {
      if (this._poll || !SimpleAdmin.Poll) return;
      this._poll = SimpleAdmin.Poll.create({
        tick: () => {
          if (!this.isPageActive()) {
            this.stopPolling();
            return;
          }
          return this.fetchDetail();
        },
        interval: 3000,
        immediate: false,
        pauseWhenHidden: true
      });
      this._poll.start();
    },

    stopPolling() {
      if (!this._poll) return;
      this._poll.stop();
      this._poll = null;
    },
  };
}

if (window.SimpleAdminSpaMode) {
  (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).netdetail = simpleNetDetail;
} else {
  mountSimpleAdminVueApp(simpleNetDetail);
}
