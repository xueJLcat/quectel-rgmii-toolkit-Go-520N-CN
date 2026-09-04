function networkPage() {
      return {
        isLoading: false,
        apn: "-",
        newApn: null,
        prefNetwork: "-",
        prefNetworkMode: null,
        lte_bands: null,
        nsa_bands: null,
        sa_bands: null,
        locked_lte_bands: null,
        locked_nsa_bands: null,
        locked_sa_bands: null,
        bands: "获取频段中...",
        isGettingBands: false,
        pdpType: "-",
        newPdpType: null,
        nrModeControlCurrent: null,
        nrModeControlNew: null,
        bandProfileName: null,
        bandProfiles: [],
        selectedBandProfile: null,
        model: "RG520N-CN", // 型号硬编码，不再动态识别
        bandGroups: [],
        settingsLoadFailed: false,
        bandsLoadFailed: false,
        _bandsFetchInFlight: {},
        _bandsRetryTimers: {},
        _bandsRetryCount: {},
        _bandsRetryGen: {},
        _initialized: false,
        _userTouchedBands: false,
        _currentSettingsFetchInFlight: null,
        _settingsPending: false,
        _settingsRetryCount: 0,
        _settingsRetryTimer: null,
        _pageReturnBound: false,
        _pageReady: false,

        // ---------- 通用工具 ----------
        sleep(ms) { return new Promise(r => setTimeout(r, ms)); },

        t(key) {
          return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
        },

        notify(type, text) {
          if (SimpleAdmin.UI && typeof SimpleAdmin.UI.notify === 'function') {
            SimpleAdmin.UI.notify(type, text);
          }
        },

        applyNetworkActionResult(result) {
          return !!(result && result.ok !== false);
        },

        reportNetworkActionFailure(result, label) {
          const message = this.t(label || '操作失败') + (result && result.error ? ': ' + result.error : '');
          this.notify('danger', message);
        },

        // ---------- 频段锁定（声明式复选框） ----------
        applyModelBands() {
          const bm = band_map.getBandsForModel(this.model, band_map.DEFAULTS);
          this.lte_bands = bm.lte;
          this.nsa_bands = bm.nsa;
          this.sa_bands = bm.sa;
        },

        rebuildBandGroups() {
          if (this._userTouchedBands) return;
          this.applyModelBands();
          this.bandGroups = SimpleAdmin.BandCheckboxes.build(this.model, {
            LTE: this.locked_lte_bands || '',
            NSA: this.locked_nsa_bands || '',
            SA: this.locked_sa_bands || ''
          });
        },

        bandModes() {
          return ['LTE', 'NSA', 'SA'];
        },

        getCheckedBands(mode) {
          const group = this.bandGroups.find(item => item.mode === mode);
          if (!group) return [];
          return group.bands.filter(band => band.checked).map(band => band.name);
        },

        bandToggleLabel(group) {
          if (!group || group.bands.length === 0) return this.t('全部选中');
          const allChecked = group.bands.every(band => band.checked);
          return allChecked ? this.t('取消选中') : this.t('全部选中');
        },

        toggleBandCheckboxes(mode) {
          const selectedMode = String(mode || 'LTE').toUpperCase();
          const group = this.bandGroups.find(item => item.mode === selectedMode);
          if (!group || group.bands.length === 0) return;
          const allChecked = group.bands.every(band => band.checked);
          this._userTouchedBands = true;
          group.bands.forEach(band => { band.checked = !allChecked; });
        },

        lockedBandsLoaded(mode) {
          if (mode === 'LTE') return this.locked_lte_bands !== null;
          if (mode === 'NSA') return this.locked_nsa_bands !== null;
          if (mode === 'SA') return this.locked_sa_bands !== null;
          return this.bandModes().every(m => this.lockedBandsLoaded(m));
        },

        setLockedBandsForMode(mode, data) {
          if (mode === 'LTE') {
            this.locked_lte_bands = data.locked_lte_bands || '';
          } else if (mode === 'NSA') {
            this.locked_nsa_bands = data.locked_nsa_bands || '';
          } else if (mode === 'SA') {
            this.locked_sa_bands = data.locked_sa_bands || '';
          }
        },

        setAllLockedBands(data) {
          this.locked_lte_bands = data.locked_lte_bands || '';
          this.locked_nsa_bands = data.locked_nsa_bands || '';
          this.locked_sa_bands = data.locked_sa_bands || '';
        },

        invalidateLockedBands(mode) {
          if (mode === 'LTE') {
            this.locked_lte_bands = null;
          } else if (mode === 'NSA') {
            this.locked_nsa_bands = null;
          } else if (mode === 'SA') {
            this.locked_sa_bands = null;
          }
        },

        scheduleBandsRetry(mode) {
          const retryMode = mode || 'ALL';
          const count = this._bandsRetryCount[retryMode] || 0;
          if (count >= 10) {
            // 重试耗尽:与 scheduleSettingsRetry 耗尽行为对齐,置位失败态,
            // 模板"频段信息读取失败"横幅经既有 retryLoadBands() 提供手动重试
            this.bandsLoadFailed = true;
            return;
          }
          this._bandsRetryCount[retryMode] = count + 1;
          if (this._bandsRetryTimers[retryMode]) {
            clearTimeout(this._bandsRetryTimers[retryMode]);
          }
          const gen = this._bandsRetryGen[retryMode] || 0;
          this._bandsRetryTimers[retryMode] = setTimeout(() => {
            delete this._bandsRetryTimers[retryMode];
            if (gen !== (this._bandsRetryGen[retryMode] || 0)) return;
            if (!this.lockedBandsLoaded(retryMode)) {
              this.getSupportedBands(true, retryMode, true);
            }
          }, 3000);
        },

        async getSupportedBands(force = false, mode = 'ALL', isRetry = false) {
          const selectedMode = String(mode || 'ALL').toUpperCase();

          if (!force && this.lockedBandsLoaded(selectedMode)) {
            this.rebuildBandGroups();
            return;
          }

          if (this._bandsFetchInFlight[selectedMode]) return this._bandsFetchInFlight[selectedMode];

          if (!isRetry) this._bandsRetryCount[selectedMode] = 0;
          this._bandsRetryGen[selectedMode] = (this._bandsRetryGen[selectedMode] || 0) + 1;
          if (this._bandsRetryTimers[selectedMode]) {
            clearTimeout(this._bandsRetryTimers[selectedMode]);
            delete this._bandsRetryTimers[selectedMode];
          }

          this.isGettingBands = true;

          const waitForResult = force ? '1' : '0';
          const payload = { action: 'bands', wait: waitForResult, force: force ? '1' : '0' };
          if (selectedMode !== 'ALL') payload.mode = selectedMode;

          this._bandsFetchInFlight[selectedMode] = SimpleAdmin.Api.networkData(payload)
            .then(data => {
              if (data.pending || data.error) {
                this.rebuildBandGroups();
                this.scheduleBandsRetry(selectedMode);
                return;
              }
              if (selectedMode === 'ALL') {
                this.setAllLockedBands(data);
              } else {
                this.setLockedBandsForMode(selectedMode, data);
              }
              this._bandsRetryCount[selectedMode] = 0;
              this.bandsLoadFailed = false;
              this.rebuildBandGroups();
            })
            .catch(error => {
              console.error('读取频段锁定失败:', error);
              this.bandsLoadFailed = true;
            })
            .finally(() => {
              delete this._bandsFetchInFlight[selectedMode];
              this.isGettingBands = Object.keys(this._bandsFetchInFlight).length > 0;
            });

          return this._bandsFetchInFlight[selectedMode];
        },

        retryLoadBands() {
          this.bandsLoadFailed = false;
          // 型号已硬编码为 RG520N-CN，无需再拉取型号，直接重试频段读取
          this.getSupportedBands(true, 'ALL');
        },

        async lockSelectedBandsForMode(mode) {
          const selectedMode = String(mode || '').toUpperCase();
          if (!this.bandModes().includes(selectedMode)) {
            this.notify('danger', '频段模式无效');
            return;
          }
          const newCheckedValues = this.getCheckedBands(selectedMode);
          if (newCheckedValues.length === 0) {
            this.notify('danger', '没有选中任何频段，请选择至少一个频段！');
            return;
          }
          const result = await SimpleAdmin.Api.networkData({ action: 'lock_bands', mode: selectedMode, values: newCheckedValues.join(':') })
            .catch(error => { console.error('发送频段锁定命令失败:', error); });
          if (!this.applyNetworkActionResult(result)) {
            this.reportNetworkActionFailure(result);
            return;
          }
          this.invalidateLockedBands(selectedMode);
          this.notify('info', '频段锁定已提交，正在刷新状态');
          await this.sleep(3000);
          await Promise.all([
            this.getSupportedBands(true, selectedMode),
            this.getCurrentSettings()
          ]);
        },

        resetBandLocking() {
          SimpleAdmin.UI.confirm({
            title: '恢复全部',
            message: '这将把频段锁定恢复为全部频段。继续？',
            danger: true,
            onConfirm: () => { this.doResetBandLocking(); }
          });
        },

        async doResetBandLocking() {
          const result = await SimpleAdmin.Api.networkData({ action: 'reset_bands', lte: this.lte_bands, nsa: this.nsa_bands, sa: this.sa_bands })
            .catch(error => { console.error('发送频段重置命令失败:', error); });
          if (!this.applyNetworkActionResult(result)) {
            this.reportNetworkActionFailure(result);
            return;
          }
          this.locked_lte_bands = null;
          this.locked_nsa_bands = null;
          this.locked_sa_bands = null;
          this._userTouchedBands = false;
          this.notify('info', '频段锁定已提交，正在刷新状态');
          await this.sleep(3000);
          await Promise.all([
            this.getSupportedBands(true, 'ALL'),
            this.getCurrentSettings()
          ]);
        },

        // ---------- 网络设置读取/保存 ----------
        scheduleSettingsRetry() {
          if (this._settingsRetryCount >= 10) {
            // 重试耗尽:标记失败,交由用户手动重试,不再自动轮询
            this.settingsLoadFailed = true;
            return;
          }
          this._settingsRetryCount += 1;
          if (this._settingsRetryTimer) clearTimeout(this._settingsRetryTimer);
          this._settingsRetryTimer = setTimeout(() => {
            this._settingsRetryTimer = null;
            if (this._settingsPending) {
              this.getCurrentSettings(true);
            }
          }, 3000);
        },

        async getCurrentSettings(isRetry = false) {
          if (this._currentSettingsFetchInFlight) return this._currentSettingsFetchInFlight;

          if (!isRetry) {
            this._settingsRetryCount = 0;
            if (this._settingsRetryTimer) {
              clearTimeout(this._settingsRetryTimer);
              this._settingsRetryTimer = null;
            }
          }

          this._currentSettingsFetchInFlight = SimpleAdmin.Api.networkData({ action: 'settings' })
            .then(settings => {
              if (settings && settings.pending === true) {
                this._settingsPending = true;
                this.scheduleSettingsRetry();
                return;
              }
              if (settings && settings.error) {
                // 200+error(AT 完全读不到)与请求失败(catch)同为可恢复故障,
                // 统一并入重试路径:避免一次性 toast 后 _settingsPending 复位、
                // 无重试按钮且 onPageReturn 不再重取,字段永久停在 "-" 的死态;
                // 重试耗尽后由 settingsLoadFailed 横幅提供手动重试
                this._settingsPending = true;
                this.scheduleSettingsRetry();
                return;
              }
              this._settingsPending = false;
              this._settingsRetryCount = 0;
              this.settingsLoadFailed = false;
              this.apn = settings.apn || '-';
              this.prefNetwork = settings.prefNetwork || '-';
              this.bands = settings.bands || '-';
              this.nrModeControlCurrent = settings.nrModeControlNum || null;
              this.nrModeControlNew = null;
              this.pdpType = settings.pdpType || '-';
            })
            .catch(error => {
              console.error("Error fetching network settings:", error);
              this._settingsPending = true;
              this.scheduleSettingsRetry();
            })
            .finally(() => {
              this._currentSettingsFetchInFlight = null;
            });
          return this._currentSettingsFetchInFlight;
        },

        retrySettings() {
          this.settingsLoadFailed = false;
          this.getCurrentSettings(0);
        },

        async saveChanges() {
          const currentApn = this.apn === '-' ? '' : String(this.apn);
          const newApn = this.newApn === null ? currentApn : String(this.newApn).trim();
          const newPdpType = this.newPdpType === null ? this.pdpType : this.newPdpType;
          const newModePref = this.prefNetworkMode;
          const newNrDisableMode = this.nrModeControlNew;

          const apnChanged = this.newApn !== null && newApn !== currentApn;
          const pdpChanged = this.newPdpType !== null && newPdpType !== this.pdpType;
          const modePrefChanged = newModePref !== null && newModePref !== this.prefNetwork;
          const nrDisableModeChanged = newNrDisableMode !== null && String(newNrDisableMode) !== String(this.nrModeControlCurrent);

          if (!apnChanged && !pdpChanged && !modePrefChanged && !nrDisableModeChanged) {
            this.notify('info', '没有做出更改');
            return;
          }

          const payload = { action: 'save_settings' };
          if (apnChanged || pdpChanged) {
            // 仅改 PDP 时一并提交当前 APN，避免后端丢失 APN 字段
            const apnValue = apnChanged ? newApn : this.apn;
            // 设置未就绪（APN 尚未加载）时拒绝提交，避免误把 PDP 改成默认值
            if (!apnValue || apnValue === '-') {
              this.notify('danger', this.t('网络设置未就绪，请稍后重试'));
              return;
            }
            let pdpType = pdpChanged ? newPdpType : this.pdpType;
            // 上面已拦截设置未就绪场景，这里仅兜底 PDP 为空的情况
            if (!pdpType || pdpType === '-') pdpType = 'IPV4V6';
            payload.pdpType = pdpType;
            payload.apn = apnValue;
          }
          if (modePrefChanged) {
            payload.modePref = newModePref;
          }
          if (nrDisableModeChanged) {
            payload.nrDisableMode = newNrDisableMode;
          }

          const result = await SimpleAdmin.Api.networkData(payload)
            .catch(error => { console.error('发送保存设置命令失败:', error); });
          if (!this.applyNetworkActionResult(result)) {
            this.reportNetworkActionFailure(result);
            return;
          }
          this.notify('success', '已保存');
          await this.sleep(3000);
          // 仅重读设置字段(本次保存的就是 APN/PDP/模式):整体 init() 会
          // 复位 _userTouchedBands 并按服务端状态重建频段复选框组,把用户
          // 尚未锁定的频段勾选静默丢弃。
          await this.getCurrentSettings();
        },

        // ---------- 锁频/锁小区配置档（浏览器本地 localStorage） ----------
        loadBandProfiles() {
          let profiles = [];
          try {
            const raw = localStorage.getItem('simpleadmin.bandProfiles');
            if (raw) {
              const parsed = JSON.parse(raw);
              if (Array.isArray(parsed)) profiles = parsed;
            }
          } catch (error) {
            console.warn('读取锁频/锁小区配置档失败:', error);
          }
          this.bandProfiles = profiles.filter(profile => profile && profile.name);
          if (this.selectedBandProfile !== null && !this.bandProfiles.some(profile => profile.name === this.selectedBandProfile)) {
            this.selectedBandProfile = null;
          }
        },

        persistBandProfiles() {
          try {
            localStorage.setItem('simpleadmin.bandProfiles', JSON.stringify(this.bandProfiles));
            return true;
          } catch (error) {
            console.warn('保存锁频/锁小区配置档失败:', error);
            this.notify('danger', '配置档保存失败');
            return false;
          }
        },

        collectBandProfileData() {
          return {
            bands: {
              LTE: this.getCheckedBands('LTE'),
              NSA: this.getCheckedBands('NSA'),
              SA: this.getCheckedBands('SA')
            },
            prefNetworkMode: this.prefNetworkMode,
            nrModeControlNew: this.nrModeControlNew
          };
        },

        saveBandProfile() {
          const name = String(this.bandProfileName || '').trim();
          if (!name) {
            this.notify('danger', '请输入档名');
            return;
          }
          if (this.bandProfiles.some(profile => profile.name === name)) {
            this.notify('danger', '已存在同名配置档');
            return;
          }
          this.bandProfiles.push({
            name: name,
            savedAt: new Date().toISOString(),
            data: this.collectBandProfileData()
          });
          if (!this.persistBandProfiles()) return;
          this.selectedBandProfile = name;
          this.notify('success', '配置档已保存');
        },

        applyBandProfile() {
          const profile = this.bandProfiles.find(item => item.name === this.selectedBandProfile);
          if (!profile || !profile.data) {
            this.notify('info', '请先选择配置档');
            return;
          }
          const data = profile.data;
          this.prefNetworkMode = data.prefNetworkMode !== undefined ? data.prefNetworkMode : null;
          this.nrModeControlNew = data.nrModeControlNew !== undefined ? data.nrModeControlNew : null;
          const bands = data.bands || {};
          this._userTouchedBands = true;
          this.bandGroups.forEach(group => {
            const values = Array.isArray(bands[group.mode]) ? bands[group.mode].map(String) : [];
            group.bands.forEach(band => { band.checked = values.includes(band.name); });
          });
          this.notify('info', '配置档已应用，仅填充界面，未自动提交');
        },

        deleteBandProfile() {
          const profile = this.bandProfiles.find(item => item.name === this.selectedBandProfile);
          if (!profile) {
            this.notify('info', '请先选择配置档');
            return;
          }
          SimpleAdmin.UI.confirm({
            title: '删除配置档',
            message: '确定删除该配置档？',
            danger: true,
            onConfirm: () => {
              this.bandProfiles = this.bandProfiles.filter(item => item.name !== profile.name);
              if (!this.persistBandProfiles()) return;
              this.selectedBandProfile = null;
              this.notify('success', '配置档已删除');
            }
          });
        },

        // ---------- 初始化 ----------
        registerPageReturn() {
          if (this._pageReturnBound) return;
          this._pageReturnBound = true;
          SimpleAdmin.Poll.onPageReturn('network', () => {
            if (!this._pageReady) return;
            if (this._settingsPending || !this.lockedBandsLoaded()) {
              this.getSupportedBands(true, 'ALL');
              this.getCurrentSettings();
            }
          });
          setTimeout(() => { this._pageReady = true; }, 0);
        },

        async init() {
          this._initialized = false;
          this._userTouchedBands = false;

          this.registerPageReturn();

          this.loadBandProfiles();

          // 型号已硬编码为 RG520N-CN，无需异步获取，直接应用固定频段集
          this.applyModelBands();
          this.rebuildBandGroups();
          const lockedBandsPromise = this.getSupportedBands(true, 'ALL');
          await Promise.all([
            this.getCurrentSettings(),
            lockedBandsPromise
          ]);

          this._initialized = true;
        },
      };
    }

    if (window.SimpleAdminSpaMode) {
      (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).network = networkPage;
    } else {
      mountSimpleAdminVueApp(networkPage);
    }
