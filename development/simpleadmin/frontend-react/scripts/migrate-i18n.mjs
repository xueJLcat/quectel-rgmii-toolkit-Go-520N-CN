#!/usr/bin/env node
/**
 * migrate-i18n.mjs — 把 Vue 版 SimpleAdmin「中文原文作 key」的扁平字典迁移为
 * react-i18next 命名空间 JSON。仅用 Node 标准库(node:vm/node:fs/node:path),Node >= 20。
 *
 * 用法(在 frontend-react/ 目录):
 *   node scripts/migrate-i18n.mjs
 *
 * 输入:  ../www/js/simpleadmin-lang.js   (SimpleAdmin.Lang 的 translations.en:中文 key → 英文 value)
 * 输出:  src/lib/i18n/locales/zh-CN/<ns>.json   (<id>: 中文原文)
 *        src/lib/i18n/locales/en/<ns>.json      (<id>: 英文翻译)
 *        src/lib/i18n/locales/manifest.json     (ns 列表 + 条目统计,供测试与壳层断言)
 *
 * 幂等:同输入必然产生字节一致的输出(无时间戳;键序 = 源字典插入序,稳定可重复执行)。
 *
 * 流程:
 *   1. 用 node:vm 造 window/document/localStorage 桩完整加载 simpleadmin-lang.js
 *      (桩做法同 windows-test/frontend_smoke.js,另补 createTreeWalker/NodeFilter
 *      以便 setLanguage 触发的 DOM apply 在桩环境安全走完),得到 SimpleAdmin.Lang;
 *   2. 从源码中按「字符串感知的花括号配对」提取 translations 对象字面量并在 vm 中求值,
 *      得到 en 字典;再 setLanguage("en") 后逐条校验 Lang.t(中文) === 字典 value,
 *      证明提取完整且与运行时行为一致;
 *   3. 命名空间分配:NS_OVERRIDES(人工校正表)→ NS_KEYWORD_RULES(关键词启发式)→ common;
 *   4. 键 id:KEY_ID_OVERRIDES(人工小表)→ 英文 value slug 化驼峰(如 "Signal Details" →
 *      signalDetails);同 ns 冲突追加中文 hash4 后缀;英文缺失/纯符号用 legacy_<序号>
 *      并计入 warning 清单,保证零条目丢失;
 *   5. 动态句式模板(simpleadmin-lang.js translateForLanguage 的正则分支)转为 i18next
 *      插值键写入 common(对照表见 INTERPOLATION_ENTRIES 注释);
 *   6. 写 JSON + manifest.json,打印统计(总数/各 ns/warnings),并断言零丢失。
 *
 * 动态句式模板 → 插值键对照(源正则 → common 键,zh-CN 模板 | en 模板):
 *   /^当前：(.*)$/                        → common:currentValue           当前：{{value}} | Current: {{value}}
 *   /^获取(.+)中\.\.\.$/                  → common:getting                获取{{thing}}中... | Getting {{thing}}...
 *   /^已激活卡(\d+)$/                     → common:simActive              已激活卡{{index}} | Active SIM {{index}}
 *   /^未激活卡(\d+)$/                     → common:simInactive            未激活卡{{index}} | Inactive SIM {{index}}
 *   /^选择短信 (\d+)$/                    → common:selectSms              选择短信 {{index}} | Select message {{index}}
 *   /^第 (\d+) 行 DNS 服务器格式无效$/    → common:dnsLineInvalid         第 {{line}} 行 DNS 服务器格式无效 | DNS server on line {{line}} has an invalid format
 *   /^请完整填写 (\d+) 组 EARFCN 和 PCI$/ → common:earfcnGroupsIncomplete 请完整填写 {{groups}} 组 EARFCN 和 PCI | Please fill in all {{groups}} EARFCN and PCI groups
 *   /^短信发送失败：(.*)$/                → common:smsSendFailedDetail    短信发送失败：{{reason}} | SMS sending failed: {{reason}}
 * 注意:i18next 中 {{count}} 触发复数逻辑,插值变量统一避开 count 命名。
 */
