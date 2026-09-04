function simpleNetConfig() {
  return {
    isLoading: false,
    statusLoaded: false,
    statusFailed: false,
    ipPassMode: "未指定",
    ipPassStatus: false,
    usbNetMode: "未指定",
    currentUsbNetMode: "未知",
    usbNetPending: false,
    DNSV4ProxyStatus: false,
    DNSV6ProxyStatus: false,
    // 本固件不支持查询 DNS V4 代理状态(AT+QMAP="DHCPV4DNS" 恒返回 ERROR),
    // 后端以 dnsV4QueryFailed 标记;未知态显示"未知",不得回退成"已启用/未启用"误导用户。
    dnsV4Unknown: false,
    lanIpStart: "",
    lanIpEnd: "",
    lanGwIp: "",
    isSavingLANIP: false,
    lanIpSaveSuccess: false,
    lanIpSaveSuccessTimer: null,
    macBindings: [],
    macBindingsLoaded: false,
    macBindFailed: false,
    newBindMac: '',
    newBindIp: '',
    isSavingBind: false,
    dnsUpstreamEnabled: false,
    dnsUpstreamServers: '',
    dnsUpstreamLoaded: false,
    dnsUpstreamFailed: false,
    isSavingDns: false,

    watch: {
      // LAN IP 草稿检测:任一字段变化即尝试置脏,
      // 程序填充与首次加载由 markLanIpDirty 内部过滤
      lanGwIp(value) { this.markLanIpDirty('lanGwIp', value); },
      lanIpStart(value) { this.markLanIpDirty('lanIpStart', value); },
      lanIpEnd(value) { this.markLanIpDirty('lanIpEnd', value); }
    },

    t(key) {
      return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
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

    validMAC(mac) {
      // 与后端 normalizeMACAddress 保持一致:接受连字符分隔与 12 位紧凑式,
      // 统一归一为冒号分隔后再校验,避免把后端本可接受的格式判为无效。
      let value = String(mac || '').trim().replace(/-/g, ':');
      if (/^[0-9A-Fa-f]{12}$/.test(value)) {
        value = value.match(/.{2}/g).join(':');
      }
      return /^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$/.test(value);
    },

    validIPv6(ip) {
      const value = String(ip || '').trim();
      if (!value.includes(':')) {
        return false;
      }
      if (!/^[0-9A-Fa-f:]+$/.test(value)) {
        return false;
      }
      if (value.includes(':::')) {
        return false;
      }
      const compressedCount = (value.match(/::/g) || []).length;
      if (compressedCount > 1) {
        return false;
      }
      const groups = value.split(':').filter((group) => group !== '');
      if (groups.length < 1 || groups.length > 8) {
        return false;
      }
      if (compressedCount === 0 && groups.length !== 8) {
        return false;
      }
      if (compressedCount === 1 && groups.length > 7) {
        return false;
      }
      return groups.every((group) => group.length <= 4);
    },

    fetchStatus(retry) {
      this._statusSeq = (this._statusSeq || 0) + 1;
      const seq = this._statusSeq;
      if (this._statusRetryTimer) {
        clearTimeout(this._statusRetryTimer);
        this._statusRetryTimer = null;
      }
      const wasLoaded = this.statusLoaded;
      this.statusLoaded = false;
      this.statusFailed = false;
      const attempt = retry || 0;
      SimpleAdmin.Api.networkConfigData({ action: 'status', force: '1' })
        .then((data) => {
          if (seq !== this._statusSeq) return;
          const scheduleRetry = () => {
            if (attempt < 8) {
              this._statusRetryTimer = setTimeout(() => {
                this._statusRetryTimer = null;
                if (seq !== this._statusSeq) return;
                this.fetchStatus(attempt + 1);
              }, 1500 * Math.min(attempt + 1, 4));
            } else {
              this.statusFailed = true;
            }
          };
          // 后端读取失败会返回 error 字段，语义同保护期，走既有重试
          if (data && (data.pending === true || data.error)) {
            scheduleRetry();
            return;
          }
          const dmzIpValue = String((data && data.dmzIP) || '');
          const lanGwValue = String((data && data.lanGwIp) || '');
          // DNS V4 查询失败(固件不支持)时 V4=false 是预期值,不作为"未加载"证据,
          // 否则会把部分成功的响应误判为全默认而无限重试。
          const dnsV4LooksDefault = data && data.dnsV4QueryFailed === true ? true : data.DNSV4ProxyStatus === false;
          const untrusted = data &&
            data.ipPassStatus === false &&
            dnsV4LooksDefault &&
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
          this.ipPassStatus = !!data.ipPassStatus;
          this.DNSV4ProxyStatus = !!data.DNSV4ProxyStatus;
          this.DNSV6ProxyStatus = !!data.DNSV6ProxyStatus;
          this.dnsV4Unknown = !!data.dnsV4QueryFailed;
          this.currentUsbNetMode = data.currentUsbNetMode || '未知';
          // 用户对网关/起始/结束有未保存草稿时保留输入,不被重取的服务端值覆盖;
          // 干净时正常填充并记录服务端值,供 markLanIpDirty 区分程序填充与用户输入
          if (!this._lanIpDirty) {
            this.lanIpStart = data.lanIpStart || '';
            this.lanIpEnd = data.lanIpEnd || '';
            this.lanGwIp = data.lanGwIp || '';
            this._lanIpServerValues = {
              lanIpStart: this.lanIpStart,
              lanIpEnd: this.lanIpEnd,
              lanGwIp: this.lanGwIp
            };
          }
          this.statusLoaded = true;
          this.statusFailed = false;
          if (this._usbNetClearPending) {
            this._usbNetClearPending = false;
            this.usbNetPending = false;
          }
        })
        .catch((error) => {
          if (seq !== this._statusSeq) return;
          console.error("错误: ", error);
          this.statusFailed = true;
          SimpleAdmin.UI.notify('danger', this.t('错误:') + ' ' + ((error && error.message) || this.t('未知错误')));
        });
    },

    retryStatus() {
      this.statusFailed = false;
      this.fetchStatus(0);
    },

    ipPassThroughEnable() {
      if (this.isLoading) return;
      if (this.ipPassMode != "未指定") {
        this.isLoading = true;
        SimpleAdmin.Api.networkConfigData({ action: 'ip_passthrough', enabled: '1', mode: this.ipPassMode })
          .then((data) => {
            if (data && data.ok === false) {
              throw new Error(data.error || this.t('操作失败'));
            }
            this.fetchStatus();
          })
          .catch((error) => {
            console.error("启用 IP 透传失败：", error);
            SimpleAdmin.UI.notify('danger', (error && error.message) || this.t('未知错误'));
          })
          .finally(() => {
            this.isLoading = false;
          });
      } else {
        console.error("未指定 IP 透传模式");
        SimpleAdmin.UI.notify('danger', '未指定 IP 透传模式');
      }
    },

    ipPassThroughDisable() {
      if (this.isLoading) return;
      this.isLoading = true;
      SimpleAdmin.UI.notify('info', '正在禁用 IP 透传，网口会重启，请等待倒计时结束。');
      const onDone = () => {
        this.refreshAfterReboot();
      };
      this._rebootActive = true;
      SimpleAdmin.Reboot.countdown(40, onDone);
      SimpleAdmin.Api.networkConfigData({ action: 'ip_passthrough', enabled: '0' })
        .then((data) => {
          if (data && (data.reboot || data.rebooting)) {
            if (this._rebootActive) {
              SimpleAdmin.Reboot.countdown(Number(data.rebootCountdownSeconds) || 40, onDone);
            }
            return;
          }
          if (data && data.ok === false) {
            SimpleAdmin.UI.notify('danger', data.error || this.t('操作失败'));
            // 兜底:模块明确拒绝时停止重启倒计时。正常失败由
            // ip_passthrough_result 事件广播处理(见 init),但前端自身也应保证,
            // 否则倒计时照常走满会误弹"重启结束"成功提示。
            if (SimpleAdmin.Reboot && typeof SimpleAdmin.Reboot.cancelCountdown === 'function') {
              SimpleAdmin.Reboot.cancelCountdown();
            }
            this._rebootActive = false;
          }
        })
        .catch((error) => {
          console.info("禁用 IP 透传期间网口/WebSocket 断开，继续保持重启倒计时：", error);
        })
        .finally(() => {
          this.isLoading = false;
        });
    },

    onDnsV6Toggle(event) {
      // 立即把开关弹回权威状态,等 fetchStatus 回读确认后才真正翻动,
      // 避免慢速 AT 命令期间开关显示虚假状态
      const enable = event.target.checked;
      event.target.checked = this.DNSV6ProxyStatus;
      this.changeDNSProxy('6', enable ? '1' : '0');
    },

    changeDNSProxy(family, enabled) {
      if (this.isLoading) return;
      this.isLoading = true;
      SimpleAdmin.Api.networkConfigData({ action: 'dns_proxy', family: family, enabled: enabled })
        .then((data) => {
          if (data && data.ok === false) {
            throw new Error(data.error || this.t('操作失败'));
          }
          // 固件不接受 DNS V4 状态回读,切换后界面仍显示"未知",
          // 用 toast 明确告知指令已被模块接受,避免用户以为没生效。
          SimpleAdmin.UI.notify('success', this.t('DNS 代理设置已下发'));
          this.fetchStatus();
        })
        .catch((error) => {
          console.error("DNS 代理设置失败：", error);
          SimpleAdmin.UI.notify('danger', (error && error.message) || this.t('未知错误'));
        })
        .finally(() => {
          this.isLoading = false;
        });
    },

    async usbNetModeChanger() {
      if (this.isLoading) return;
      if (this.usbNetMode === "未指定") {
        console.error("未指定 USB 网络模式");
        SimpleAdmin.UI.notify('danger', '未指定 USB 网络模式');
        return;
      }

      const map = { RMNET: 0, ECM: 1, MBIM: 2, RNDIS: 3 };
      const code = map[this.usbNetMode];
      if (code === undefined) {
        console.warn("USB 网络模式无效");
        SimpleAdmin.UI.notify('danger', 'USB 网络模式无效');
        return;
      }

      this.isLoading = true;
      try {
        const data = await SimpleAdmin.Api.networkConfigData({ action: 'usbnet', mode: this.usbNetMode });
        if (data && data.ok === false) {
          throw new Error(data.error || this.t('操作失败'));
        }
        this.fetchStatus();
        this.usbNetPending = true;
        SimpleAdmin.Reboot.request({
          onDone: () => {
            this._usbNetClearPending = true;
            this.refreshAfterReboot();
          }
        });
      } catch (e) {
        console.error("设置 usbnet 失败：", e);
        SimpleAdmin.UI.notify('danger', (e && e.message) || this.t('未知错误'));
      } finally {
        this.isLoading = false;
      }
    },

    markLanIpDirty(field, value) {
      // 首次成功填充前(_lanIpServerValues 未生成)不置脏;
      // watch 回调异步触发,程序填充赋的正是服务端值,
      // 用值比对区分"程序填充"与"用户输入",避免首次填充被误判为脏
      const serverValues = this._lanIpServerValues;
      if (!serverValues || serverValues[field] === value) return;
      this._lanIpDirty = true;
    },

    setLANIP() {
      this.lanIpSaveSuccess = false;
      if (this.lanIpSaveSuccessTimer) {
        clearTimeout(this.lanIpSaveSuccessTimer);
        this.lanIpSaveSuccessTimer = null;
      }

      if (!this.lanGwIp || !this.lanIpStart || !this.lanIpEnd) {
        console.error("请输入有效的网关 IP 地址和起始、结束 IP 地址！");
        SimpleAdmin.UI.notify('danger', '请输入有效的网关 IP 地址和起始、结束 IP 地址！');
        return;
      }

      if (!this.validIPv4(this.lanGwIp)) {
        console.error("网关 IP 地址格式无效！");
        SimpleAdmin.UI.notify('danger', '网关 IP 地址格式无效！');
        return;
      }

      const startNum = Number(String(this.lanIpStart).trim());
      const endNum = Number(String(this.lanIpEnd).trim());
      if (!Number.isInteger(startNum) || startNum < 1 || startNum > 254 ||
          !Number.isInteger(endNum) || endNum < 1 || endNum > 254) {
        console.error("起始/结束地址必须是 1-254 的整数！");
        SimpleAdmin.UI.notify('danger', '起始/结束地址必须是 1-254 的整数！');
        return;
      }

      if (startNum > endNum) {
        console.error("起始地址不能大于结束地址！");
        SimpleAdmin.UI.notify('danger', '起始地址不能大于结束地址');
        return;
      }

      const gwLastOctet = Number(this.lanGwIp.split('.')[3]);
      if (gwLastOctet < 1 || gwLastOctet > 254) {
        console.error("网关 IP 地址末段必须在 1-254 之间！");
        SimpleAdmin.UI.notify('danger', '网关 IP 地址末段必须在 1-254 之间');
        return;
      }

      if (gwLastOctet >= startNum && gwLastOctet <= endNum) {
        console.error("DHCP 地址池不能包含网关自身地址！");
        SimpleAdmin.UI.notify('danger', 'DHCP 地址池不能包含网关地址');
        return;
      }

      const gwIpParts = this.lanGwIp.split('.');

      // 用校验后的数值拼接,避免 " 100"/"1e2" 等通过 Number() 校验的输入
      // 原样拼进 IP 被后端拒绝(报不直观的 "invalid lan ip")。
      const startIp = `${gwIpParts[0]}.${gwIpParts[1]}.${gwIpParts[2]}.${startNum}`;
      const endIp = `${gwIpParts[0]}.${gwIpParts[1]}.${gwIpParts[2]}.${endNum}`;

      this.isSavingLANIP = true;

      return SimpleAdmin.Api.networkConfigData({ action: 'lanip', start: startIp, end: endIp, gateway: this.lanGwIp })
        .then((data) => {
          if (data && data.ok === false) {
            throw new Error(data.error || this.t('操作失败'));
          }
          this.lanIpSaveSuccess = true;
          this.lanIpSaveSuccessTimer = setTimeout(() => {
            this.lanIpSaveSuccess = false;
            this.lanIpSaveSuccessTimer = null;
          }, 3000);
          // 保存成功后草稿即成为服务端值,复位脏标志允许后续重取正常覆盖
          this._lanIpDirty = false;
          this.fetchStatus();
        })
        .catch((error) => {
          console.error("LAN IP 设置失败：", error);
          SimpleAdmin.UI.notify('danger', (error && error.message) || this.t('未知错误'));
        })
        .finally(() => {
          this.isSavingLANIP = false;
        });
    },

    fetchMacBindings() {
      this._bindSeq = (this._bindSeq || 0) + 1;
      const seq = this._bindSeq;
      this.macBindFailed = false;
      SimpleAdmin.Api.networkConfigData({ action: 'mac_bind_list' })
        .then((data) => {
          if (seq !== this._bindSeq) return;
          // 后端在 AT 查询失败/未就绪时返回 ok:false(附 atReady:false),
          // 此时不得把空列表当真实状态渲染,改为失败态供用户重试。
          if (!data || data.ok === false || data.atReady === false) {
            this.macBindFailed = true;
            return;
          }
          this.macBindings = (data && data.bindings) || [];
          this.macBindingsLoaded = true;
        })
        .catch((error) => {
          if (seq !== this._bindSeq) return;
          console.error("获取静态绑定失败：", error);
          this.macBindFailed = true;
        });
    },

    addMacBinding() {
      if (this.isSavingBind) return;
      const mac = String(this.newBindMac || '').trim();
      const ip = String(this.newBindIp || '').trim();
      if (!this.validMAC(mac)) {
        console.error("MAC 地址格式无效");
        SimpleAdmin.UI.notify('danger', this.t('MAC 地址格式无效'));
        return;
      }
      if (!this.validIPv4(ip)) {
        console.error("IP 地址格式无效");
        SimpleAdmin.UI.notify('danger', this.t('IP 地址格式无效'));
        return;
      }
      // 后端会把 MAC 归一化为大写,本地查重同样忽略大小写,避免同一地址重复提交。
      const macKey = mac.toUpperCase();
      if (this.macBindings.some((entry) => entry && String(entry.mac || '').toUpperCase() === macKey)) {
        SimpleAdmin.UI.notify('danger', this.t('该 MAC 地址已绑定'));
        return;
      }
      if (this.macBindings.some((entry) => entry && entry.ip === ip)) {
        SimpleAdmin.UI.notify('danger', this.t('该 IP 地址已绑定'));
        return;
      }
      if (this.macBindings.length >= 10) {
        console.error("最多添加 10 条静态绑定");
        SimpleAdmin.UI.notify('danger', this.t('最多添加 10 条静态绑定'));
        return;
      }
      this.isSavingBind = true;
      return SimpleAdmin.Api.networkConfigData({ action: 'mac_bind_set', mac: mac, ip: ip })
        .then((data) => {
          if (data && data.ok === false) {
            throw new Error(data.error || this.t('操作失败'));
          }
          SimpleAdmin.UI.notify('success', this.t('已添加静态绑定'));
          this.newBindMac = '';
          this.newBindIp = '';
          this.fetchMacBindings();
        })
        .catch((error) => {
          console.error("添加静态绑定失败：", error);
          SimpleAdmin.UI.notify('danger', (error && error.message) || this.t('未知错误'));
        })
        .finally(() => {
          this.isSavingBind = false;
        });
    },

    removeMacBinding(entry) {
      if (!entry || this.isSavingBind) return;
      SimpleAdmin.UI.confirm({
        title: '删除静态绑定',
        message: '删除后该设备将恢复动态分配 IP。',
        danger: true,
        onConfirm: () => {
          this.isSavingBind = true;
          SimpleAdmin.Api.networkConfigData({ action: 'mac_bind_del', mac: entry.mac })
            .then((data) => {
              if (data && data.ok === false) {
                throw new Error(data.error || this.t('操作失败'));
              }
              SimpleAdmin.UI.notify('success', this.t('已删除静态绑定'));
              this.fetchMacBindings();
            })
            .catch((error) => {
              console.error("删除静态绑定失败：", error);
              SimpleAdmin.UI.notify('danger', (error && error.message) || this.t('未知错误'));
            })
            .finally(() => {
              this.isSavingBind = false;
            });
        }
      });
    },

    fetchDNSUpstream() {
      this._dnsSeq = (this._dnsSeq || 0) + 1;
      const seq = this._dnsSeq;
      this.dnsUpstreamFailed = false;
      SimpleAdmin.Api.networkConfigData({ action: 'dns_upstream' })
        .then((data) => {
          if (seq !== this._dnsSeq) return;
          this.dnsUpstreamEnabled = !!(data && data.enabled);
          this.dnsUpstreamServers = ((data && data.servers) || []).join('\n');
          this.dnsUpstreamLoaded = true;
        })
        .catch((error) => {
          if (seq !== this._dnsSeq) return;
          console.error("获取上游 DNS 失败：", error);
          this.dnsUpstreamFailed = true;
        });
    },

    parseDNSServers() {
      const seen = {};
      const servers = [];
      String(this.dnsUpstreamServers || '').split(/[\s,]+/).forEach((raw) => {
        const value = raw.trim();
        if (!value) return;
        const dedupeKey = value.toLowerCase();
        if (seen[dedupeKey]) return;
        seen[dedupeKey] = true;
        servers.push(value);
      });
      return servers;
    },

    saveDNSUpstream() {
      if (this.isSavingDns) return;
      const servers = this.parseDNSServers();
      if (servers.length === 0) {
        console.error("请至少填写一个 DNS 服务器");
        SimpleAdmin.UI.notify('danger', this.t('请至少填写一个 DNS 服务器'));
        return;
      }
      for (let i = 0; i < servers.length; i += 1) {
        if (!this.validIPv4(servers[i]) && !this.validIPv6(servers[i])) {
          console.error(`第 ${i + 1} 行 DNS 服务器格式无效`);
          SimpleAdmin.UI.notify('danger', this.t(`第 ${i + 1} 行 DNS 服务器格式无效`));
          return;
        }
      }
      if (servers.length > 4) {
        console.error("最多配置 4 个上游 DNS 服务器");
        SimpleAdmin.UI.notify('danger', this.t('最多配置 4 个上游 DNS 服务器'));
        return;
      }
      SimpleAdmin.UI.confirm({
        title: '自定义上游 DNS',
        message: '保存时将短暂重启本机 DNS 服务（约 1 秒），确定保存？',
        onConfirm: () => {
          this.isSavingDns = true;
          SimpleAdmin.Api.networkConfigData({ action: 'dns_upstream_set', enabled: '1', servers: servers.join(',') })
            .then((data) => {
              if (data && data.ok === false) {
                throw new Error(data.error || this.t('操作失败'));
              }
              SimpleAdmin.UI.notify('success', this.t('自定义上游 DNS 已保存'));
              this.fetchDNSUpstream();
            })
            .catch((error) => {
              console.error("保存自定义上游 DNS 失败：", error);
              SimpleAdmin.UI.notify('danger', (error && error.message) || this.t('未知错误'));
            })
            .finally(() => {
              this.isSavingDns = false;
            });
        }
      });
    },

    disableDNSUpstream() {
      if (this.isSavingDns) return;
      SimpleAdmin.UI.confirm({
        title: '恢复运营商 DNS',
        message: '恢复后上游 DNS 将跟随运营商下发，确定恢复？',
        onConfirm: () => {
          this.isSavingDns = true;
          SimpleAdmin.Api.networkConfigData({ action: 'dns_upstream_set', enabled: '0' })
            .then((data) => {
              if (data && data.ok === false) {
                throw new Error(data.error || this.t('操作失败'));
              }
              SimpleAdmin.UI.notify('success', this.t('已恢复运营商 DNS'));
              this.fetchDNSUpstream();
            })
            .catch((error) => {
              console.error("恢复运营商 DNS 失败：", error);
              SimpleAdmin.UI.notify('danger', (error && error.message) || this.t('未知错误'));
            })
            .finally(() => {
              this.isSavingDns = false;
            });
        }
      });
    },

    refreshAfterReboot() {
      if (this._reinitTimer) {
        clearTimeout(this._reinitTimer);
        this._reinitTimer = null;
      }
      this._reinitTimer = setTimeout(() => {
        this._reinitTimer = null;
        this.fetchStatus();
      }, 5000);
    },

    dismissUsbPending() {
      this.usbNetPending = false;
    },

    init() {
      if (!this._ipPassResultBound) {
        this._ipPassResultBound = true;
        window.addEventListener('simpleadmin:ip_passthrough_result', (event) => {
          const detail = event && event.detail;
          if (detail && String(detail.ok) === 'false') {
            SimpleAdmin.UI.notify('danger', '操作失败，已取消后续步骤');
            // 禁用失败时停止重启倒计时;方法由 simpleadmin-reboot.js 提供,先做存在性判断
            if (SimpleAdmin.Reboot && typeof SimpleAdmin.Reboot.cancelCountdown === 'function') {
              SimpleAdmin.Reboot.cancelCountdown();
            }
            this._rebootActive = false;
          }
        });
      }
      if (!this._rebootDoneBound) {
        this._rebootDoneBound = true;
        window.addEventListener('simpleadmin:reboot-done', () => {
          this._rebootActive = false;
        });
      }
      if (!this._pageReturnBound) {
        this._pageReturnBound = true;
        SimpleAdmin.Poll.onPageReturn('netconfig', () => {
          if (this._pageChangeReady) {
            this.fetchStatus();
            this.fetchMacBindings();
            this.fetchDNSUpstream();
          }
        });
      }
      this.fetchStatus();
      this.fetchMacBindings();
      this.fetchDNSUpstream();
      setTimeout(() => { this._pageChangeReady = true; }, 0);
    },
  };
}

if (window.SimpleAdminSpaMode) {
  (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).netconfig = simpleNetConfig;
} else {
  mountSimpleAdminVueApp(simpleNetConfig);
}
