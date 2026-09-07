#!/usr/bin/env node
// =====================================================================
// check-size.mjs —— React 前端产物体积预算闸门(零依赖,需 node ≥ 20)
//
// 用法:
//   node scripts/check-size.mjs              # 检查 ../../www(即 development/simpleadmin/www)
//   node scripts/check-size.mjs --dist dist  # 直接检查 Vite 构建输出目录(CI 中 build 后
//                                            # www 未必同步;dist 与 www 结构一致,index.html 在根)
//
// 预算项(raw 字节,KB/MB 按 1024 进制):
//   ① 静态目录总字节数                                     ≤ 2.6MB
//   ② 初始壳层:index.html 引用的全部本地 /assets/*.{js,css}
//      (script src / modulepreload / stylesheet 合并去重),
//      其中 echarts-*/xterm-* 由 ③④ 专款检查,不计入本项      ≤ 1000KB
//   ③ assets/echarts-*.js 合计                              ≤ 560KB
//   ④ assets/xterm-*.js 合计(如存在)                        ≤ 450KB
//
// 阈值来源:设计方案 §8(初版 450/420/380,按 echarts 核心基线与实测上调为
// 600/560/400;② 因 vendor chunk 进入 modulepreload 图、实测约 903KB,再上调
// 为 1000KB,偏差已在方案风险节记录)。④ 因控制台引入 @xterm/addon-webgl(GPU
// 渲染,上下文不可用自动回落 DOM)与 @xterm/addon-search(回滚缓冲搜索)由
// 400KB 上调为 450KB(实测约 396KB,仍为懒加载专款 chunk、不入初始壳层)。
//
// 超限打印明细并 exit 1;全部通过打印各项实测值与余量,exit 0。
// =====================================================================
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const KB = 1024;
const MB = 1024 * KB;

// ---- 目标目录:缺省查 www;--dist <dir> 查构建产物(相对当前工作目录) ----
const scriptDir = dirname(fileURLToPath(import.meta.url));
const argv = process.argv.slice(2);
let target;
if (argv.length === 0) {
  target = resolve(scriptDir, "../../www");
} else if (argv[0] === "--dist" && argv.length === 2) {
  target = resolve(process.cwd(), argv[1]);
} else {
  console.error("用法: node check-size.mjs [--dist <dir>]");
  process.exit(2);
}

let indexHtml;
try {
  indexHtml = readFileSync(join(target, "index.html"), "utf8");
} catch (err) {
  console.error(`[错误] 无法读取 ${join(target, "index.html")}: ${err.message}`);
  process.exit(2);
}

const fmt = (bytes) => `${bytes} B (${(bytes / KB).toFixed(1)}KB)`;
const baseName = (urlPath) => urlPath.slice(urlPath.lastIndexOf("/") + 1);
const fileSize = (path) => statSync(path).size;

// ---- ① 目录树总字节数 ----
function treeBytes(dir) {
  let total = 0;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    total += entry.isDirectory() ? treeBytes(path) : fileSize(path);
  }
  return total;
}

// ---- ② 初始壳层:index.html 引用的本地 /assets/*.{js,css} ----
// 收集 src/href 属性(覆盖 <script src>、<link rel=modulepreload>、<link rel=stylesheet>)并去重
const assetRefs = [
  ...new Set(
    [...indexHtml.matchAll(/(?:\bsrc|\bhref)="([^"]+)"/g)]
      .map((m) => m[1])
      .filter((u) => /^\.?\/assets\/[^/]+\.(?:js|css)$/.test(u)),
  ),
];
// echarts/xterm 专款 chunk 由 ③④ 单独检查,② 中排除以免双重计数
const DEDICATED = /^(echarts|xterm)-.*\.js$/;

const missing = [];
const shellDetail = [];
let shellBytes = 0;
for (const ref of assetRefs) {
  const path = join(target, ref.replace(/^\.\//, "").replace(/^\//, ""));
  let size;
  try {
    size = fileSize(path);
  } catch {
    missing.push(ref);
    continue;
  }
  const dedicated = DEDICATED.test(baseName(ref));
  shellDetail.push(`    ${dedicated ? "[专款③④,不计入]" : "[壳层]"} ${ref} = ${fmt(size)}`);
  if (!dedicated) shellBytes += size;
}

// ---- ③④ assets/ 下按前缀匹配的专款 chunk ----
let assetEntries = [];
try {
  assetEntries = readdirSync(join(target, "assets"));
} catch {
  // assets 缺失时 ② 的 missing 检查会兜底报错
}
function dedicatedSum(prefix) {
  const re = new RegExp(`^${prefix}-.*\\.js$`);
  const files = assetEntries.filter((name) => re.test(name));
  const bytes = files.reduce((sum, name) => sum + fileSize(join(target, "assets", name)), 0);
  return { files, bytes };
}
const echarts = dedicatedSum("echarts");
const xterm = dedicatedSum("xterm");

// ---- 汇总与输出 ----
const items = [
  {
    label: "① 目录总字节数",
    actual: treeBytes(target),
    limit: 2.6 * MB,
    detail: () =>
      readdirSync(target, { withFileTypes: true }).map((entry) => {
        const path = join(target, entry.name);
        const bytes = entry.isDirectory() ? treeBytes(path) : fileSize(path);
        return `    ${entry.isDirectory() ? entry.name + "/" : entry.name} = ${fmt(bytes)}`;
      }),
  },
  {
    label: "② 初始壳层(index.html 引用,除 echarts/xterm 专款)",
    actual: shellBytes,
    limit: 1000 * KB,
    detail: () => shellDetail,
  },
  {
    label: "③ assets/echarts-*.js",
    actual: echarts.bytes,
    limit: 560 * KB,
    detail: () => echarts.files.map((name) => `    ${name} = ${fmt(fileSize(join(target, "assets", name)))}`),
  },
  {
    label: "④ assets/xterm-*.js(如存在)",
    actual: xterm.bytes,
    limit: 450 * KB,
    detail: () => xterm.files.map((name) => `    ${name} = ${fmt(fileSize(join(target, "assets", name)))}`),
  },
];

console.log(`[体积预算] 目标目录: ${target}`);
const failed = items.filter((item) => item.actual > item.limit);
for (const item of items) {
  const mark = item.actual <= item.limit ? "通过" : "超限";
  const margin = ((item.limit - item.actual) / KB).toFixed(1);
  console.log(`[${mark}] ${item.label}: 实测 ${fmt(item.actual)} / 预算 ${(item.limit / KB).toFixed(1)}KB / 余量 ${margin}KB`);
}

if (failed.length > 0) {
  console.error("[明细] 超限项构成:");
  for (const item of failed) {
    console.error(`  ${item.label}:`);
    for (const line of item.detail()) console.error(line);
  }
}
if (missing.length > 0) {
  console.error(`[错误] index.html 引用了不存在的产物: ${missing.join(", ")}`);
}
if (failed.length > 0 || missing.length > 0) {
  process.exit(1);
}
console.log("[完成] 体积预算全部通过");