import console from "node:console";
import fs from "node:fs";
import path from "node:path";
import { clearTimeout, setTimeout } from "node:timers";
import { fileURLToPath } from "node:url";
import vm from "node:vm";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const FRONTEND_DIR = path.resolve(SCRIPT_DIR, "..");
const LANG_JS = path.resolve(FRONTEND_DIR, "../www/js/simpleadmin-lang.js");
const LOCALES_DIR = path.join(FRONTEND_DIR, "src", "lib", "i18n", "locales");
const LANGUAGES = ["zh-CN", "en"];
const DEFAULT_NAMESPACE = "common";
const NAMESPACES = [
  "nav",
  "common",
  "dashboard",
  "signal",
  "network",
  "celllock",
  "netdetail",
  "netconfig",
  "firewall",
  "sms",
  "atcommands",
  "console",
  "automation",
  "sysmon",
  "settings",
  "deviceinfo",
  "login",
];

// ---------------------------------------------------------------------------
// 命名空间关键词启发式:按顺序检查,先命中先得(专用词在前,泛用词在后)。
// 覆盖不了的条目由 NS_OVERRIDES 人工校正,两者都未命中则落入 common。
// ---------------------------------------------------------------------------
const NS_KEYWORD_RULES = [
  [
    /看门狗|定时重启|重启时刻|时间同步|系统时间|自愈|探测目标|冷却|NTP|同步|轮询|Webhook|回调|自动化|连续失败|检测间隔|重注册/,
    "automation",
  ],
  [/防火墙|DMZ|阻止|放行|端口|规则|数据包|字节|默认策略/, "firewall"],
  [/短信|收件箱|收件人|发件人|发信息/, "sms"],
  [/小区|SCS|频点|EARFCN|earfcn|pci|扫描/i, "celllock"],
  [
    /精简显示|完整显示|刷新频率|在线时长|累计流量|下载速率|上传速率|时间范围|信号评估|互联网连接|网络信息|激活 SIM|信号百分比|RAM|温度/,
    "dashboard",
  ],
  [/DNS|IP 透传|USB|MAC|网关|静态绑定|上游/, "netconfig"],
  [/频段|锁频|NR5G|LTE|NSA|4G|5G|配置档|档名|网络模式|首选网络/, "network"],
  [/进程|内存|CPU/i, "sysmon"],
  [/IMEI|密码|TTL|语言|主题|暗夜|浅色|重启|关机|账户安全|界面偏好/, "settings"],
  [/登录|账号|认证/, "login"],
  [/AT ?命令|AT 终端|AT&F|AT ?数据/, "atcommands"],
  [/制造商|型号|固件|电话号码|本机号码/, "deviceinfo"],
  [/接口|租期|主机名|累计上行|累计下行|在线设备|局域网设备/, "netdetail"],
  [/天线|聚合|载波|信号|网络制式|服务小区/, "signal"],
  [/控制台/, "console"],
  [/导航|菜单/, "nav"],
];

