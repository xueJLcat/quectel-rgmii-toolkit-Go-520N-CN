function fetchDeviceInfo() {
  const DEVICE_INFO_REFRESH_MS = 5000;

  return {
    manufacturer: "-",
    modelName: "-",
    firmwareVersion: "-",
    simStatus: "未知",
    imsi: "-",
    iccid: "-",
    imei: "-",
    lanIp: "-",
    wwanIpv4: "-",
    wwanIpv6: "-",
    phoneNumber: "-",

    isLoading: false,
    loadFailed: false,
    pending: false,

    _deviceInfoPoll: null,
    _loadingDeviceInfo: false,
    _didInitialFetch: false,
    _loadingCount: 0,
    _failCount: 0,
    _deviceInfoActive: false,
    _pageChangeHandler: null,

    isDeviceInfoPageActive() {
      if (!window.SimpleAdminSpaMode) return true;
      const section = document.querySelector('.sa-page[data-page="deviceinfo"]');
      return !!(section && section.classList.contains('active'));
    },

    startDeviceInfoRefresh() {
      if (!this.isDeviceInfoPageActive()) return;
      this._deviceInfoActive = true;
      if (!this._deviceInfoPoll) {
        this._deviceInfoPoll = SimpleAdmin.Poll.create({
          tick: () => this.fetchATCommand(),
          interval: DEVICE_INFO_REFRESH_MS,
          immediate: true,
          pauseWhenHidden: true
        });
      }
      this._deviceInfoPoll.start();
    },

    stopDeviceInfoRefresh() {
      this._deviceInfoActive = false;
      if (this._deviceInfoPoll) this._deviceInfoPoll.stop();
    },

    async fetchATCommand() {
      if (!this._deviceInfoActive || !this.isDeviceInfoPageActive()) {
        this.stopDeviceInfoRefresh();
        return;
      }
      if (this._loadingDeviceInfo) return;
      this._loadingDeviceInfo = true;
      this._loadingCount++;
      this.isLoading = true;
      try {
        const data = await SimpleAdmin.Api.getDeviceInfo({ force: false });
        Object.assign(this, {
          manufacturer: data.manufacturer || '-',
          modelName: data.modelName || '-',
          firmwareVersion: data.firmwareVersion || '-',
          simStatus: data.simStatus || '未知',
          imsi: data.imsi || '-',
          iccid: data.iccid || '-',
          imei: data.imei || '-',
          lanIp: data.lanIp || '-',
          wwanIpv4: data.wwanIpv4 || '-',
          wwanIpv6: data.wwanIpv6 || '-',
          phoneNumber: data.phoneNumber || '未插卡'
        });
        if (data && data.pending === true) {
          // 模块未就绪(开机/重启保护期):正常的"就绪中"分支,显示 pending 提示
          // 并按原间隔继续轮询。绝不能 throw——否则会被轮询器计为失败并触发
          // 指数退避(5s→40s),模块就绪后用户仍要久等,且无任何可见状态。
          this.pending = true;
          this._failCount = 0;
          this.loadFailed = false;
          return;
        }
        if (data && data.error) {
          // AT 读取失败:交由 catch 计失败,连续多次后给出可见失败横幅。
          throw new Error(data.error);
        }
        // 成功取回数据:清零失败计数,撤下失败/就绪中提示。
        this.pending = false;
        this._failCount = 0;
        this.loadFailed = false;
      } catch (err) {
        console.warn('设备信息自动刷新失败：', err);
        // 连续多次失败才判定为故障(单次失败可能只是瞬时抖动),
        // 给出可见的失败横幅与手动重试入口,避免页面永久静默显示旧数据。
        this.pending = false;
        this._failCount++;
        if (this._failCount >= 3) this.loadFailed = true;
        throw err;
      } finally {
        this._loadingCount--;
        this.isLoading = this._loadingCount > 0;
        this._loadingDeviceInfo = false;
      }
    },

    // 失败横幅上的手动重试:复位失败标志并立即重取一次。
    retryDeviceInfo() {
      this.loadFailed = false;
      this._failCount = 0;
      this.fetchATCommand().catch(() => {});
    },

    init() {
      if (this._didInitialFetch) return;
      this._didInitialFetch = true;
      SimpleAdmin.Poll.onPageReturn('deviceinfo', () => this.startDeviceInfoRefresh());
      this._pageChangeHandler = (event) => {
        if (!(event && event.detail && event.detail.page === 'deviceinfo')) {
          this.stopDeviceInfoRefresh();
        }
      };
      if (window.SimpleAdminSpaMode) {
        window.addEventListener('simpleadmin:page-changed', this._pageChangeHandler);
      }
      if (this.isDeviceInfoPageActive()) {
        this.startDeviceInfoRefresh();
      }
    },
  };
}

if (window.SimpleAdminSpaMode) {
  (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).deviceinfo = fetchDeviceInfo;
} else {
  mountSimpleAdminVueApp(fetchDeviceInfo);
}
