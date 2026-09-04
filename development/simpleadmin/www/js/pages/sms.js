function fetchSMS() {
  return {
    isLoading: false,
    smsLoadFailed: false,
    smsPending: false,
    selectAllChecked: false,
    messages: [],
    senders: [],
    dates: [],
    selectedMessages: [],
    phoneNumber: '',
    messageToSend: '',
    isSending: false,
    messageIndices: [],
    serviceCenters: [],
    activeMessageIndex: null,
    smsDetailGlobalHandlersBound: false,
    smsAutoRefreshPoll: null,
    smsAutoRefreshIntervalMs: 5000,
    smsListSignature: '',
    smsIndexSignature: '',
    smsMetaSignature: '',
    smsPendingData: null,
    smsPendingIndexSignature: '',
    smsPendingListSignature: '',
    smsPendingStableCount: 0,
    smsPendingFirstSeenAt: 0,
    smsStablePollsRequired: 2,
    smsPendingMaxWaitMs: 15000,
    smsPollFailCount: 0,

    watch: {
      selectedMessages: {
        handler() {
          this.syncSelectAll();
        },
        deep: true
      }
    },

    t(key) {
      return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
    },

    notify(type, text) {
      if (SimpleAdmin.UI && typeof SimpleAdmin.UI.notify === 'function') {
        SimpleAdmin.UI.notify(type, text);
      }
    },

    clearData() {
      this.messages = [];
      this.senders = [];
      this.dates = [];
      this.selectedMessages = [];
      this.messageIndices = [];
      this.smsListSignature = '';
      this.smsIndexSignature = '';
      this.smsMetaSignature = '';
      this.smsPending = false;
      this.resetSMSPendingRefresh();
      this.activeMessageIndex = null;
      this.setMessageDetailOpenState(false);
      this.syncSelectAll();
    },

    requestSMS(options = {}) {
      const silent = options.silent === true;
      const params = { action: 'list' };
      if (options.force === true) params.force = '1';
      if (!silent) this.isLoading = true;
      return SimpleAdmin.Api.smsData(params)
        .then((data) => {
          if (this.applySMSPendingOrError(data)) return;
          this.smsLoadFailed = false;
          this.applySMSData(data, {
            keepDetail: options.keepDetail === true,
            keepSelection: options.keepSelection === true
          });
        })
        .finally(() => {
          if (!silent) this.isLoading = false;
        });
    },

    // sms_data 应答的 pending/error 状态统一处理:开机/模块重启保护期后端
    // 返回 pending(解析为空列表),读取完全失败返回 error。两者都不得用
    // 空列表覆盖既有收件箱——pending 保留加载提示等待后台就绪(自动轮询
    // 会补取),error 走失败横幅+手动重试。返回 true 表示调用方中止应用。
    applySMSPendingOrError(data) {
      if (data && data.pending === true) {
        this.smsPending = true;
        return true;
      }
      this.smsPending = false;
      if (data && data.error) {
        this.smsLoadFailed = true;
        return true;
      }
      return false;
    },

    applySMSData(data, options = {}) {
      // 后台轮询(fetchStableSMSList)也经由此方法应用数据,
      // 成功载入时复位失败/等待标志,避免初次加载失败被轮询救回后横幅残留。
      this.smsLoadFailed = false;
      this.smsPending = false;
      const activeKey = options.keepDetail === true ? this.currentMessageKey(this.activeMessageIndex) : '';
      const selectedKeys = options.keepSelection === true
        ? this.selectedMessages.map((index) => this.currentMessageKey(index)).filter(Boolean)
        : [];

      this.messages = [];
      this.senders = [];
      this.dates = [];
      this.selectedMessages = [];
      this.messageIndices = [];
      this.serviceCenters = data.serviceCenters || [];
      (data.messages || []).forEach((msg) => {
        const date = msg.date ? SimpleAdmin.Time.parseCustomDate(msg.date) : null;
        this.pushSMSMessage(
          msg.sender || '',
          date,
          msg.text || '',
          msg.indices || [],
          Array.isArray(msg.textLines) ? msg.textLines : [],
          msg.storage
        );
      });

      this.smsListSignature = this.makeSMSListSignature(data);
      this.smsIndexSignature = this.makeSMSIndexSignature(data);
      this.smsMetaSignature = this.makeSMSMetaSignature(data);
      this.resetSMSPendingRefresh();
      this.restoreSelectionByKeys(selectedKeys);
      this.restoreActiveMessageByKey(activeKey);
      this.syncSelectAll();
    },

    pushSMSMessage(sender, date, text, indices, textLines = [], storage = 'ME') {
      const normalizedText = this.normalizeMessageText(text);
      const lines = this.normalizeMessageLines(normalizedText, textLines);
      this.messageIndices.push(indices);
      this.senders.push(sender);
      this.dates.push(date ? this.formatDate(date) : '-');
      this.messages.push({ text: normalizedText, lines, sender, date, indices, storage: storage === 'SM' ? 'SM' : 'ME' });
    },

    normalizeMessageText(text) {
      return String(text || '')
        .replace(/\r\n/g, '\n')
        .replace(/\r/g, '\n')
        .replace(/[\v\f\u0085\u2028\u2029]/g, '\n')
        .replace(/\\r\\n/g, '\n')
        .replace(/\\n/g, '\n')
        .replace(/\\r/g, '\n');
    },

    normalizeMessageLines(text, textLines = []) {
      if (Array.isArray(textLines) && textLines.length > 0) {
        const lines = [];
        textLines.forEach((line) => {
          this.normalizeMessageText(line).split('\n').forEach((part) => {
            lines.push(part);
          });
        });
        return lines;
      }
      return this.normalizeMessageText(text).split('\n');
    },

    getMessagePreview(message) {
      const text = Array.isArray(message?.lines) ? message.lines.join(' ') : message?.text;
      return String(text || '').replace(/\s+/g, ' ').trim() || '（无内容）';
    },

    makeSMSListSignature(data) {
      return JSON.stringify((data.messages || []).map((msg) => [
        msg.storage === 'SM' ? 'SM' : 'ME',
        msg.sender || '',
        msg.date || '',
        msg.text || '',
        Array.isArray(msg.indices) ? msg.indices.join(',') : '',
        Array.isArray(msg.textLines) ? msg.textLines.join('\n') : ''
      ]));
    },

    makeSMSIndexSignature(data) {
      const indices = [];
      (data.messages || []).forEach((msg) => {
        if (!Array.isArray(msg.indices)) return;
        const storage = msg.storage === 'SM' ? 'SM' : 'ME';
        msg.indices.forEach((index) => {
          const n = Number(index);
          if (Number.isFinite(n)) indices.push(storage + ':' + n);
        });
      });
      indices.sort();
      return indices.join(',');
    },

    hasIncompleteMultipartSMS(data) {
      return (data.messages || []).some((msg) => {
        const total = Number(msg.concatTotal || 0);
        if (!Number.isFinite(total) || total <= 1) return false;
        const indices = Array.isArray(msg.indices) ? msg.indices : [];
        return indices.length < total;
      });
    },

    resetSMSPendingRefresh() {
      this.smsPendingData = null;
      this.smsPendingIndexSignature = '';
      this.smsPendingListSignature = '';
      this.smsPendingStableCount = 0;
      this.smsPendingFirstSeenAt = 0;
    },

    messageKey(message, sender, dateText) {
      if (!message) return '';
      const indices = Array.isArray(message.indices) ? message.indices.join(',') : '';
      return [indices, sender || message.sender || '', dateText || '', message.text || ''].join('|');
    },

    currentMessageKey(index) {
      if (index === null || index === undefined) return '';
      return this.messageKey(this.messages[index], this.senders[index], this.dates[index]);
    },

    restoreSelectionByKeys(keys) {
      if (!Array.isArray(keys) || keys.length === 0) return;
      const keySet = new Set(keys);
      this.selectedMessages = this.messages
        .map((_, index) => index)
        .filter((index) => keySet.has(this.currentMessageKey(index)));
    },

    restoreActiveMessageByKey(key) {
      if (!key) {
        this.activeMessageIndex = null;
        this.setMessageDetailOpenState(false);
        return;
      }
      const nextIndex = this.messages.findIndex((_, index) => this.currentMessageKey(index) === key);
      if (nextIndex >= 0) {
        this.activeMessageIndex = nextIndex;
        this.setMessageDetailOpenState(true);
        return;
      }
      this.activeMessageIndex = null;
      this.setMessageDetailOpenState(false);
    },

    syncSelectAll() {
      this.selectAllChecked = this.selectedMessages.length > 0 && this.selectedMessages.length === this.messages.length;
    },

    bindMessageDetailGlobalHandlers() {
      if (this.smsDetailGlobalHandlersBound) return;
      this.smsDetailGlobalHandlersBound = true;
      window.addEventListener('simpleadmin:page-changed', (event) => {
        if (event?.detail?.page === 'sms') {
          this.startSMSAutoRefresh();
          return;
        }
        this.stopSMSAutoRefresh();
        this.closeMessageDetail();
      });
      window.addEventListener('keydown', (event) => {
        if (event.key === 'Escape' && this.activeMessageIndex !== null) {
          this.closeMessageDetail();
        }
      });
    },

    isSMSPageActive() {
      const page = document.querySelector('.sa-page[data-page="sms"]');
      return !!page && page.classList.contains('active');
    },

    startSMSAutoRefresh() {
      if (!this.isSMSPageActive()) return;
      if (!this.smsAutoRefreshPoll && SimpleAdmin.Poll) {
        this.smsAutoRefreshPoll = SimpleAdmin.Poll.create({
          tick: () => this.pollSMSMeta(),
          interval: this.smsAutoRefreshIntervalMs,
          immediate: false
        });
      }
      if (this.smsAutoRefreshPoll) this.smsAutoRefreshPoll.start();
    },

    stopSMSAutoRefresh() {
      if (this.smsAutoRefreshPoll) this.smsAutoRefreshPoll.stop();
      this.resetSMSPendingRefresh();
    },

    pollSMSMeta() {
      if (!this.isSMSPageActive()) {
        this.stopSMSAutoRefresh();
        return null;
      }
      if (this.isLoading) return null;
      return SimpleAdmin.Api.smsData({ action: 'list_meta' })
        .then((data) => {
          this.smsPollFailCount = 0;
          if (!this.isSMSPageActive()) return null;
          return this.handlePolledSMSMeta(data);
        })
        .catch((err) => {
          this.smsPollFailCount += 1;
          console.warn(`短信轮询失败（连续 ${this.smsPollFailCount} 次）`, err);
          throw err;
        });
    },

    refreshInbox() {
      if (this.isLoading) return Promise.resolve();
      return SimpleAdmin.Api.smsData({ action: 'list_meta' })
        .then((data) => {
          this.smsPollFailCount = 0;
          if (!this.isSMSPageActive()) return null;
          if (data && data.pending === true) {
            this.smsPending = true;
            return null;
          }
          this.smsPending = false;
          if (data && data.error) {
            this.smsLoadFailed = true;
            return null;
          }
          this.smsLoadFailed = false;
          const indexSignature = this.makeSMSIndexSignature(data);
          const metaSignature = this.makeSMSMetaSignature(data);
          if (indexSignature === this.smsIndexSignature && metaSignature === this.smsMetaSignature) {
            this.resetSMSPendingRefresh();
            return null;
          }
          return this.fetchStableSMSList('');
        })
        .catch((err) => {
          this.smsPollFailCount += 1;
          console.warn('短信刷新失败', err);
          this.smsLoadFailed = true;
          this.notify('danger', this.t('短信读取失败'));
        });
    },

    makeSMSMetaSignature(data) {
      return JSON.stringify((data.messages || []).map((msg) => [
        msg.sender || '',
        msg.date || '',
        Array.isArray(msg.indices) ? msg.indices.join(',') : '',
        msg.concatTotal || '',
        msg.concatRef || '',
        msg.concatSeq || ''
      ]));
    },

    handlePolledSMSMeta(data) {
      // 保护期 pending/读取失败 error 的 meta 不参与变更检测:空列表签名会
      // 误判为"收件箱变化"触发全量重拉并清空界面;静默跳过本轮,
      // 下一轮轮询自动补取。
      if (data && (data.pending === true || data.error)) return null;
      const indexSignature = this.makeSMSIndexSignature(data);
      const metaSignature = this.makeSMSMetaSignature(data);
      if (indexSignature === this.smsIndexSignature && metaSignature === this.smsMetaSignature) {
        this.resetSMSPendingRefresh();
        return null;
      }

      const now = Date.now();
      if (indexSignature === this.smsPendingIndexSignature && metaSignature === this.smsPendingListSignature) {
        this.smsPendingStableCount += 1;
      } else {
        this.smsPendingData = data;
        this.smsPendingIndexSignature = indexSignature;
        this.smsPendingListSignature = metaSignature;
        this.smsPendingStableCount = 1;
        this.smsPendingFirstSeenAt = now;
      }

      const waited = now - this.smsPendingFirstSeenAt;
      const incompleteMultipart = this.hasIncompleteMultipartSMS(this.smsPendingData || data);
      if (incompleteMultipart && waited < this.smsPendingMaxWaitMs) return null;
      if (this.smsPendingStableCount < this.smsStablePollsRequired && waited < this.smsPendingMaxWaitMs) return null;

      return this.fetchStableSMSList(this.smsPendingIndexSignature);
    },

    fetchStableSMSList(expectedIndexSignature) {
      return SimpleAdmin.Api.smsData({ action: 'list', force: '1' })
        .then((data) => {
          this.smsPollFailCount = 0;
          if (!this.isSMSPageActive()) return;
          if (this.applySMSPendingOrError(data)) return;
          const indexSignature = this.makeSMSIndexSignature(data);
          const metaSignature = this.makeSMSMetaSignature(data);
          if (indexSignature === this.smsIndexSignature && metaSignature === this.smsMetaSignature) {
            this.resetSMSPendingRefresh();
            return;
          }
          if (expectedIndexSignature && indexSignature !== expectedIndexSignature) {
            this.smsPendingData = data;
            this.smsPendingIndexSignature = indexSignature;
            this.smsPendingListSignature = metaSignature;
            this.smsPendingStableCount = 1;
            this.smsPendingFirstSeenAt = Date.now();
            return;
          }
          this.applySMSData(data, { keepDetail: true, keepSelection: true });
        })
        .catch((err) => {
          this.smsPollFailCount += 1;
          console.warn(`短信轮询失败（连续 ${this.smsPollFailCount} 次）`, err);
          throw err;
        });
    },

    setMessageDetailOpenState(isOpen) {
      if (document.body) {
        document.body.classList.toggle('sa-sms-detail-open', isOpen);
      }
    },

    openMessageDetail(index) {
      this.activeMessageIndex = index;
      this.setMessageDetailOpenState(true);
      if (typeof this.$nextTick === 'function') {
        this.$nextTick(() => {
          const closeButton = document.querySelector('.sa-sms-detail-modal .sa-modal-close');
          if (closeButton) closeButton.focus({ preventScroll: true });
        });
      }
    },

    closeMessageDetail() {
      this.activeMessageIndex = null;
      this.setMessageDetailOpenState(false);
    },

    formatDate(date) {
      // 输出与后端一致的 YY/MM/DD(年在前)顺序,保证解析↔显示精确往返;
      // 旧实现输出 DD/MM/YY,与 parseCustomDate 的错位解构互相抵消才显得正常,
      // 解析修正后必须同步为年在前,否则日期会再次被调换显示。
      const year = (date.getUTCFullYear() - 2000).toString().padStart(2, '0');
      const month = (date.getUTCMonth() + 1).toString().padStart(2, '0');
      const day = date.getUTCDate().toString().padStart(2, '0');
      const hour = date.getUTCHours().toString().padStart(2, '0');
      const minute = date.getUTCMinutes().toString().padStart(2, '0');
      const second = date.getUTCSeconds().toString().padStart(2, '0');
      return `${year}/${month}/${day},${hour}:${minute}:${second}`;
    },

    deleteSelectedSMS() {
      if (this.selectedMessages.length === 0) {
        console.warn('没有选中的短信');
        return;
      }

      if (!this.messageIndices || this.messageIndices.length === 0) {
        console.error('短信索引未正确初始化或为空');
        return;
      }

      if (this.selectedMessages.length === this.messages.length) {
        this.confirmClearAllSMS();
        return;
      }

      SimpleAdmin.UI.confirm({
        title: '删除短信',
        message: '确定删除选中的短信？',
        danger: true,
        confirmText: '删除',
        onConfirm: () => this.performDeleteSelectedSMS()
      });
    },

    performDeleteSelectedSMS() {
      const indicesToDelete = [];
      this.selectedMessages.forEach((index) => {
        const msg = this.messages[index] || {};
        const storage = msg.storage === 'SM' ? 'SM' : 'ME';
        (msg.indices || []).forEach((idx) => {
          indicesToDelete.push(storage + ':' + idx);
        });
      });

      if (indicesToDelete.length === 0) {
        console.warn('没有有效的短信索引');
        return;
      }

      SimpleAdmin.Api.smsData({ action: 'delete_indices', indices: indicesToDelete.join(',') })
        .then((data) => {
          if (data && data.ok === false) {
            this.notify('danger', this.t('删除失败') + '：' + (data.error || this.t('未知错误')));
            return;
          }
          this.selectedMessages = [];
          this.syncSelectAll();
          // 删除成功后刷新收件箱;刷新失败单独提示(删除本身已成功),
          // 不能吞掉 Promise 拒绝形成未处理 rejection。
          return this.requestSMS({ force: true }).catch(() => {
            this.notify('danger', this.t('短信读取失败'));
          });
        })
        .catch(() => {
          this.notify('danger', '删除失败');
        });
    },

    confirmClearAllSMS() {
      SimpleAdmin.UI.confirm({
        title: '清空短信',
        message: '确定删除全部短信？',
        danger: true,
        confirmText: '清空',
        onConfirm: () => this.deleteAllSMS()
      });
    },

    deleteAllSMS() {
      SimpleAdmin.Api.smsData({ action: 'delete_all' })
        .then((data) => {
          if (data && data.ok === false) {
            this.notify('danger', this.t('删除失败') + '：' + (data.error || this.t('未知错误')));
            return;
          }
          this.clearData();
          return this.requestSMS({ force: true });
        })
        .catch(() => {
          this.notify('danger', '删除失败');
        });
    },

    async sendSMS() {
      if (this.isSending) return;
      if (!this.phoneNumber || !this.messageToSend) {
        this.notify('warning', '号码或内容不能为空');
        return;
      }

      this.isSending = true;
      try {
        try {
          const simStatus = await SimpleAdmin.Api.smsData({ action: 'sim_status' });
          if (!simStatus.inserted) {
            this.notify('danger', '未检测到 SIM 卡');
            return;
          }
        } catch { }

        try {
          const result = await SimpleAdmin.Api.smsData({
            action: 'send',
            number: this.phoneNumber,
            message: this.messageToSend
          });
          if (result.ok) {
            this.notify('success', '短信发送成功！');
            this.phoneNumber = '';
            this.messageToSend = '';
            this.requestSMS({ force: true });
            return;
          }
          this.notify('danger', '短信发送失败：' + (result.error || this.t('未知错误')));
        } catch (error) {
          this.notify('danger', '短信发送失败：' + (error?.message || this.t('未知错误')));
        }
      } finally {
        this.isSending = false;
      }
    },

    init() {
      this.bindMessageDetailGlobalHandlers();
      this.clearData();
      this.requestSMS({ force: true })
        .catch((err) => {
          console.error('短信加载失败', err);
          this.smsLoadFailed = true;
        })
        .finally(() => {
          this.startSMSAutoRefresh();
        });
    },

    toggleAll(event) {
      // 模板使用内联 @change="toggleAll()"，Vue 不会传入事件对象（未写 $event），
      // 此处若依赖 event.target.checked 会抛 TypeError 导致全选永远失效。
      // change 触发时 v-model 已更新 selectAllChecked，直接以它为准；
      // 保留 event 形参以兼容可能的事件/无参数调用，但逻辑不依赖它。
      const checked = (event && event.target) ? !!event.target.checked : this.selectAllChecked;
      this.selectedMessages = checked ? this.messages.map((_, index) => index) : [];
      this.syncSelectAll();
    }
  };
}

if (window.SimpleAdminSpaMode) {
  (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).sms = fetchSMS;
} else {
  mountSimpleAdminVueApp(fetchSMS);
}