// ---------------------------------------------------------------------------
// 关键词启发式误判的人工校正表:依据 www/index.html 各 section[data-page] 与
// www/js/pages/*.js、login.html 中文案的实际归属逐条核对得出(见脚本头注释流程 3)。
// 导航组名/页面名 → nav;跨页面复用的泛用词 → common;其余归实际出现的页面 ns。
// ---------------------------------------------------------------------------
const NS_OVERRIDE_GROUPS = [
  [
    "nav",
    [
      "首页",
      "网络",
      "控制台",
      "设备信息",
      "总览",
      "蜂窝网络",
      "系统设置",
      "短信服务",
      "退出登录",
      "AT 命令",
      "自动化",
      "系统监控",
      "小区锁定",
      "网络设置",
      "网络详情",
      "信号详情",
      "防火墙",
      "监控",
      "通信",
      "工具",
      "系统",
    ],
  ],
  [
    "dashboard",
    [
      "SIM 卡",
      "运营商",
      "网络模式",
      "速率",
      "信号与流量趋势",
      "暂无历史数据，页面保持打开后将自动积累",
      "信号强度",
      "优秀",
      "良好",
      "一般",
      "差",
      "无信号",
      "已激活",
      "未激活",
      "已连接",
      "未连接",
      "未知时间",
    ],
  ],
  [
    "signal",
    [
      "频点",
      "PCC PCI值:",
      "SCC PCI值:",
      "服务小区",
      "更新于",
      "物理小区标识 (PCI)",
      "小区 ID",
      "角色",
    ],
  ],
  [
    "network",
    [
      "网络工具",
      "IP 协议类型",
      "保存更改",
      "复选框将会在这里生成",
      "锁定当前选中",
      "启用所有",
      "取消选中",
      "没有做出更改",
      "自动",
      "未锁定",
      "未禁用",
      "禁用SA",
      "全部选中",
      "锁频/锁小区配置档",
      "保存当前配置",
      "恢复全部",
      "蜂窝设置",
      "网络设置未就绪，请稍后重试",
    ],
  ],
  [
    "celllock",
    [
      "清除",
      "初始化网络...",
      "选择",
      "信号",
      "请输入所有必填字段",
      "解锁LTE",
      "解锁NR5G-SA",
      "正在解析LTE数据",
      "正在解析NR5G-SA数据",
      "模块尚未就绪,请稍后重试",
      "锁定失败",
      "解锁失败",
      "参数必须为纯数字",
      "锁定模式",
      "操作成功，正在刷新状态",
    ],
  ],
  ["netdetail", ["WAN / LAN 地址", "LAN 网关", "当前速率", "网络详情加载失败", "不足1分钟"]],
  [
    "netconfig",
    [
      "当前：已启用",
      "当前：未启用",
      "未指定",
      "ECM (推荐)",
      "更改",
      "LAN IP 设置",
      "起始地址",
      "结束地址",
      "当前状态",
      "系统托管",
      "LAN IP 段",
      "起始/结束地址必须是 1-254 的整数！",
      "待重启生效",
      "操作失败，已取消后续步骤",
      "网络设置保存失败",
      "起始地址不能大于结束地址",
      "静态地址绑定",
      "IP 地址",
      "IP 地址，例如 192.168.5.20",
      "为设备固定分配 IP（DHCP 静态租约），保存后立即生效；固件持久化，设备重启后保留；最多 10 条。",
      "自定义",
      "跟随运营商",
      "每行一个，例如 223.5.5.5",
      "启用自定义",
      "恢复运营商",
      "删除后该设备将恢复动态分配 IP。",
      "IP 地址格式无效",
      "该 IP 地址已绑定",
    ],
  ],
  [
    "firewall",
    [
      "请输入有效的 IP 地址！",
      "应用更改",
      "有未应用的更改",
      "已应用",
      "应用失败",
      "类型",
      "目标",
      "协议",
      "入接口",
      "出接口",
      "源地址",
      "目的地址",
      "详情",
      "例如 8080",
    ],
  ],
  [
    "sms",
    [
      "全选",
      "内容",
      "时间",
      "日期和时间:",
      "未检测到 SIM 卡",
      "号码或内容不能为空",
      "发送失败",
      "删除失败",
    ],
  ],
  [
    "atcommands",
    [
      "用分号（;）分隔多个命令，模块会按组合命令一次性处理，示例：AT+CFUN?;+CCID",
      "重置",
      "常用命令",
      "命令历史",
      "暂无历史记录",
      "复制",
      "已复制",
      "复制失败",
      "危险操作",
      "确认重置",
      "↑ / ↓ 键可翻阅历史命令",
      "这将把调制解调器 AT 配置恢复出厂。继续？",
      "模块信息",
      "LAN 配置",
      "这将把调制解调器 AT 配置恢复出厂。",
      "清除历史",
      "重置失败:",
    ],
  ],
  ["console", ["重载"]],
  [
    "automation",
    [
      "直接重启模块",
      "每行一个，格式 地址 或 地址:端口，留空使用内置目标",
      "留空时使用内置公共 DNS 探测（223.5.5.5 / 1.1.1.1 / 8.8.8.8，端口 53）；自定义目标最多 4 个，非法条目会被忽略。",
      "从未",
      "重启模块",
      "偏差",
      "短信转发",
      "短信转发配置加载失败",
    ],
  ],
  ["sysmon", ["实时负载", "负载", "运行时长", "排序", "用户", "系统监控数据加载失败"]],
  [
    "settings",
    [
      "更新",
      "访问",
      "代码库",
      "文档",
      "以获取更多信息。版权所有。",
      "其他设置",
      "顶部的切换按钮只影响当前浏览器；此默认值在新浏览器访问或清除缓存后生效。",
      "中文",
      "设备操作",
      "系统状态获取失败",
      "已本地生效，服务器保存失败",
      "退出并重新登录",
      "知道了",
    ],
  ],
  ["deviceinfo", ["设备信息读取失败", "局域网IP", "广域网IPv", "版本", "未插卡"]],
  [
    "login",
    [
      "网络连接失败，请检查网络后重试",
      "会话已过期",
      "用户名",
      "密码",
      "请输入账号和密码",
      "用户名或密码错误",
      "尝试次数过多，请稍后重试",
      "尝试次数过多，请 ",
      "秒后重试",
      "秒后可重试",
    ],
  ],
];
const NS_OVERRIDES = Object.fromEntries(
  NS_OVERRIDE_GROUPS.flatMap(([ns, list]) => list.map((zh) => [zh, ns])),
);

