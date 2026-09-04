function simpleAutomation() {
  return {
    watchdogEnabled: false,
    watchdogThreshold: 5,
    watchdogCooldown: 30,
    watchdogInterval: 3,
    watchdogTargets: '',
    watchdogActionPolicy: 'reboot',
    watchdogLoaded: false,
    watchdogLoadFailed: false,
    isSavingWatchdog: false,
    watchdogFailures: 0,
    watchdogLastActionTime: '',
    watchdogLastActionType: '',
    watchdogPollerRunning: false,
    schedulerEnabled: false,
    schedulerTime: '04:00',
    schedulerLoaded: false,
    schedulerLoadFailed: false,
    isSavingScheduler: false,
    timeSyncEnabled: false,
    timeSyncInterval: 60,
    timeSyncServer: 'ntp.aliyun.com',
    timeSyncLoaded: false,
    timeSyncLoadFailed: false,
    isSavingTimeSync: false,
    isSyncingTime: false,
    timeSyncPollerRunning: false,
    timeSyncLastTime: '',
    timeSyncLastOK: null,
    timeSyncLastError: '',
    timeSyncLastOffsetMs: 0,
    timeSyncSystemTime: '',
    webhookEnabled: false,
    webhookURL: '',
    webhookLoaded: false,
    webhookLoadFailed: false,
    isSavingWebhook: false,

    t(key) {
      return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
    },

    notify(type, text) {
      if (SimpleAdmin.UI && typeof SimpleAdmin.UI.notify === 'function') {
        SimpleAdmin.UI.notify(type, text);
      }
    },

    fetchWatchdog() {
      if (!SimpleAdmin.Api.getWatchdog) return Promise.resolve(false);
      return SimpleAdmin.Api.getWatchdog()
        .then((data) => {
          this.watchdogEnabled = !!(data && data.enabled);
          if (data && Number(data.failThreshold) > 0) this.watchdogThreshold = Number(data.failThreshold);
          if (data && Number(data.cooldownMinutes) > 0) this.watchdogCooldown = Number(data.cooldownMinutes);
          if (data && Number(data.checkIntervalMinutes) > 0) this.watchdogInterval = Number(data.checkIntervalMinutes);
          this.watchdogTargets = ((data && data.targets) || []).join('\n');
          this.watchdogActionPolicy = data && data.actionPolicy === 'escalate' ? 'escalate' : 'reboot';
          this.watchdogFailures = Number((data && data.consecutiveFailures) || 0);
          this.watchdogLastActionTime = (data && data.lastActionTime) || '';
          this.watchdogLastActionType = (data && data.lastActionType) || '';
          this.watchdogPollerRunning = !!(data && data.pollerRunning);
          this.watchdogLoaded = true;
          this.watchdogLoadFailed = false;
          return true;
        })
        .catch(() => {
          this.watchdogLoaded = false;
          this.watchdogLoadFailed = true;
          return false;
        });
    },

    retryWatchdog() {
      this.watchdogLoadFailed = false;
      this.fetchWatchdog();
    },

    saveWatchdog() {
      if (this.isSavingWatchdog || !this.watchdogLoaded) return;
      const threshold = Number(this.watchdogThreshold);
      if (!Number.isInteger(threshold) || threshold < 1 || threshold > 60) {
        this.notify('danger', '连续失败次数必须是 1-60 的整数');
        return;
      }
      const cooldown = Number(this.watchdogCooldown);
      if (!Number.isInteger(cooldown) || cooldown < 1 || cooldown > 1440) {
        this.notify('danger', '冷却分钟必须是 1-1440 的整数');
        return;
      }
      const interval = Number(this.watchdogInterval);
      if (!Number.isInteger(interval) || interval < 1 || interval > 30) {
        this.notify('danger', '检测间隔必须是 1-30 的整数');
        return;
      }
      this.isSavingWatchdog = true;
      SimpleAdmin.Api.setWatchdog({
        enabled: this.watchdogEnabled ? '1' : '0',
        failThreshold: String(threshold),
        cooldownMinutes: String(cooldown),
        checkInterval: String(interval),
        targets: String(this.watchdogTargets || ''),
        actionPolicy: this.watchdogActionPolicy
      })
        .then((data) => {
          if (data && data.ok === false) throw new Error(data.error || '');
          this.notify('success', '已保存');
        })
        .catch((error) => {
          console.error('保存看门狗设置失败：', error);
          this.notify('danger', this.t('保存失败'));
        })
        .finally(() => {
          this.isSavingWatchdog = false;
          this.fetchWatchdog();
        });
    },

    lastActionText() {
      if (!this.watchdogLastActionTime) return this.t('从未');
      let typeLabel = '';
      if (this.watchdogLastActionType === 'radio') typeLabel = this.t('无线电重注册');
      else if (this.watchdogLastActionType === 'reboot') typeLabel = this.t('重启模块');
      return typeLabel ? this.watchdogLastActionTime + ' · ' + typeLabel : this.watchdogLastActionTime;
    },

    fetchScheduler() {
      if (!SimpleAdmin.Api.getScheduler) return Promise.resolve(false);
      return SimpleAdmin.Api.getScheduler()
        .then((data) => {
          this.schedulerEnabled = !!(data && data.rebootEnabled);
          if (data && data.rebootTime) this.schedulerTime = data.rebootTime;
          this.schedulerLoaded = true;
          this.schedulerLoadFailed = false;
          return true;
        })
        .catch(() => {
          this.schedulerLoaded = false;
          this.schedulerLoadFailed = true;
          return false;
        });
    },

    retryScheduler() {
      this.schedulerLoadFailed = false;
      this.fetchScheduler();
    },

    saveScheduler() {
      if (this.isSavingScheduler || !this.schedulerLoaded) return;
      const rebootTime = String(this.schedulerTime || '').trim();
      if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(rebootTime)) {
        this.notify('danger', '重启时刻格式无效，应为 HH:MM');
        return;
      }
      this.isSavingScheduler = true;
      SimpleAdmin.Api.setScheduler({
        rebootEnabled: this.schedulerEnabled ? '1' : '0',
        rebootTime: rebootTime
      })
        .then((data) => {
          if (data && data.ok === false) throw new Error(data.error || '');
          this.notify('success', '已保存');
        })
        .catch((error) => {
          console.error('保存定时重启失败：', error);
          this.notify('danger', this.t('保存失败'));
        })
        .finally(() => {
          this.isSavingScheduler = false;
          this.fetchScheduler();
        });
    },

    fetchTimeSync() {
      if (!SimpleAdmin.Api.getTimeSync) return Promise.resolve(false);
      return SimpleAdmin.Api.getTimeSync()
        .then((data) => {
          this.timeSyncEnabled = !!(data && data.enabled);
          if (data && Number(data.intervalMinutes) > 0) this.timeSyncInterval = Number(data.intervalMinutes);
          if (data && data.server) this.timeSyncServer = data.server;
          this.timeSyncPollerRunning = !!(data && data.pollerRunning);
          this.timeSyncLastTime = (data && data.lastSyncTime) || '';
          this.timeSyncLastOK = data && data.lastSyncTime ? !!(data && data.lastSyncOK) : null;
          this.timeSyncLastError = (data && data.lastError) || '';
          this.timeSyncLastOffsetMs = Number((data && data.lastOffsetMs) || 0);
          this.timeSyncSystemTime = (data && data.systemTime) || '';
          this.timeSyncLoaded = true;
          this.timeSyncLoadFailed = false;
          return true;
        })
        .catch(() => {
          this.timeSyncLoaded = false;
          this.timeSyncLoadFailed = true;
          return false;
        });
    },

    retryTimeSync() {
      this.timeSyncLoadFailed = false;
      this.fetchTimeSync();
    },

    // 镜像后端 normalizeTimeSyncServer 的规则做前置校验:非法服务器后端会
    // 静默回退缺省源并返回成功,前端若不拦会显示"已保存"但值被偷换。
    // 空值合法(后端用缺省源);允许 ntp://、ntps:// 前缀;
    // 主体不得含空白与 / # ? : 等字符,长度不超过 253。
    validNtpServer(value) {
      let server = String(value || '').trim();
      if (server === '') return true;
      server = server.replace(/^ntps?:\/\//, '');
      if (server.length === 0 || server.length > 253) return false;
      return !/[ \t/#?:]/.test(server);
    },

    saveTimeSync() {
      if (this.isSavingTimeSync || !this.timeSyncLoaded) return;
      const interval = Number(this.timeSyncInterval);
      if (!Number.isInteger(interval) || interval < 1 || interval > 1440) {
        this.notify('danger', this.t('同步间隔必须是 1-1440 的整数'));
        return;
      }
      const server = String(this.timeSyncServer || '').trim();
      if (!this.validNtpServer(server)) {
        this.notify('danger', this.t('NTP 服务器格式无效'));
        return;
      }
      this.isSavingTimeSync = true;
      SimpleAdmin.Api.setTimeSync({
        enabled: this.timeSyncEnabled ? '1' : '0',
        intervalMinutes: String(interval),
        server: server
      })
        .then((data) => {
          if (data && data.ok === false) throw new Error(data.error || '');
          this.notify('success', this.t('已保存'));
        })
        .catch((error) => {
          console.error('保存时间同步设置失败：', error);
          this.notify('danger', this.t('保存失败'));
        })
        .finally(() => {
          this.isSavingTimeSync = false;
          this.fetchTimeSync();
        });
    },

    syncTimeNow() {
      if (this.isSyncingTime || !SimpleAdmin.Api.timeSyncNow) return;
      this.isSyncingTime = true;
      SimpleAdmin.Api.timeSyncNow()
        .then((data) => {
          if (data && data.ok) {
            this.notify('success', this.t('时间同步成功') + '（' + this.t('偏差') + ' ' + Number(data.offsetMs || 0) + ' ms）');
          } else {
            this.notify('danger', this.t('时间同步失败') + (data && data.error ? '：' + data.error : ''));
          }
        })
        .catch((error) => {
          console.error('手动时间同步失败：', error);
          this.notify('danger', this.t('时间同步失败'));
        })
        .finally(() => {
          this.isSyncingTime = false;
          this.fetchTimeSync();
        });
    },

    lastTimeSyncText() {
      if (!this.timeSyncLastTime) return this.t('从未');
      let suffix = '';
      if (this.timeSyncLastOK === true) suffix = ' · ' + this.t('偏差') + ' ' + this.timeSyncLastOffsetMs + ' ms';
      else if (this.timeSyncLastOK === false) suffix = ' · ' + this.t('失败');
      return this.timeSyncLastTime + suffix;
    },

    fetchWebhookSetting() {
      if (!SimpleAdmin.Api.getSMSWebhook) return;
      SimpleAdmin.Api.getSMSWebhook()
        .then((data) => {
          this.webhookEnabled = !!(data && data.enabled);
          if (data && data.url) this.webhookURL = data.url;
          this.webhookLoaded = true;
          this.webhookLoadFailed = false;
        })
        .catch(() => {
          this.webhookLoaded = false;
          this.webhookLoadFailed = true;
        });
    },

    retryWebhook() {
      this.webhookLoadFailed = false;
      this.fetchWebhookSetting();
    },

    saveWebhook() {
      if (this.isSavingWebhook || !this.webhookLoaded) return;
      this.isSavingWebhook = true;
      SimpleAdmin.Api.setSMSWebhook({
        enabled: this.webhookEnabled ? '1' : '0',
        url: this.webhookURL
      })
        .then((data) => {
          if (data && data.ok === false) {
            throw new Error(data.error || this.t('操作失败'));
          }
          this.notify('success', '已保存');
        })
        .catch((error) => {
          console.error('保存短信转发设置失败：', error);
          this.notify('danger', (error && error.message) || this.t('保存失败'));
        })
        .finally(() => {
          this.isSavingWebhook = false;
          this.fetchWebhookSetting();
        });
    },

    init() {
      if (!this._pageReturnBound && SimpleAdmin.Poll) {
        this._pageReturnBound = true;
        SimpleAdmin.Poll.onPageReturn('automation', () => {
          if (!this._pageChangeReady) return;
          this.fetchWatchdog();
          this.fetchScheduler();
          this.fetchTimeSync();
          this.fetchWebhookSetting();
        });
      }

      this.fetchWatchdog();
      this.fetchScheduler();
      this.fetchTimeSync();
      this.fetchWebhookSetting();

      setTimeout(() => { this._pageChangeReady = true; }, 0);
    },
  };
}

if (window.SimpleAdminSpaMode) {
  (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).automation = simpleAutomation;
} else {
  mountSimpleAdminVueApp(simpleAutomation);
}
