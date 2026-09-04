#!/usr/bin/env node
// 前端模块装配冒烟测试:按 index.html 加载顺序执行全部 SimpleAdmin 模块脚本,
// 验证 window.SimpleAdmin 命名空间完整、关键方法存在。
// 用法(仓库根目录或任意目录):node windows-test/frontend_smoke.js
'use strict';

const fs = require('fs');
const path = require('path');
const vm = require('vm');

const WWW = path.join(__dirname, '..', 'development', 'simpleadmin', 'www');
// 按 index.html 的实际加载顺序装配全部模块(含页面工厂),任何页面脚本的
// 顶层语法错误或注册缺失都会让冒烟失败。
const FILES = [
  'band_map.js',
  'vue.global.prod.js',
  'simpleadmin-api.js', 'simpleadmin-brand.js', 'simpleadmin-text.js',
  'simpleadmin-time.js', 'simpleadmin-sms.js', 'simpleadmin-lang.js', 'simpleadmin-ui.js',
  'simpleadmin-reboot.js', 'simpleadmin-poll.js',
  'simpleadmin-mockat.js', 'simpleadmin-logout.js',
  'vue-app.js',
  'populate-checkbox.js',
  'pages/index.js', 'pages/signal.js', 'pages/network.js', 'pages/celllock.js', 'pages/netconfig.js', 'pages/firewall.js',
  'pages/netdetail.js', 'pages/atcommands.js', 'pages/settings.js', 'pages/automation.js', 'pages/sysmon.js', 'pages/sms.js', 'pages/deviceinfo.js',
  'simpleadmin-spa.js'
];

const listeners = {};
const documentStub = {
  readyState: 'loading',
  addEventListener: (ev, fn) => { (listeners[ev] = listeners[ev] || []).push(fn); },
  removeEventListener: () => {},
  querySelector: () => null,
  querySelectorAll: () => [],
  createElement: () => ({ style: {}, setAttribute() {}, appendChild() {} }),
  documentElement: { lang: 'zh-CN', setAttribute() {} },
  body: { appendChild() {}, classList: { add() {}, remove() {} } }
};
const storageStub = { getItem: () => null, setItem: () => {}, removeItem: () => {} };
const windowStub = {
  location: { protocol: 'http:', host: '127.0.0.1:18080', href: '', replace() {}, hash: '' },
  document: documentStub,
  localStorage: storageStub,
  sessionStorage: storageStub,
  addEventListener: () => {},
  removeEventListener: () => {},
  dispatchEvent: () => true,
  CustomEvent: function (name, opts) { this.name = name; this.detail = opts && opts.detail; },
  WebSocket: function () { throw new Error('no ws in smoke test'); },
  fetch: () => Promise.reject(new Error('no fetch in smoke test')),
  MutationObserver: function () { this.observe = () => {}; this.disconnect = () => {}; },
  navigator: { language: 'zh-CN' },
  console
};
windowStub.window = windowStub;
windowStub.globalThis = windowStub;
// index.html 头部内联脚本的等价物:启用 SPA 模式,页面脚本只注册工厂不直接挂载。
windowStub.SimpleAdminSpaMode = true;

const context = vm.createContext(windowStub);
for (const file of FILES) {
  const filePath = path.join(WWW, 'js', file);
  if (!fs.existsSync(filePath)) {
    console.error('FAIL: missing module file', filePath);
    process.exit(1);
  }
  const code = fs.readFileSync(filePath, 'utf8');
  vm.runInContext(code, context, { filename: file });
}

const SA = windowStub.SimpleAdmin;
if (!SA) { console.error('FAIL: SimpleAdmin namespace missing'); process.exit(1); }
const expect = {
  Api: ['getAT', 'refreshAT', 'getDashboardData', 'getDeviceInfo', 'networkData', 'atData', 'networkConfigData', 'firewallData', 'systemData', 'smsData', 'signalData', 'getTimeSync', 'setTimeSync', 'timeSyncNow', 'systemMonitor'],
  Brand: ['init'],
  Text: ['lines', 'compactHex'],
  Time: ['parseSmsDate'],
  Sms: ['parseConcatHeader'],
  Lang: [],
  UI: ['setText', 'notify', 'confirm', 'initTheme', 'getTheme', 'setTheme'],
  Reboot: ['request', 'countdown'],
  Poll: ['create', 'onPageReturn'],
  MockAT: [],
  Logout: ['start'],
  Vue: []
};
let failed = false;
for (const [mod, fns] of Object.entries(expect)) {
  if (!SA[mod]) { console.error(`FAIL: SimpleAdmin.${mod} missing`); failed = true; continue; }
  for (const fn of fns) {
    if (typeof SA[mod][fn] !== 'function') { console.error(`FAIL: SimpleAdmin.${mod}.${fn} missing`); failed = true; }
  }
}
if (typeof SA.Text.lines !== 'function' || SA.Text.lines('a\nb').length !== 2) {
  console.error('FAIL: Text.lines behavior');
  failed = true;
}
// 页面工厂与路由/挂载基础设施完整性。
const expectedPages = ['dashboard', 'signal', 'network', 'celllock', 'netconfig', 'firewall', 'netdetail', 'atcommands', 'settings', 'automation', 'sysmon', 'sms', 'deviceinfo'];
if (!SA.Pages) {
  console.error('FAIL: SimpleAdmin.Pages registry missing');
  failed = true;
} else {
  for (const page of expectedPages) {
    if (typeof SA.Pages[page] !== 'function') {
      console.error(`FAIL: SimpleAdmin.Pages.${page} factory missing`);
      failed = true;
    }
  }
}
if (!SA.Vue || typeof SA.Vue.mount !== 'function') {
  console.error('FAIL: SimpleAdmin.Vue.mount missing');
  failed = true;
}
if (!SA.Spa || typeof SA.Spa.showPage !== 'function') {
  console.error('FAIL: SimpleAdmin.Spa.showPage missing');
  failed = true;
}
if (failed) process.exit(1);
console.log('SMOKE_OK: all ' + FILES.length + ' modules assembled, namespace complete, ' + expectedPages.length + ' page factories registered');