// ---------------------------------------------------------------------------
// 键 id 人工小表:处理同 ns 内英文 value slug 冲突与不便作键的 slug。
// 未列出的条目一律用英文 value 的 slug(见 slugify)。
// ---------------------------------------------------------------------------
const KEY_ID_OVERRIDES = {
  "获取中...": "fetching", // 与 加载中...(loading) 同译 Loading...,按中文语义区分
  加载中: "loadingShort", // 与 加载中... 冲突
  读取设置失败: "failedToReadSettings", // 与 设置读取失败(failedToLoadSettings) 同译
  "发件人:": "senderLabel", // 与 发件人(sender) 同译 Sender
  "短信发送失败：": "smsSendingFailedPrefix", // 与 短信发送失败 同译;此为拼接前缀
  锁定频段: "lockBands", // 与 频段锁定(bandLock) 同译 Band Lock;此为动作
  秒后重试: "secondsBeforeRetry", // 与 秒后可重试 同译 seconds;登录倒计时拼接片段
  秒后可重试: "secondsUntilRetry",
  未启用: "notEnabled", // 与 已禁用(disabled) 同译 Disabled
  不足1分钟: "underOneMinute", // slug "<1 min" → 1Min 不宜作键
  "内存%": "memPercent", // slug "Mem%" → mem 丢失百分号语义
};

// 动态句式模板 → i18next 插值键(对照表见文件头注释),统一写入 common。
const INTERPOLATION_ENTRIES = [
  { id: "currentValue", zh: "当前：{{value}}", en: "Current: {{value}}" },
  { id: "getting", zh: "获取{{thing}}中...", en: "Getting {{thing}}..." },
  { id: "simActive", zh: "已激活卡{{index}}", en: "Active SIM {{index}}" },
  { id: "simInactive", zh: "未激活卡{{index}}", en: "Inactive SIM {{index}}" },
  { id: "selectSms", zh: "选择短信 {{index}}", en: "Select message {{index}}" },
  {
    id: "dnsLineInvalid",
    zh: "第 {{line}} 行 DNS 服务器格式无效",
    en: "DNS server on line {{line}} has an invalid format",
  },
  {
    id: "earfcnGroupsIncomplete",
    zh: "请完整填写 {{groups}} 组 EARFCN 和 PCI",
    en: "Please fill in all {{groups}} EARFCN and PCI groups",
  },
  {
    id: "smsSendFailedDetail",
    zh: "短信发送失败：{{reason}}",
    en: "SMS sending failed: {{reason}}",
  },
];

// --- vm 桩:加载 simpleadmin-lang.js(做法同 windows-test/frontend_smoke.js) ---
function createWindowStub() {
  const storage = new Map();
  const storageStub = {
    getItem: (key) => (storage.has(String(key)) ? storage.get(String(key)) : null),
    setItem: (key, value) => storage.set(String(key), String(value)),
    removeItem: (key) => storage.delete(String(key)),
  };
  const documentStub = {
    readyState: "loading",
    nodeType: 9,
    addEventListener: () => {},
    removeEventListener: () => {},
    querySelector: () => null,
    querySelectorAll: () => [],
    createElement: () => ({ style: {}, setAttribute() {}, appendChild() {} }),
    createTreeWalker: () => ({ currentNode: null, nextNode: () => false }),
    documentElement: { lang: "zh-CN", setAttribute() {} },
    body: { appendChild() {}, classList: { add() {}, remove() {} } },
  };
  const windowStub = {
    location: { protocol: "http:", host: "127.0.0.1:18080", href: "", replace() {}, hash: "" },
    document: documentStub,
    localStorage: storageStub,
    sessionStorage: storageStub,
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => true,
    CustomEvent: function (name, opts) {
      this.name = name;
      this.detail = opts && opts.detail;
    },
    MutationObserver: function () {
      this.observe = () => {};
      this.disconnect = () => {};
    },
    NodeFilter: { SHOW_TEXT: 4, FILTER_ACCEPT: 1, FILTER_REJECT: 2 },
    navigator: { language: "zh-CN" },
    setTimeout,
    clearTimeout,
    console,
  };
  windowStub.window = windowStub;
  windowStub.globalThis = windowStub;
  return windowStub;
}

