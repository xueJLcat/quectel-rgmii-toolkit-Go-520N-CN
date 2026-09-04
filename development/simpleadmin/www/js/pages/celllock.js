function celllockPage() {
      return {
        isLoading: false,
        networkModeCell: "-",
        earfcn1: null, pci1: null,
        earfcn2: null, pci2: null,
        earfcn3: null, pci3: null,
        earfcn4: null, pci4: null,
        earfcn5: null, pci5: null,
        earfcn6: null, pci6: null,
        earfcn7: null, pci7: null,
        earfcn8: null, pci8: null,
        earfcn9: null, pci9: null,
        earfcn10: null, pci10: null,
        scs: null,
        band: null,
        cellNum: null,
        cellLockStatus: "未知",
        selectedCells: [],
        nr5g_cells_parsed: [],
        lte_cells_parsed: [],
        isCellScanning: false,
        resultDoneCell: false,
        scanNotice: "",
        settingsLoadFailed: false,
        servingSCS: "auto",
        newEarfcnNR5G: "",
        newEarfcnLTE: "",
        earfcnLockNR5G: [],
        earfcnLockLTE: [],
        earfcnLockLoaded: false,
        earfcnLockFailed: false,
        isSavingEarfcn: false,
        _cellScanGen: 0,
        _currentSettingsFetchInFlight: null,
        _settingsPending: false,
        _settingsRetryCount: 0,
        _settingsRetryTimer: null,
        _pageReturnBound: false,
        _pageReady: false,

        computed: {
          scanTableRows() {
            return [...this.nr5g_cells_parsed, ...this.lte_cells_parsed];
          }
        },

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

        // ---------- 小区扫描（声明式表格） ----------
        startCellScan() {
          if (this.isCellScanning) return;
          this.clearCellScan();

          this.isLoading = true;
          this.isCellScanning = true;

          this._cellScanGen = (this._cellScanGen || 0) + 1;
          const gen = this._cellScanGen;

          this.scanNotice = '扫描中…最长约 3 分钟,请勿离开页面';

          SimpleAdmin.Api.networkData({ action: 'scan', mode: 'Neighbour Scan' })
            .then(data => {
              if (gen !== this._cellScanGen) return;
              if (!data || data.ok === false) {
                this.selectedCells = [];
                this.clearCellScanData();
                this.scanNotice = '';
                this.notify('danger', (data && data.error) || '扫描失败，请重试');
                return;
              }
              if (data.pending === true) {
                this.selectedCells = [];
                this.clearCellScanData();
                this.scanNotice = '模块尚未就绪,请稍后重试';
                return;
              }
              this.nr5g_cells_parsed = data.nr5g_cells_parsed || [];
              this.lte_cells_parsed = data.lte_cells_parsed || [];
              this.resultDoneCell = true;
              if (data.empty === true) {
                this.scanNotice = '扫描完成,模块未返回任何小区(该固件扫描功能可能受限)';
              } else {
                this.scanNotice = this.scanTableRows.length === 0 ? '未扫描到小区' : '';
              }
            })
            .catch(error => {
              if (gen !== this._cellScanGen) return;
              console.error("Error processing scan data:", error);
              this.selectedCells = [];
              this.clearCellScanData();
              this.scanNotice = '';
              this.notify('danger', '扫描失败，请重试');
            })
            .finally(() => {
              if (gen !== this._cellScanGen) return;
              this.isLoading = false;
              this.isCellScanning = false;
            });
        },

        // 小区唯一标识:同一 PCI 在不同频点(earfcn)上属于不同小区
        cellKey(cell) {
          return [cell.type, cell.pci, cell.provider, cell.freq].join('|');
        },

        isCellSelected(cell) {
          const key = this.cellKey(cell);
          return this.selectedCells.some(item => this.cellKey(item) === key);
        },

        signalBars(rsrp) {
          const value = Number(rsrp);
          if (!Number.isFinite(value)) return 0;
          if (value >= -55) return 5;
          if (value >= -85) return 4;
          if (value >= -95) return 3;
          if (value >= -105) return 2;
          if (value >= -115) return 1;
          return 0;
        },

        toggleCellSelection(pci, provider, type, freq) {
          const key = this.cellKey({ pci, provider, type, freq });
          const index = this.selectedCells.findIndex(cell => this.cellKey(cell) === key);

          if (type === "NR5G") {
            if (index === -1) {
              const lteSelected = this.selectedCells.filter(cell => cell.type !== "NR5G");
              lteSelected.push({ pci, provider, type, freq });
              this.selectedCells = lteSelected;
            } else {
              this.selectedCells.splice(index, 1);
            }
          } else {
            if (index === -1) {
              const lteSelectedCount = this.selectedCells.filter(cell => cell.type !== "NR5G").length;
              if (lteSelectedCount >= 10) {
                this.notify('danger', '最多只能选择 10 条小区进行锁定');
              } else {
                this.selectedCells.push({ pci, provider, type, freq });
              }
            } else {
              this.selectedCells.splice(index, 1);
            }
          }
        },

        clearCellScanData() {
          this.nr5g_cells_parsed = [];
          this.lte_cells_parsed = [];
        },

        clearCellScan() {
          this.selectedCells = [];
          this.clearCellScanData();
          this.resultDoneCell = false;
          this.scanNotice = '';
        },

        lockSelectedCells() {
          if (this.selectedCells.length === 0) {
            this.notify('danger', '请至少选择一个小区进行锁定.');
            return;
          }

          const nr5gSelected = this.selectedCells.find(cell => cell.type === "NR5G");
          if (nr5gSelected) {
            SimpleAdmin.UI.confirm({
              title: '锁定小区',
              message: this.t('NR5G 小区锁需要探测 SCS，期间网络可能短暂中断约 25 秒。继续？'),
              danger: true,
              onConfirm: () => this.doLockSelectedCells()
            });
            return;
          }
          this.doLockSelectedCells();
        },

        async doLockSelectedCells() {
          const nr5gSelected = this.selectedCells.find(cell => cell.type === "NR5G");
          const lteSelectedCells = this.selectedCells.filter(cell => cell.type !== "NR5G");
          let nr5gLocked = false;

          if (nr5gSelected) {
            const { earfcn1: earfcn, pci1: cellPci, scs, band } = this.getCellDetails(nr5gSelected.pci, nr5gSelected.provider, "NR5G", nr5gSelected.freq);

            if (earfcn === undefined || scs === undefined || band === undefined) {
              this.notify('danger', '找不到对应的小区数据，请重新扫描');
              return;
            }

            const result = await SimpleAdmin.Api.networkData({ action: 'lock_scanned_cells', mode: "NR5G Only", pci: cellPci, earfcn, scs, band })
              .catch(error => { console.error('发送锁定命令失败:', error); });
            if (!this.applyNetworkActionResult(result)) {
              this.reportNetworkActionFailure(result, '锁定失败');
              return;
            }
            nr5gLocked = true;
          }

          if (lteSelectedCells.length > 0) {
            if (lteSelectedCells.length > 10) {
              this.notify('danger', '最多只能选择 10 条小区进行锁定');
              return;
            }
            const pairs = [];
            const pcias = [];

            for (const { pci, provider, freq } of lteSelectedCells) {
              const { earfcn1: earfcn, pci1: cellPci } = this.getCellDetails(pci, provider, "LTE", freq);

              if (earfcn === undefined || cellPci === undefined) {
                this.notify('danger', '找不到对应的小区数据，请重新扫描');
                return;
              }

              pairs.push(earfcn);
              pcias.push(cellPci);
            }

            const result = await SimpleAdmin.Api.networkData({
              action: 'lock_scanned_cells',
              mode: "LTE Only",
              earfcn: pairs.join(','),
              pci: pcias.join(',')
            })
              .catch(error => { console.error('发送锁定命令失败:', error); });
            if (!this.applyNetworkActionResult(result)) {
              this.reportNetworkActionFailure(result, '锁定失败');
              // NR5G 已锁成功而 LTE 失败:状态已变化,回读刷新,避免界面停留在
              // 旧锁定状态、用户无从得知 NR5G 锁其实已生效。
              if (nr5gLocked) {
                await this.sleep(3000);
                await this.init();
              }
              return;
            }
          }

          this.notify('success', '操作成功，正在刷新状态');
          await this.sleep(3000);
          await this.init();
        },

        getCellDetails(pci, provider, type, freq) {
          const matches = (c) => c.pci === pci && c.provider === provider &&
            (freq === undefined || freq === null || c.freq === freq);
          let cell;
          if (type === "NR5G") {
            cell = this.nr5g_cells_parsed.find(matches);
          } else if (type === "LTE") {
            cell = this.lte_cells_parsed.find(matches);
          }

          if (!cell) {
            console.error(`找不到对应的 cell 数据: PCI=${pci}, Provider=${provider}, Type=${type}, Freq=${freq}`);
            return { earfcn1: undefined, pci1: undefined, scs: undefined, band: undefined };
          }

          return {
            earfcn1: cell.freq,
            pci1: cell.pci,
            scs: '',
            band: cell.band
          };
        },

        // ---------- 手动小区锁定/解锁 ----------
        getLteCellCount() {
          if (this.cellNum === null || this.cellNum === undefined || this.cellNum === '') return 0;
          const count = Math.floor(Number(this.cellNum));
          if (!Number.isFinite(count) || count < 1) return 0;
          return Math.min(count, 10);
        },

        hasLteManualCellCount() {
          return this.networkModeCell === "LTE" && this.getLteCellCount() > 0;
        },

        visibleLteCellIndexes() {
          const count = this.getLteCellCount();
          const indexes = [];
          for (let i = 1; i <= count; i += 1) indexes.push(i);
          return indexes;
        },

        getLteManualValue(index, field) {
          const suffix = field === 'pci' ? 'pci' : 'earfcn';
          return this[`${suffix}${index}`] || '';
        },

        setLteManualValue(index, field, value) {
          const suffix = field === 'pci' ? 'pci' : 'earfcn';
          this[`${suffix}${index}`] = value;
        },

        clearLteManualInputsAfter(count) {
          for (let i = count + 1; i <= 10; i += 1) {
            this[`earfcn${i}`] = null;
            this[`pci${i}`] = null;
          }
        },

        resetLteManualInputs() {
          this.cellNum = null;
          this.clearLteManualInputsAfter(0);
        },

        resetManualLockForm() {
          this.networkModeCell = "-";
          this.resetLteManualInputs();
          this.scs = null;
          this.band = null;
        },

        normalizeLteCellCount() {
          if (this.cellNum === null || this.cellNum === undefined || this.cellNum === '') {
            this.cellNum = null;
            this.clearLteManualInputsAfter(0);
            return;
          }

          const count = this.getLteCellCount();
          if (count < 1) {
            this.cellNum = null;
            this.clearLteManualInputsAfter(0);
            return;
          }

          this.cellNum = count;
          this.clearLteManualInputsAfter(count);
        },

        onNetworkModeCellChange(event) {
          const selectedMode = event && event.target ? event.target.value : this.networkModeCell;
          this.networkModeCell = selectedMode;
          if (selectedMode === "LTE") {
            this.resetLteManualInputs();
          }
        },

        getVisibleLteManualPairs(count) {
          const pairs = [];
          for (let i = 1; i <= count; i += 1) {
            const earfcn = String(this[`earfcn${i}`] || '').trim();
            const pci = String(this[`pci${i}`] || '').trim();
            if (!earfcn || !pci) return null;
            if (!/^\d+$/.test(earfcn) || !/^\d+$/.test(pci)) return false;
            pairs.push({ earfcn, pci });
          }
          return pairs;
        },

        async cellLockEnableLTE() {
          const cellNum = this.getLteCellCount();
          if (cellNum < 1) {
            this.notify('danger', '请输入要锁定的小区数量');
            return;
          }

          const pairs = this.getVisibleLteManualPairs(cellNum);
          if (pairs === false) {
            this.notify('danger', '参数必须为纯数字');
            return;
          }
          if (!pairs) {
            this.notify('danger', `请完整填写 ${cellNum} 组 EARFCN 和 PCI`);
            return;
          }

          const result = await SimpleAdmin.Api.networkData({
            action: 'lock_lte_manual',
            cellNum,
            pairs: pairs.map(p => `${p.earfcn},${p.pci}`).join(';')
          })
            .catch(error => { console.error('发送 LTE 锁定命令失败:', error); });
          if (!this.applyNetworkActionResult(result)) {
            this.reportNetworkActionFailure(result);
            return;
          }
          this.resetManualLockForm();
          this.notify('success', '操作成功，正在刷新状态');
          await this.sleep(3000);
          await this.init();
        },

        cellLockEnableNR() {
          const { earfcn1: earfcn, pci1: pci, scs, band } = this;
          if (earfcn === null || pci === null || scs === null || band === null) {
            this.notify('danger', '请输入所有必填字段');
            return;
          }
          const nrEarfcn = String(earfcn).trim();
          const nrPci = String(pci).trim();
          const nrScs = String(scs).trim();
          const nrBand = String(band).trim();
          if (![nrEarfcn, nrPci, nrScs, nrBand].every(v => /^\d+$/.test(v))) {
            this.notify('danger', '参数必须为纯数字');
            return;
          }
          SimpleAdmin.UI.confirm({
            title: '锁定小区',
            message: 'NR5G 小区锁需要探测 SCS，期间网络可能短暂中断约 25 秒。继续？',
            danger: true,
            onConfirm: () => this.doCellLockEnableNR(nrEarfcn, nrPci, nrScs, nrBand)
          });
        },

        async doCellLockEnableNR(nrEarfcn, nrPci, nrScs, nrBand) {
          const result = await SimpleAdmin.Api.networkData({ action: 'lock_nr_manual', pci: nrPci, earfcn: nrEarfcn, scs: nrScs, band: nrBand })
            .catch(error => { console.error('发送 NR5G 锁定命令失败:', error); });
          if (!this.applyNetworkActionResult(result)) {
            this.reportNetworkActionFailure(result);
            return;
          }
          this.resetManualLockForm();
          this.notify('success', '操作成功，正在刷新状态');
          await this.sleep(3000);
          await this.init();
        },

        cellLockDisableLTE() {
          SimpleAdmin.UI.confirm({
            title: '解锁LTE小区',
            message: '这将解除 LTE 小区锁定。继续？',
            danger: true,
            onConfirm: () => { this.doCellLockDisable('unlock_lte'); }
          });
        },

        cellLockDisableNR() {
          SimpleAdmin.UI.confirm({
            title: '解锁NR5G-SA小区',
            message: '这将解除 NR5G-SA 小区锁定。继续？',
            danger: true,
            onConfirm: () => { this.doCellLockDisable('unlock_nr'); }
          });
        },

        async doCellLockDisable(action) {
          const result = await SimpleAdmin.Api.networkData({ action })
            .catch(error => { console.error('发送解锁命令失败:', error); });
          if (!this.applyNetworkActionResult(result)) {
            this.reportNetworkActionFailure(result, '解锁失败');
            return;
          }
          this.resetManualLockForm();
          this.notify('success', '操作成功，正在刷新状态');
          await this.sleep(3000);
          await this.init();
        },

        // ---------- 锁定当前服务小区 ----------
        lockServingCell() {
          SimpleAdmin.UI.confirm({
            title: '锁定当前服务小区',
            message: '将锁定当前驻留小区，期间可能短暂断网。继续？',
            danger: true,
            onConfirm: () => this.doLockServingCell()
          });
        },

        async doLockServingCell() {
          this.isLoading = true;
          const payload = { action: 'lock_serving_cell' };
          if (this.servingSCS !== 'auto') payload.scs = this.servingSCS;
          const result = await SimpleAdmin.Api.networkData(payload)
            .catch(error => { console.error('发送锁定当前服务小区命令失败:', error); });
          this.isLoading = false;
          if (!result || result.ok === false) {
            this.notify('danger', (result && result.error) || this.t('SCS 探测失败，锁定已自动解除，请手动指定 SCS 重试'));
            return;
          }
          const details = [result.locked, result.pci, result.freq, result.band]
            .filter(value => value !== undefined && value !== null && value !== '')
            .join(' / ');
          this.notify('success', details);
          await this.sleep(3000);
          await this.init();
        },

        // ---------- 频点锁定 ----------
        fetchEarfcnLockStatus(isRetry = false) {
          return SimpleAdmin.Api.networkData({ action: 'earfcn_lock_status' })
            .then(data => {
              if (!data || data.ok === false) {
                this.earfcnLockLoaded = false;
                this.earfcnLockFailed = true;
                return;
              }
              if (data.pending === true) {
                if (!isRetry) {
                  setTimeout(() => { this.fetchEarfcnLockStatus(true); }, 3000);
                } else {
                  this.earfcnLockLoaded = false;
                  this.earfcnLockFailed = true;
                }
                return;
              }
              this.earfcnLockNR5G = Array.isArray(data.nr5g_arfcns) ? data.nr5g_arfcns : [];
              this.earfcnLockLTE = Array.isArray(data.lte_arfcns) ? data.lte_arfcns : [];
              this.earfcnLockLoaded = true;
              this.earfcnLockFailed = false;
            })
            .catch(error => {
              console.error('Error fetching earfcn lock status:', error);
              this.earfcnLockLoaded = false;
              this.earfcnLockFailed = true;
            });
        },

        async setEarfcnLock(rat) {
          const isNR = rat === 'NR5G';
          const raw = isNR ? this.newEarfcnNR5G : this.newEarfcnLTE;
          const arfcns = String(raw || '').split(',').map(value => value.trim()).filter(value => value !== '');
          const max = isNR ? 32 : 2;
          if (arfcns.length === 0) {
            this.notify('danger', '请输入频点');
            return;
          }
          if (!arfcns.every(value => /^\d+$/.test(value))) {
            this.notify('danger', '参数必须为纯数字');
            return;
          }
          if (arfcns.length > max) {
            this.notify('danger', '频点数量超出限制');
            return;
          }
          this.isSavingEarfcn = true;
          const result = await SimpleAdmin.Api.networkData({ action: 'set_earfcn_lock', rat, arfcns: arfcns.join(',') })
            .catch(error => { console.error('发送频点锁定命令失败:', error); });
          this.isSavingEarfcn = false;
          if (!result || result.ok === false) {
            this.notify('danger', (result && result.error) || this.t('操作失败'));
            return;
          }
          if (isNR) {
            this.newEarfcnNR5G = '';
          } else {
            this.newEarfcnLTE = '';
          }
          this.notify('success', '操作成功，正在刷新状态');
          await this.fetchEarfcnLockStatus();
        },

        clearEarfcnLock(rat) {
          SimpleAdmin.UI.confirm({
            title: '解锁频点',
            message: '这将解除 ' + rat + ' 频点锁定。继续？',
            danger: true,
            onConfirm: () => this.doClearEarfcnLock(rat)
          });
        },

        async doClearEarfcnLock(rat) {
          this.isSavingEarfcn = true;
          const result = await SimpleAdmin.Api.networkData({ action: 'clear_earfcn_lock', rat })
            .catch(error => { console.error('发送频点解锁命令失败:', error); });
          this.isSavingEarfcn = false;
          if (!result || result.ok === false) {
            this.reportNetworkActionFailure(result, '解锁失败');
            return;
          }
          this.notify('success', '操作成功，正在刷新状态');
          await this.fetchEarfcnLockStatus();
        },

        earfcnLockStatusText(rat) {
          const list = rat === 'NR5G' ? this.earfcnLockNR5G : this.earfcnLockLTE;
          if (!Array.isArray(list) || list.length === 0) return this.t('未启用');
          return this.t('已启用') + ': ' + list.join(',');
        },

        // ---------- 设置读取（仅小区锁定状态） ----------
        scheduleSettingsRetry() {
          if (this._settingsRetryCount >= 10) {
            // 重试耗尽:标记失败态,模板显示横幅+手动重试按钮,不再自动轮询
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
                // 统一并入重试路径,避免字段永久停在 "未知" 的死态
                this._settingsPending = true;
                this.scheduleSettingsRetry();
                return;
              }
              this._settingsPending = false;
              this._settingsRetryCount = 0;
              this.settingsLoadFailed = false;
              this.cellLockStatus = settings.cellLockStatus || '未知';
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

        // 重试耗尽后横幅上的手动重试:复位失败标志与重试计数,重新拉取。
        retrySettings() {
          this.settingsLoadFailed = false;
          this.getCurrentSettings(false);
        },

        // ---------- 初始化 ----------
        registerPageReturn() {
          if (this._pageReturnBound) return;
          this._pageReturnBound = true;
          SimpleAdmin.Poll.onPageReturn('celllock', () => {
            if (!this._pageReady) return;
            if (this._settingsPending) this.getCurrentSettings();
          });
          setTimeout(() => { this._pageReady = true; }, 0);
        },

        async init() {
          this.registerPageReturn();
          await this.getCurrentSettings();
          this.fetchEarfcnLockStatus();
        },
      };
    }

    if (window.SimpleAdminSpaMode) {
      (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).celllock = celllockPage;
    } else {
      mountSimpleAdminVueApp(celllockPage);
    }
