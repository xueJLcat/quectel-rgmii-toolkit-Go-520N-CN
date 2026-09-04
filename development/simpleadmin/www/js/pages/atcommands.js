function simpleAtCommands() {
  return {
    isLoading: false,
    isClean: true,
    atcmd: "",
    atCommandResponse: "",
    history: [],
    historyIndex: -1,
    historyDraft: "",
    quickCommands: [
      { label: "模块信息", cmd: "ATI" },
      { label: "信号强度", cmd: "AT+CSQ" },
      { label: "服务小区", cmd: 'AT+QENG="servingcell"' },
      { label: "信号详情", cmd: "AT+QRSRP" },
      { label: "固件版本", cmd: "AT+QGMR" },
      { label: "IMSI", cmd: "AT+CIMI" },
      { label: "LAN 配置", cmd: 'AT+QMAP="LANIP"' }
    ],

    t(key) {
      return SimpleAdmin.Lang ? SimpleAdmin.Lang.t(key) : key;
    },

    notify(type, text) {
      if (SimpleAdmin.UI && typeof SimpleAdmin.UI.notify === 'function') {
        SimpleAdmin.UI.notify(type, text);
      }
    },

    loadHistory() {
      try {
        const raw = localStorage.getItem("simpleadmin.atHistory");
        const parsed = raw ? JSON.parse(raw) : [];
        this.history = Array.isArray(parsed)
          ? parsed.filter((item) => typeof item === "string" && item.trim() !== "").slice(0, 50)
          : [];
      } catch (error) {
        console.error("读取命令历史失败：", error);
        this.history = [];
      }
    },

    saveHistory() {
      try {
        localStorage.setItem("simpleadmin.atHistory", JSON.stringify(this.history));
      } catch (error) {
        console.error("保存命令历史失败：", error);
      }
    },

    pushHistory(command) {
      const cmd = String(command || "").trim();
      if (!cmd) return;
      const index = this.history.indexOf(cmd);
      if (index !== -1) {
        this.history.splice(index, 1);
      }
      this.history.unshift(cmd);
      if (this.history.length > 50) {
        this.history = this.history.slice(0, 50);
      }
      this.saveHistory();
    },

    clearHistory() {
      this.history = [];
      this.historyIndex = -1;
      this.historyDraft = "";
      try {
        localStorage.removeItem("simpleadmin.atHistory");
      } catch (error) {
        console.error("清除命令历史失败：", error);
      }
    },

    historyPrev() {
      if (!this.history.length) return;
      if (this.historyIndex < 0) {
        this.historyDraft = this.atcmd;
      }
      if (this.historyIndex < this.history.length - 1) {
        this.historyIndex++;
        this.atcmd = this.history[this.historyIndex];
      }
    },

    historyNext() {
      if (this.historyIndex < 0) return;
      this.historyIndex--;
      if (this.historyIndex < 0) {
        this.atcmd = this.historyDraft;
        this.historyDraft = "";
      } else {
        this.atcmd = this.history[this.historyIndex];
      }
    },

    appendOutput(command, response) {
      const entry = "> " + command + "\n" + (response || "") + "\n";
      let output = this.atCommandResponse ? this.atCommandResponse + entry : entry;
      const lines = output.split("\n");
      if (lines.length > 200) {
        let cut = lines.length - 200;
        while (cut < lines.length && lines[cut].indexOf("> ") !== 0) {
          cut++;
        }
        if (cut >= lines.length) {
          cut = lines.length - 200;
        }
        output = lines.slice(cut).join("\n");
      }
      this.atCommandResponse = output;
      this.isClean = false;
    },

    sendATCommand() {
      if (this.isLoading) {
        return Promise.resolve("");
      }
      if (!this.atcmd || !this.atcmd.trim()) {
        return;
      }
      const command = this.atcmd;
      this.isLoading = true;
      return SimpleAdmin.Api.atData({ action: 'manual_at', command: command })
        .then((data) => {
          if (data && data.ok === false) {
            throw new Error(data.error || this.t('未知错误'));
          }
          this.appendOutput(command, data.response || '');
          this.pushHistory(command);
          this.historyIndex = -1;
          this.historyDraft = "";
          return data.response || '';
        })
        .catch((error) => {
          console.error("错误: ", error);
          this.notify('danger', this.t('发送AT命令失败:') + ' ' + ((error && error.message) || this.t('未知错误')));
          return '';
        })
        .finally(() => {
          this.isLoading = false;
        });
    },

    runQuickCommand(cmd) {
      this.atcmd = cmd;
      return this.sendATCommand();
    },

    copyOutput() {
      const text = this.atCommandResponse;
      const fallbackCopy = () => {
        const textarea = document.createElement("textarea");
        textarea.value = text;
        textarea.style.position = "fixed";
        textarea.style.opacity = "0";
        document.body.appendChild(textarea);
        textarea.select();
        let ok = false;
        try {
          ok = document.execCommand("copy");
        } catch (error) {
          ok = false;
        }
        document.body.removeChild(textarea);
        return ok;
      };
      const report = (ok) => {
        if (ok) {
          this.notify('success', this.t("已复制"));
        } else {
          this.notify('danger', this.t("复制失败"));
        }
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text)
          .then(() => report(true))
          .catch(() => report(fallbackCopy()));
      } else {
        report(fallbackCopy());
      }
    },

    clearResponses() {
      this.atCommandResponse = "";
      this.isClean = true;
    },

    confirmResetAT() {
      SimpleAdmin.UI.confirm({
        title: '重置 AT&F',
        message: '这将把调制解调器 AT 配置恢复出厂。',
        confirmText: '确认重置',
        danger: true,
        onConfirm: () => this.resetAT()
      });
    },

    resetAT() {
      if (this.isLoading) return Promise.resolve();
      this.isLoading = true;
      return SimpleAdmin.Api.atData({ action: 'reset_at' })
        .then((data) => {
          if (data && data.ok === false) {
            this.notify('danger', this.t('操作失败'));
            return;
          }
          this.atCommandResponse = "";
          this.isClean = true;
          SimpleAdmin.Reboot.request();
        })
        .catch((error) => {
          console.error("错误: ", error);
          this.notify('danger', this.t('重置失败:') + ' ' + ((error && error.message) || this.t('未知错误')));
        })
        .finally(() => {
          this.isLoading = false;
        });
    },

    init() {
      this.loadHistory();
    },
  };
}

if (window.SimpleAdminSpaMode) {
  (window.SimpleAdmin.Pages = window.SimpleAdmin.Pages || {}).atcommands = simpleAtCommands;
} else {
  mountSimpleAdminVueApp(simpleAtCommands);
}