// 字符串感知的花括号配对,提取 `const translations = {...}` 对象字面量源码。
function extractObjectLiteral(src, marker) {
  const at = src.indexOf(marker);
  if (at < 0) throw new Error(`marker not found in source: ${marker}`);
  const start = src.indexOf("{", at);
  if (start < 0) throw new Error(`object literal not found after marker: ${marker}`);
  let depth = 0;
  let quote = null;
  for (let i = start; i < src.length; i++) {
    const c = src[i];
    if (quote) {
      if (c === "\\") i++;
      else if (c === quote) quote = null;
      continue;
    }
    if (c === "'" || c === '"' || c === "`") {
      quote = c;
      continue;
    }
    if (c === "{") depth++;
    else if (c === "}" && --depth === 0) return src.slice(start, i + 1);
  }
  throw new Error(`unbalanced object literal after marker: ${marker}`);
}

function loadLangModule() {
  if (!fs.existsSync(LANG_JS)) {
    throw new Error(`dictionary source not found: ${LANG_JS}`);
  }
  const source = fs.readFileSync(LANG_JS, "utf8");
  const context = vm.createContext(createWindowStub());
  vm.runInContext(source, context, { filename: "simpleadmin-lang.js" });
  const lang = context.SimpleAdmin && context.SimpleAdmin.Lang;
  if (!lang || typeof lang.t !== "function") {
    throw new Error("SimpleAdmin.Lang not available after loading simpleadmin-lang.js");
  }
  const translations = vm.runInContext(
    `(${extractObjectLiteral(source, "const translations = ")})`,
    context,
  );
  const enDict = translations && translations.en;
  if (!enDict || typeof enDict !== "object" || Object.keys(enDict).length === 0) {
    throw new Error("translations.en dictionary is empty or missing");
  }
  // 交叉校验:切到 en 后运行时 t() 必须逐条等于提取出的字典 value(证明零丢失提取)。
  void lang.setLanguage("en");
  const mismatches = Object.keys(enDict).filter((key) => lang.t(key) !== enDict[key]);
  if (mismatches.length > 0) {
    throw new Error(
      `Lang.t mismatch for ${mismatches.length} keys, e.g. ${JSON.stringify(mismatches.slice(0, 3))}`,
    );
  }
  return enDict;
}

// FNV-1a(32bit)前 4 位十六进制,用于 id 冲突消歧,确定性且幂等。
function hash4(text) {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h.toString(16).padStart(8, "0").slice(0, 4);
}

// 英文 value → 语义化 camelCase slug:"Signal Details" → signalDetails。
function slugify(value) {
  const words = String(value)
    .replace(/[^a-zA-Z0-9]+/g, " ")
    .trim()
    .split(/\s+/)
    .filter(Boolean);
  if (words.length === 0) return "";
  return words
    .map((w, i) => (i === 0 ? w.toLowerCase() : w[0].toUpperCase() + w.slice(1).toLowerCase()))
    .join("");
}

function assignNamespace(zh) {
  const override = NS_OVERRIDES[zh];
  if (override) return { ns: override, via: "override" };
  for (const [pattern, ns] of NS_KEYWORD_RULES) {
    if (pattern.test(zh)) return { ns, via: "keyword" };
  }
  return { ns: DEFAULT_NAMESPACE, via: "fallback" };
}

