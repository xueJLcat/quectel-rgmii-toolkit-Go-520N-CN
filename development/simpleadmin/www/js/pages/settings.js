function simpleSettings() {
      return {
        isLoading: false,
        imei: "-",
        newImei: "",
        language: SimpleAdmin.Lang ? SimpleAdmin.Lang.getCurrentLanguage() : "zh-CN",
        isSavingLanguage: false,
        themeDefault: "light",
        isSavingTheme: false,
        currentPassword: "",
        newPassword: "",
        confirmPassword: "",
        isSavingPassword: false,
        showReloginButton: false,
        systemStatusFailed: false,
        ttldata: null,
        ttlvalue: 0,
        ttlStatus: null,
        ttlFailed: false,
        newTTL: null,
        isSavingTTL: false,

        t(key) {
          return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
        },

        notify(type, text) {
          if (SimpleAdmin.UI && typeof SimpleAdmin.UI.notify === 'function') {
            SimpleAdmin.UI.notify(type, text);
          }
        },

        fetchLanguageSetting() {
          if (!SimpleAdmin.Lang) return Promise.resolve(this.language);
          return SimpleAdmin.Lang.load().then((language) => {
            this.language = language;
            return language;
          });
        },

        saveLanguageSetting() {
          if (!SimpleAdmin.Lang) return;
          this.isSavingLanguage = true;
          SimpleAdmin.Lang.setLanguage(this.language, { save: true })
            .then((result) => {
              const language = result && result.language ? result.language : result;
              this.language = language;
              if (result && result.localOnly) {
                this.notify('warning', '已本地生效，服务器保存失败');
              } else {
                this.notify('success', '已保存');
              }
            })
            .catch((error) => {
              console.error("保存语言设置失败：", error);
              this.notify('danger', '保存失败');
            })
            .finally(() => {
              this.isSavingLanguage = false;
            });
        },

        normalizeTheme(theme) {
          return theme === 'dark' ? 'dark' : 'light';
        },

        fetchThemeSetting() {
          if (!SimpleAdmin.Api || typeof SimpleAdmin.Api.getTheme !== 'function') return;
          SimpleAdmin.Api.getTheme()
            .then((res) => res.json())
            .then((data) => {
              const theme = data && data.theme;
              if (theme === 'dark' || theme === 'light') {
                this.themeDefault = theme;
                return;
              }
              // 服务端无有效值时回退为当前浏览器正在使用的主题,避免显示假默认值
              this.themeDefault = SimpleAdmin.UI && typeof SimpleAdmin.UI.getTheme === 'function'
                ? SimpleAdmin.UI.getTheme()
                : 'light';
            })
            .catch((error) => {
              console.error("获取主题设置失败：", error);
              this.themeDefault = SimpleAdmin.UI && typeof SimpleAdmin.UI.getTheme === 'function'
                ? SimpleAdmin.UI.getTheme()
                : 'light';
            });
        },

        saveThemeSetting() {
          const theme = this.normalizeTheme(this.themeDefault);
          this.themeDefault = theme;
          // 先本地生效(当前浏览器立即切换并记住),再保存为设备默认值
          if (SimpleAdmin.UI && typeof SimpleAdmin.UI.setTheme === 'function') {
            SimpleAdmin.UI.setTheme(theme, { persist: true });
          }
          if (!SimpleAdmin.Api || typeof SimpleAdmin.Api.setTheme !== 'function') return;
          this.isSavingTheme = true;
          SimpleAdmin.Api.setTheme(theme)
            .then((res) => res.json())
            .then((data) => {
              if (!data || data.error) {
                throw new Error((data && data.error) || 'theme save failed');
              }
              this.themeDefault = this.normalizeTheme(data.theme);
              this.notify('success', '已保存');
            })
            .catch((error) => {
              console.error("保存主题设置失败：", error);
              this.notify('warning', '已本地生效，服务器保存失败');
            })
            .finally(() => {
              this.isSavingTheme = false;
            });
        },

        changeLoginPassword() {
          if (!this.currentPassword || !this.newPassword) {
            this.notify('warning', '请输入当前密码和新密码');
            return;
          }
          if (this.newPassword !== this.confirmPassword) {
            this.notify('warning', '两次输入的新密码不一致');
            return;
          }

          this.isSavingPassword = true;
          SimpleAdmin.Api.setPassword(this.currentPassword, this.newPassword, this.confirmPassword)
            .then((res) => {
              return res.json().catch(() => ({})).then((data) => ({ res, data }));
            })
            .then(({ res, data }) => {
              if (!res.ok) {
                const error = data && data.error ? data.error : "";
                if (res.status === 403 || error === "current password incorrect") {
                  throw new Error("当前密码不正确");
                }
                if (error === "new password is empty") {
                  throw new Error("新密码不能为空");
                }
                if (error === "new password must not contain line breaks") {
                  throw new Error("新密码不能包含换行符");
                }
                if (error === "new password is too long") {
                  throw new Error("新密码过长");
                }
                if (error === "password confirmation mismatch") {
                  throw new Error("密码确认不一致");
                }
                throw new Error(error || "密码保存失败");
              }
              this.currentPassword = "";
              this.newPassword = "";
              this.confirmPassword = "";
              this.notify('success', '密码已保存，请使用新密码重新登录。');
              this.showReloginButton = true;
            })
            .catch((error) => {
              console.error("保存登录密码失败：", error);
              this.notify('danger', error.message || '密码保存失败');
            })
            .finally(() => {
              this.isSavingPassword = false;
            });
        },

        reloginNow() {
          if (window.SimpleAdmin && SimpleAdmin.Logout && typeof SimpleAdmin.Logout.start === 'function') {
            SimpleAdmin.Logout.start();
          } else {
            window.location.href = '/api/logout';
          }
        },

        rebootDevice() {
          // AT命令重启:经 AT+CFUN=1,1 重启蜂窝模块,设备本身保持运行。
          if (!SimpleAdmin.Reboot) return;
          SimpleAdmin.Reboot.request({
            title: 'AT命令重启',
            message: '将通过 AT 指令（AT+CFUN=1,1）重启蜂窝模块，约需 40 秒恢复。继续吗?',
            confirmText: '重启',
            onDone: () => {
              setTimeout(() => { this.init(); }, 5000);
            }
          });
        },

        rebootSystem() {
          // 设备重启:执行系统 reboot 命令,二次确认后触发。
          if (!SimpleAdmin.UI) return;
          SimpleAdmin.UI.confirm({
            title: '设备重启',
            message: '这将重启整台设备，期间管理界面不可用，约需 60 秒恢复。继续吗?',
            danger: true,
            confirmText: '重启设备',
            onConfirm: () => this.startSystemReboot()
          });
        },

        startSystemReboot() {
          // 乐观倒计时:命令发出后设备随即开始停机,WS 中断属常态,
          // 仅当后端明确返回失败(命令无法启动)时取消倒计时。
          if (SimpleAdmin.Reboot) {
            SimpleAdmin.Reboot.countdown(60, () => {
              setTimeout(() => { this.init(); }, 5000);
            }, '设备重启中…请稍候,请勿关闭页面');
          }
          SimpleAdmin.Api.systemData({ action: 'reboot_device' })
            .then((data) => {
              if (data && data.ok === false && data.error) {
                if (SimpleAdmin.Reboot && typeof SimpleAdmin.Reboot.cancelCountdown === 'function') {
                  SimpleAdmin.Reboot.cancelCountdown();
                }
                this.notify('danger', data.error);
              }
            })
            .catch(() => { });
        },

        poweroffDevice() {
          // 关机:执行系统 poweroff 命令,二次确认后触发。
          if (!SimpleAdmin.UI) return;
          SimpleAdmin.UI.confirm({
            title: '关机',
            message: '这将使设备断电关机，关机后设备无法自行恢复，必须手动通电开机。继续吗?',
            danger: true,
            confirmText: '关机',
            onConfirm: () => this.startPoweroff()
          });
        },

        startPoweroff() {
          const showPoweroffNotice = () => {
            const overlay = document.getElementById('sa-poweroff-overlay');
            if (!overlay || !SimpleAdmin.UI || !SimpleAdmin.UI.modal) return;
            if (!this._poweroffOverlayBound) {
              this._poweroffOverlayBound = true;
              const okBtn = document.getElementById('saPoweroffOk');
              if (okBtn) okBtn.addEventListener('click', () => SimpleAdmin.UI.modal.close(overlay));
            }
            SimpleAdmin.UI.modal.open(overlay, { focus: document.getElementById('saPoweroffOk') });
          };
          const probeDeviceAlive = () => fetch('/login.html', { cache: 'no-store' })
            .then(() => true)
            .catch(() => false);
          const confirmPoweroffByProbe = () => {
            // 传输中断无法区分"设备正在断电"与"命令未送达":延时探测免会话的
            // 登录页,设备仍可达说明命令未送达(只提示错误),不可达才判定已关机。
            setTimeout(() => {
              probeDeviceAlive().then((alive) => {
                if (alive) {
                  this.notify('danger', this.t('关机命令未送达，设备仍在运行，请重试'));
                } else {
                  showPoweroffNotice();
                }
              });
            }, 6000);
          };
          const waitDevicePoweredOff = () => {
            // 关机命令送达≠已断电:断电是异步过程,设备还会继续运行数秒。
            // 轮询探测直到设备不可达再弹"已关机",避免设备仍在运行时误导用户;
            // 超时仍未断电也照常提示(设备随后会完成断电)。
            let attempts = 0;
            const maxAttempts = 8;
            const poll = () => {
              probeDeviceAlive().then((alive) => {
                if (!alive) {
                  showPoweroffNotice();
                  return;
                }
                attempts++;
                if (attempts >= maxAttempts) {
                  showPoweroffNotice();
                  return;
                }
                setTimeout(poll, 2000);
              });
            };
            setTimeout(poll, 2000);
          };
          SimpleAdmin.Api.systemData({ action: 'poweroff_device' })
            .then((data) => {
              if (data && data.ok === false) {
                this.notify('danger', data.error || this.t('操作失败'));
                return;
              }
              waitDevicePoweredOff();
            })
            .catch(() => {
              confirmPoweroffByProbe();
            });
        },

        openImeiModal() {
          const val = (this.newImei || '').trim();
          if (!val) {
            this.notify('danger', '没有提供新的 IMEI。');
            return;
          }
          if (val.length !== 15 || !/^\d+$/.test(val)) {
            this.notify('danger', 'IMEI 无效');
            return;
          }
          if (this.imei !== '-' && val === this.imei) {
            this.notify('danger', 'IMEI 与当前 IMEI 相同');
            return;
          }
          SimpleAdmin.UI.confirm({
            title: '这将修改 IMEI 并重启调制解调器。',
            message: '继续？',
            danger: true,
            confirmText: '确认并重启',
            onConfirm: () => this.updateIMEI()
          });
        },

        updateIMEI() {
          const val = (this.newImei || '').trim();
          this.isLoading = true;
          this._imeiRebootFailed = false;
          if (SimpleAdmin.Reboot) {
            SimpleAdmin.Reboot.countdown(40, () => {
              if (!this._imeiRebootFailed) this.init();
            });
          }
          SimpleAdmin.Api.systemData({ action: 'set_imei', imei: val })
            .then((data) => {
              if (data && data.ok === false) {
                this._imeiRebootFailed = true;
                // 修改失败时取消重启倒计时;方法由 simpleadmin-reboot.js 提供,先做存在性判断
                if (SimpleAdmin.Reboot && typeof SimpleAdmin.Reboot.cancelCountdown === 'function') {
                  SimpleAdmin.Reboot.cancelCountdown();
                }
                this.notify('danger', data.error || this.t('操作失败'));
              }
            })
            .catch((error) => {
              console.error('修改 IMEI 失败:', error);
              // 传输层失败(会话过期/网络中断/超时)时命令并未送达设备,
              // 必须取消乐观启动的重启倒计时,否则界面仍会等待 40s 并误触发 init()。
              this._imeiRebootFailed = true;
              if (SimpleAdmin.Reboot && typeof SimpleAdmin.Reboot.cancelCountdown === 'function') {
                SimpleAdmin.Reboot.cancelCountdown();
              }
              this.notify('danger', this.t('操作失败'));
            })
            .finally(() => {
              this.isLoading = false;
            });
        },

        fetchSystemStatus(retry) {
          const attempt = retry || 0;
          if (attempt === 0) {
            this._systemStatusSeq = (this._systemStatusSeq || 0) + 1;
            if (this._systemStatusTimer) {
              clearTimeout(this._systemStatusTimer);
              this._systemStatusTimer = null;
            }
          }
          const seq = this._systemStatusSeq;
          SimpleAdmin.Api.systemData({ action: 'status', force: '1' })
            .then((data) => {
              if (seq !== this._systemStatusSeq) return;
              if (data && data.pending === true) {
                if (attempt < 8) {
                  this._systemStatusTimer = setTimeout(() => {
                    this._systemStatusTimer = null;
                    this.fetchSystemStatus(attempt + 1);
                  }, 1500 * Math.min(attempt + 1, 4));
                } else {
                  this.systemStatusFailed = true;
                }
                return;
              }
              if (data && data.error) {
                // AT 读取失败:给出可见失败态与重试入口,不静默保留旧 IMEI。
                this.systemStatusFailed = true;
                return;
              }
              this.systemStatusFailed = false;
              const oldImei = this.imei;
              const currentImei = (data.imei || '').trim();
              if (/^\d{14,17}$/.test(currentImei)) {
                this.imei = currentImei;
                if (!this.newImei || this.newImei === '-' || this.newImei === oldImei) {
                  this.newImei = currentImei;
                }
              }
            })
            .catch((error) => {
              if (seq !== this._systemStatusSeq) return;
              console.error("错误: ", error);
              this.systemStatusFailed = true;
              this.notify('danger', this.t('错误:') + ' ' + ((error && error.message) || this.t('未知错误')));
            });
        },

        retrySystemStatus() {
          this.systemStatusFailed = false;
          this.fetchSystemStatus(0);
        },

        fetchTTL() {
          SimpleAdmin.Api.getTTLStatus()
            .then((res) => {
              return res.json();
            })
            .then((data) => {
              if (!data) throw new Error('empty TTL response');
              this.ttldata = data;
              this.ttlStatus = !!data.isEnabled;
              this.ttlvalue = data.ttl;
              this.ttlFailed = false;
            })
            .catch((error) => {
              console.error("Error fetching TTL status: ", error);
              this.ttlFailed = true;
            });
        },

        retryTTL() {
          this.ttlFailed = false;
          this.fetchTTL();
        },

        setTTL() {
          if (this.isSavingTTL) return;
          // 严格数字校验:parseInt 会把 "0x10" 解析成 0(=禁用 TTL)、
          // "12abc" 解析成 12,静默放行会造成与输入不符的生效值。
          const rawTTL = String(this.newTTL === null || this.newTTL === undefined ? '' : this.newTTL).trim();
          if (!/^\d{1,3}$/.test(rawTTL)) {
            this.notify('danger', 'TTL 值必须是 0-255 的整数');
            return;
          }
          const ttlValueWithoutLeadingZero = Number(rawTTL);

          if (ttlValueWithoutLeadingZero < 0 || ttlValueWithoutLeadingZero > 255) {
            this.notify('danger', 'TTL 值必须是 0-255 的整数');
            return;
          }

          const ttlval = ttlValueWithoutLeadingZero;
          this.isSavingTTL = true;

          SimpleAdmin.Api.setTTL(ttlval)
            .then((res) => {
              return res.json().catch(() => ({}));
            })
            .then((data) => {
              // 后端对非法取值/规则应用失败会返回 ok:false,
              // 不能一律显示"已保存"误导用户。
              if (!data || data.ok === false) {
                throw new Error((data && data.error) || '');
              }
              this.notify('success', '已保存');
              this.fetchTTL();
            })
            .catch((error) => {
              console.error("Error setting TTL: ", error);
              this.notify('danger', '数据更新失败');
              this.fetchTTL();
            })
            .finally(() => {
              this.isSavingTTL = false;
            });
        },

        init() {
          if (!this._pageReturnBound && SimpleAdmin.Poll) {
            this._pageReturnBound = true;
            SimpleAdmin.Poll.onPageReturn('settings', () => {
              if (!this._pageChangeReady) return;
              this.fetchSystemStatus();
              this.fetchTTL();
            });
          }

          this.fetchLanguageSetting();
          this.fetchThemeSetting();
          this.fetchSystemStatus();
          this.fetchTTL();

          setTimeout(() => { this._pageChangeReady = true; }, 0);
        },
      };
    }

    if (window.SimpleAdminSpaMode) {
      (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).settings = simpleSettings;
    } else {
      mountSimpleAdminVueApp(simpleSettings);
    }
