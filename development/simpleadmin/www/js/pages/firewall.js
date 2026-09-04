function simpleFirewall() {
  return {
    serverRules: [],
    rules: [],
    newPort: "",
    newAction: "block",
    chains: [],
    ruleCount: 0,
    jumpInstalled: false,
    isLoading: false,
    applying: false,
    statusLoaded: false,
    loadFailed: false,
    dmzFailed: false,
    dmzMode: "0",
    dmzIP: "",

    computed: {
      sortedRules() {
        return (this.rules || []).slice().sort((a, b) => Number(a.port) - Number(b.port));
      },
    },

    t(key) {
      return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
    },

    fetchStatus() {
      this.isLoading = true;
      SimpleAdmin.Api.firewallData({ action: 'status' })
        .then((data) => {
          const rules = (data && Array.isArray(data.rules))
            ? data.rules.map((rule) => ({
                port: String((rule && rule.port) || ''),
                action: (rule && rule.action) === 'accept' ? 'accept' : 'block',
              }))
            : [];
          // 必须在更新 serverRules 之前判断：hasChanges() 比较 rules 与 serverRules，
          // 若先同步 serverRules 会污染比较基准，导致首次加载也被误判为"有未应用更改"。
          const hasUnappliedChanges = this.hasChanges();
          // serverRules 始终同步为服务端真实状态（保存成功后依赖它消除"未应用更改"提示）。
          this.serverRules = JSON.parse(JSON.stringify(rules));
          // 存在未应用更改（用户增删改规则后未点"应用更改"）时，刷新（统计面板刷新按钮、
          // onPageReturn）不能用服务端规则覆盖本地编辑区，否则会静默冲掉暂存；
          // 此时只更新统计与状态字段。首次加载规则未编辑过，hasUnappliedChanges 为假，正常填充。
          if (!hasUnappliedChanges) {
            this.rules = JSON.parse(JSON.stringify(rules));
          }
          this.chains = (data && Array.isArray(data.chains)) ? data.chains : [];
          this.ruleCount = (data && data.ruleCount) || 0;
          this.jumpInstalled = !!(data && data.jumpInstalled);
          this.loadFailed = false;
        })
        .catch((error) => {
          console.error("获取防火墙状态失败：", error);
          this.loadFailed = true;
        })
        .finally(() => {
          this.isLoading = false;
        });
    },

    retryStatus() {
      this.loadFailed = false;
      this.fetchStatus();
    },

    fetchDmzStatus(retry) {
      this._dmzSeq = (this._dmzSeq || 0) + 1;
      const seq = this._dmzSeq;
      if (this._dmzRetryTimer) {
        clearTimeout(this._dmzRetryTimer);
        this._dmzRetryTimer = null;
      }
      const wasLoaded = this.statusLoaded;
      this.statusLoaded = false;
      this.dmzFailed = false;
      const attempt = retry || 0;
      SimpleAdmin.Api.networkConfigData({ action: 'status', force: '1' })
        .then((data) => {
          if (seq !== this._dmzSeq) return;
          const scheduleRetry = () => {
            if (attempt < 8) {
              this._dmzRetryTimer = setTimeout(() => {
                this._dmzRetryTimer = null;
                if (seq !== this._dmzSeq) return;
                this.fetchDmzStatus(attempt + 1);
              }, 1500 * Math.min(attempt + 1, 4));
            } else {
              this.dmzFailed = true;
            }
          };
          if (data && data.pending === true) {
            scheduleRetry();
            return;
          }
          const dmzIpValue = String((data && data.dmzIP) || '');
          const lanGwValue = String((data && data.lanGwIp) || '');
          const untrusted = data &&
            data.ipPassStatus === false &&
            data.DNSV4ProxyStatus === false &&
            data.DNSV6ProxyStatus === false &&
            data.currentUsbNetMode === '未知' &&
            dmzIpValue === '' &&
            lanGwValue === '';
          if (untrusted) {
            if (wasLoaded) {
              this.statusLoaded = true;
              return;
            }
            scheduleRetry();
            return;
          }
          const currentDmzIp = String((data && data.dmzIP) || '').trim();
          this.dmzIP = currentDmzIp;
          this.dmzMode = this.resolveDmzMode(data && data.dmzMode, currentDmzIp);
          this.statusLoaded = true;
          this.dmzFailed = false;
        })
        .catch((error) => {
          if (seq !== this._dmzSeq) return;
          console.error("错误: ", error);
          this.dmzFailed = true;
          SimpleAdmin.UI.notify('danger', this.t('错误:') + ' ' + ((error && error.message) || this.t('未知错误')));
        });
    },

    retryDmzStatus() {
      this.dmzFailed = false;
      this.fetchDmzStatus(0);
    },

    resolveDmzMode(mode, ip) {
      const currentMode = String(mode || '').trim();
      const currentIp = String(ip || '').trim();
      if (currentIp && currentIp !== '-') {
        return '1';
      }
      return currentMode === '1' ? '1' : '0';
    },

    validIPv4(ip) {
      const parts = String(ip || '').trim().split('.');
      if (parts.length !== 4) {
        return false;
      }
      return parts.every((part) => {
        if (!/^\d+$/.test(part)) {
          return false;
        }
        const num = parseInt(part, 10);
        return num >= 0 && num <= 255;
      });
    },

    setDMZEnable() {
      if (this.isLoading) return;
      const ip = String(this.dmzIP || '').trim();
      if (!ip || !this.validIPv4(ip)) {
        console.error("请输入有效的 IP 地址！");
        SimpleAdmin.UI.notify('danger', '请输入有效的 IP 地址！');
        return;
      }
      this.isLoading = true;
      SimpleAdmin.Api.networkConfigData({ action: 'dmz', enabled: '1', ip: ip })
        .then((data) => {
          if (data && data.ok === false) {
            throw new Error(data.error || this.t('操作失败'));
          }
          this.dmzMode = '1';
        })
        .catch((error) => {
          console.error("启用 DMZ 失败：", error);
          SimpleAdmin.UI.notify('danger', (error && error.message) || this.t('未知错误'));
        })
        .finally(() => {
          this.isLoading = false;
        });
    },

    setDMZDisable() {
      if (this.isLoading) return;
      this.isLoading = true;
      SimpleAdmin.Api.networkConfigData({ action: 'dmz', enabled: '0' })
        .then((data) => {
          if (data && data.ok === false) {
            throw new Error(data.error || this.t('操作失败'));
          }
          this.dmzMode = '0';
        })
        .catch((error) => {
          console.error("禁用 DMZ 失败：", error);
          SimpleAdmin.UI.notify('danger', (error && error.message) || this.t('未知错误'));
        })
        .finally(() => {
          this.isLoading = false;
        });
    },

    addRule() {
      const input = String(this.newPort || '').trim();
      if (!/^\d+$/.test(input)) {
        SimpleAdmin.UI.notify('danger', '端口必须是 1-65535 的数字');
        return;
      }
      const portNum = parseInt(input, 10);
      if (portNum < 1 || portNum > 65535) {
        SimpleAdmin.UI.notify('danger', '端口必须是 1-65535 的数字');
        return;
      }
      const port = String(portNum);
      if (this.rules.some((rule) => rule.port === port)) {
        SimpleAdmin.UI.notify('danger', '该端口已在列表中');
        return;
      }
      this.rules.push({ port: port, action: this.newAction });
      this.newPort = "";
    },

    removeRule(rule) {
      if (!rule) return;
      SimpleAdmin.UI.confirm({
        title: '删除',
        message: '确定删除该规则？',
        confirmText: '删除',
        danger: true,
        onConfirm: () => {
          this.rules = this.rules.filter((item) => item.port !== rule.port);
        }
      });
    },

    clearRules() {
      if (this.rules.length === 0) return;
      SimpleAdmin.UI.confirm({
        title: '清空',
        message: '确定清空全部规则？',
        confirmText: '清空',
        danger: true,
        onConfirm: () => {
          this.rules = [];
        }
      });
    },

    hasChanges() {
      const normalize = (list) => (list || []).slice().sort((a, b) => Number(a.port) - Number(b.port));
      return JSON.stringify(normalize(this.rules)) !== JSON.stringify(normalize(this.serverRules));
    },

    saveRules() {
      if (this.applying) return;
      this.applying = true;
      const blockPorts = this.rules.filter((rule) => rule.action !== 'accept').map((rule) => rule.port);
      const acceptPorts = this.rules.filter((rule) => rule.action === 'accept').map((rule) => rule.port);
      SimpleAdmin.Api.firewallData({ action: 'save', block_ports: blockPorts.join(','), accept_ports: acceptPorts.join(',') })
        .then((data) => {
          if (data && data.ok === false) {
            throw new Error(data.error || this.t('应用失败'));
          }
          SimpleAdmin.UI.notify('success', '已应用');
          this.fetchStatus();
        })
        .catch((error) => {
          console.error("应用防火墙规则失败：", error);
          SimpleAdmin.UI.notify('danger', (error && error.message) || this.t('应用失败'));
        })
        .finally(() => {
          this.applying = false;
        });
    },

    formatBytes(n) {
      const value = Number(n);
      if (!Number.isFinite(value)) {
        return '-';
      }
      if (value < 1024) {
        return value + ' B';
      }
      const units = ['K', 'M', 'G'];
      let scaled = value;
      for (let i = 0; i < units.length; i++) {
        scaled = scaled / 1024;
        if (scaled < 1024 || i === units.length - 1) {
          return scaled.toFixed(1) + ' ' + units[i] + 'B';
        }
      }
    },

    formatCount(n) {
      const value = Number(n);
      if (!Number.isFinite(value)) {
        return '-';
      }
      return String(value).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
    },

    actionLabel(action) {
      return action === 'accept' ? this.t('放行') : this.t('阻止');
    },

    init() {
      this.fetchStatus();
      this.fetchDmzStatus();
      if (!this._pageReturnBound) {
        this._pageReturnBound = true;
        SimpleAdmin.Poll.onPageReturn('firewall', () => {
          if (this._pageChangeReady) {
            this.fetchStatus();
            this.fetchDmzStatus();
          }
        });
      }
      setTimeout(() => { this._pageChangeReady = true; }, 0);
    },
  };
}

if (window.SimpleAdminSpaMode) {
  (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).firewall = simpleFirewall;
} else {
  mountSimpleAdminVueApp(simpleFirewall);
}