function main() {
  const enDict = loadLangModule();
  const zhKeys = Object.keys(enDict);
  const warnings = [];
  const viaCount = { override: 0, keyword: 0, fallback: 0 };
  const usedIds = new Map(NAMESPACES.map((ns) => [ns, new Set()]));
  // entries: ns -> [{ id, zh, en }],保持字典插入序,保证幂等。
  const entries = new Map(NAMESPACES.map((ns) => [ns, []]));
  let legacySeq = 0;

  for (const zh of zhKeys) {
    const en = enDict[zh];
    const { ns, via } = assignNamespace(zh);
    viaCount[via]++;
    let base = KEY_ID_OVERRIDES[zh] || slugify(en);
    if (!base) {
      legacySeq++;
      base = `legacy_${legacySeq}`;
      warnings.push(`legacy id (英文 value 缺失或为纯符号): ${JSON.stringify(zh)} → ${ns}:${base}`);
    }
    const used = usedIds.get(ns);
    let id = base;
    if (used.has(id)) {
      id = `${base}_${hash4(zh)}`;
      warnings.push(`id collision (已加 hash4 后缀): ${JSON.stringify(zh)} → ${ns}:${id}`);
    }
    let seq = 2;
    while (used.has(id)) id = `${base}_${seq++}`;
    used.add(id);
    entries.get(ns).push({ id, zh, en });
  }

  // 动态句式模板 → common 插值键。
  const commonUsed = usedIds.get(DEFAULT_NAMESPACE);
  for (const item of INTERPOLATION_ENTRIES) {
    if (commonUsed.has(item.id)) {
      throw new Error(`interpolation id conflicts with dictionary id: common:${item.id}`);
    }
    commonUsed.add(item.id);
    entries.get(DEFAULT_NAMESPACE).push({ id: item.id, zh: item.zh, en: item.en });
  }

  // 零丢失断言:字典条目全部落位 + 插值键全部写入。
  const assigned = [...entries.values()].reduce((sum, list) => sum + list.length, 0);
  const expected = zhKeys.length + INTERPOLATION_ENTRIES.length;
  if (assigned !== expected) {
    throw new Error(`entry loss detected: assigned ${assigned}, expected ${expected}`);
  }

  // 写文件:先清理语言目录中不在 NAMESPACES 的旧 json,保证重复执行结果一致。
  for (const lng of LANGUAGES) {
    const dir = path.join(LOCALES_DIR, lng);
    fs.mkdirSync(dir, { recursive: true });
    for (const file of fs.readdirSync(dir)) {
      if (file.endsWith(".json") && !NAMESPACES.includes(file.slice(0, -5))) {
        fs.unlinkSync(path.join(dir, file));
      }
    }
    for (const ns of NAMESPACES) {
      const bag = {};
      for (const { id, zh, en } of entries.get(ns)) bag[id] = lng === "zh-CN" ? zh : en;
      fs.writeFileSync(path.join(dir, `${ns}.json`), `${JSON.stringify(bag, null, 2)}\n`);
    }
  }

  const counts = Object.fromEntries(NAMESPACES.map((ns) => [ns, entries.get(ns).length]));
  const manifest = {
    source: path.relative(FRONTEND_DIR, LANG_JS).split(path.sep).join("/"),
    defaultNamespace: DEFAULT_NAMESPACE,
    languages: LANGUAGES,
    namespaces: NAMESPACES,
    counts,
    dictionaryEntries: zhKeys.length,
    interpolationEntries: INTERPOLATION_ENTRIES.length,
    interpolationKeys: INTERPOLATION_ENTRIES.map((item) => item.id),
    total: assigned,
  };
  fs.mkdirSync(LOCALES_DIR, { recursive: true });
  fs.writeFileSync(
    path.join(LOCALES_DIR, "manifest.json"),
    `${JSON.stringify(manifest, null, 2)}\n`,
  );

  // 统计输出。
  const log = (msg) => console.log(`[migrate-i18n] ${msg}`);
  log(`source: ${manifest.source} (${zhKeys.length} 条字典条目, vm 桩加载 + t() 交叉校验通过)`);
  log(
    `ns 分配: override=${viaCount.override} keyword=${viaCount.keyword} fallback-common=${viaCount.fallback}`,
  );
  log(
    `插值键: ${INTERPOLATION_ENTRIES.length} → ${DEFAULT_NAMESPACE} (${INTERPOLATION_ENTRIES.map((i) => i.id).join(", ")})`,
  );
  for (const ns of NAMESPACES) log(`  ns ${ns}: ${counts[ns]}`);
  log(`总条目: ${assigned} (zh-CN ${assigned} / en ${assigned}), 丢失: 0`);
  log(`warnings: ${warnings.length}`);
  for (const w of warnings) log(`  - ${w}`);
  log(
    `wrote: ${path.relative(FRONTEND_DIR, LOCALES_DIR)}/{${LANGUAGES.join(",")}}/<ns>.json + manifest.json`,
  );
}

main();
