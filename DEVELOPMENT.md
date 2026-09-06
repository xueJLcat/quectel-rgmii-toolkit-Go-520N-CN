# SimpleAdmin 开发与迭代指南

> 面向后续迭代开发者(含 AI 代理)。覆盖架构、约定、构建、测试、部署与固件事实。
> 配套文档:[DEPLOY.md](DEPLOY.md)(部署细则与验收清单)、[AT_PROXY.md](AT_PROXY.md)(AT 代理协议,供外部程序解析)。
> 最后更新:2026-09-06(前端切换为 React 19 + Vite + Tailwind v4 + shadcn/ui 构建管线;后端新增网络诊断与防火墙 IPv6/DNAT 端口转发)。

## 1. 项目概述

SimpleAdmin 是 Quectel RG520N-CN 5G CPE(高通 SDX65 平台)的 Web 管理系统:

- **后端**:`simpleadmin-httpd`,Go 单体服务(静态编译、无外部依赖),运行于设备 `:80`。
  独占唯一 AT 通道 `/dev/smd11`,经 Unix Socket 代理对外暴露受限 AT 执行能力。
- **前端**:React 19 + TypeScript + Vite + Tailwind CSS v4 + shadcn/ui,源码在
  `development/simpleadmin/frontend-react/`,构建产物同步到 `development/simpleadmin/www/`
  提交入库,纯静态文件由后端托管。
- **设备环境**:Linux armv7,systemd;`/usrdata` 与 `/etc` 同为可写 ubifs 卷,rootfs 只读。
- 设备档案见运维资料库 `/opt/cpe-doc`(01 设备档案 / 02 Shell 接入 / 03 AT 通道)。

设备端布局:

```
/usrdata/simpleadmin/
├── simpleadmin-httpd          # 主二进制(由 simpleadmin-httpd.armv7 安装而来)
├── www/                       # 前端静态文件(config/ 内为界面语言/默认主题配置)
├── simpleadmin.auth           # 登录凭据(username:password,0600,升级保留)
├── at_devices.conf            # AT 设备列表(/dev/smd11)
├── firewall_ports.conf        # 防火墙阻止/放行规则状态文件
├── firewall_fwd.conf          # DNAT 端口转发规则状态文件
├── mac_bind.conf              # DHCP 静态租约状态文件(本服务下发记录)
├── dns_upstream.conf          # 自定义上游 DNS 状态文件
├── ttlvalue                   # TTL 状态
├── dnsmasq.d/upstream.conf    # 注入 dnsmasq 的上游 DNS 包含文件
├── backup/dnsmasq.conf.orig   # 厂商 dnsmasq 配置首次注入前备份
├── .deploy-backup/            # SSH 部署脚本的版本备份(可 --rollback)
└── at_proxy.sock              # AT 代理 Socket(服务启动时创建)
```

## 2. 仓库结构

```
quectel-rgmii-toolkit-Go/
├── Makefile                        # 构建入口(见 §5)
├── DEVELOPMENT.md                  # 本文档
├── DEPLOY.md                       # 部署方式/验收清单/回滚
├── AT_PROXY.md                     # AT 代理专项文档(机器可解析)
├── development/
│   ├── install_simpleadmin_go.sh   # 设备端安装脚本(三种部署方式共用)
│   ├── uninstall_simpleadmin_go.sh # 卸载(含 dnsmasq-cleanup 调用)
│   ├── deploy_via_ssh.sh           # SSH 一键部署(构建→备份→上传→验证→回滚)
│   ├── simpleadmin/
│   │   ├── simpleadmin-httpd.armv7 # ARMv7 产物(make arm)
│   │   ├── www/                    # 前端产物(提交入库,设备直接服务)
│   │   ├── systemd/simpleadmin-httpd.service
│   │   ├── simplepasswd
│   │   └── mobileap_bridge0_mac.sh
│   └── simpleadmin/frontend-react/ # React 前端源码(Vite 构建,产物同步进 www/)
├── go-build/simpleadmin-go/        # Go 模块(模块名含 ./cmd/simpleadmin-httpd)
│   └── cmd/simpleadmin-httpd/      # 全部后端代码(单 package main)
└── windows-test/                   # Windows 预览构建(mock 模式本地测试)
```

## 3. 后端架构(package main)

### 3.1 子命令

| 子命令 | 用途 |
|---|---|
| (无)/`serve` | 主服务:HTTP + WS 网关 + AT 代理 + 开机自愈任务 |
| `__smd-reader <dev>` | 主进程派生的 SMD 读取子进程(内部使用) |
| `ttl <on/off/value>` | TTL 规则管理(卸载脚本调用) |
| `at <命令>` | 直连串口执行 AT(运维诊断用;外部程序请用 `at-client`) |
| `at-client [--sock] [--timeout-ms] [--payload] <命令>` | 只连 Unix Socket 的 AT 客户端,退出码 0/1/2(见 AT_PROXY.md) |
| `dnsmasq-cleanup` | 卸载时清理本服务的 dnsmasq 注入(卸载脚本调用) |

### 3.2 文件分组约定

| 前缀/文件 | 职责 | 约定 |
|---|---|---|
| `main.go` / `server_*.go` | 入口、路由、认证、配置、TLS | 路由表在 `server_routes.go`;API 优先走 `/api/ws` 网关,`-direct-api` 才注册直连 HTTP 路由 |
| `page_*.go` | 各页面 API handler(`handleXxxData`)+ 响应解析纯函数 | 解析函数必须纯函数化以便单测 |
| `api_handlers.go` / `api_websocket.go` | 通用端点与 WS 网关 | 网关逐请求复核会话 |
| `at_runner.go` / `at_cache.go` / `at_cache_policy.go` | AT 执行链:事务、缓存、策略分级 | 见 §3.3 |
| `at_parse_util.go` / `at_command_util.go` | AT 响应解析工具 | `atLines`/`isQMAPRecord`/`csvFields`/`atResponseOK` 等 |
| `native_*.go` + `native_*_stub.go` | 平台相关实现(**必须成对**):`//go:build linux` 与 `//go:build !linux` 占位 | 保证 Windows 预览版可编译;占位实现返回明确错误 |
| `mock_*.go` | `-mock` 模式的预制响应 | **必须模拟真实固件行为**(含怪癖,见 §8) |
| `*_test.go` | 表驱动单测 + e2e mock 测试 | 见 §6 |

### 3.3 AT 执行链(核心机制)

```
页面 action / at-client / 代理
        │
        ▼
全局文件锁( flock /tmp/simpleadmin-go-at.lock,可用 SIMPLEADMIN_AT_LOCK_FILE 覆盖;
             等待上限 atGlobalLockAcquireTimeout=200s,必须 ≥ 最大命令超时 )
        │
        ▼
每设备互斥( atDeviceLockRegistry )→ 串口事务(写命令 → 读至终结行)
```

- **读命令缓存**:`atCommandCache` 按命令串为键;分级策略在 `at_cache_policy.go`
  (`atCacheConfigMaxAge=60s` 等),新增命令串要检查分类归属。
- **开机保护期**:`AT+CFUN=1,1` 后 35 秒(`atCacheBootGracePeriod`)内读命令延迟执行并返回
  pending 文本 `atCachePendingText`;**动作命令不受保护期延迟**。
- **动作命令判定**:`isATActionCommand`(模式匹配,大写比较)。新增写命令必须把模式加进去,
  否则会被当读命令入队延迟,调用方已返回失败而命令稍后才真正执行。
- **结果判定语义**(重要):
  - `atResponseOK`:**按行判定**——存在独立 `OK` 行且无终结错误行(`ERROR`/`+CME ERROR`/`+CMS ERROR`)。
    子串 `Contains("OK")` 会被回显欺骗,已废弃该写法。
  - `atResponseRebootOK`:重启类命令(`AT+CFUN=1,1` 等)模块先重启后应答,
    **收不到应答是常态**;只有出现终结错误行才算失败。`set_imei`/`reboot` 等必须用它。
  - 组合命令某子命令失败 → 模块中止后续子命令并以 ERROR 收尾;**解析必须容忍尾部 ERROR**,
    已收到的字段照常使用(见 `page_readonly_error_test.go`)。

### 3.4 响应契约(API)

- **输入校验失败** → `400` + `{ok:false,error}`。
- **AT/运行时执行失败** → `200` + `{ok:false,error,...}`(前端统一消费 `ok` 字段)。
- **查询类失败绝不返回 `ok:true`**:返回 `error`/`pending`/`atReady:false` 等标记,
  由前端显示失败/重试,而不是把失败渲染成"空结果"。
- 状态端点的"读取失败"判定:**只有完全拿不到数据行才算失败**;
  尾部 ERROR 的部分成功必须按成功解析(谓词 `networkConfigStatusReadFailed`,有回归测试)。

### 3.5 状态文件与开机自愈

状态文件一律:JSON + 原子写(临时文件 → `Sync()` → rename,权限 0600)。
路径用 `defaultXxxFile` 常量 + `runtimeXxx` 包级变量(测试可注入临时目录)。

| 机制 | 说明 |
|---|---|
| `reconcileDNSUpstreamAtStartup()` | 上游 DNS 状态自愈:仅当状态启用且"标记块+包含文件"与状态不一致时才重新应用(避免每次服务重启瞬断 DNS);goroutine 执行不阻塞 HTTP 启动 |
| `app.reconcileMacBindAtStartup()` | 静态租约补齐:延迟 20s、重试 3 次、持 `macBindMu`;下发前做 IP 冲突预检;固件查询未就绪(非 `atResponseOK`)绝不盲目下发 |
| 互斥 | `macBindMu`(读-算-写全程)、`dnsUpstreamMu`(applyDNSUpstream 全程)——文件+外部服务事务必须整体互斥 |

### 3.6 dnsmasq 集成

配置链:`/var/run/data/dnsmasq.conf.bridge0`(tmpfs,QCMAP 生成)→ `/etc/data/dnsmasq.conf`
(厂商文件,出厂后不被重写)→ 我方标记块 `conf-file=/usrdata/simpleadmin/dnsmasq.d/upstream.conf`。

- **静态租约**:走固件私有命令 `AT+QMAP="MAC_bind"`(见 §8);固件写 `/etc/data/dhcp_hosts`
  但**不重载 dnsmasq**,我方以 `systemctl reload dnsmasq_service@0.service`
  (=厂商 ExecReload `killall -HUP dnsmasq`)使其生效。SIGHUP 只重读 hosts 文件。
- **上游 DNS**:修改配置链必须重启单元(`systemctl restart dnsmasq_service@0.service`);
  重启前先 `dnsmasq --test` 校验;失败回滚包含文件(持锁事务)。
- **注入规范**:标记块带起始/结束标记,幂等追加、完整移除;首次注入前备份厂商原文;
  块损坏(如只剩起始标记)要能检测并修复;包含文件必须先于标记块写入,任何时刻不得缺失。

### 3.7 AT 代理

协议与退出码契约见 **AT_PROXY.md**。要点:

- Socket 缺省 `/usrdata/simpleadmin/at_proxy.sock`(`-at-proxy-sock` 覆盖);SIGTERM 清理;
  启动时对遗留文件先探活,存活则拒启、不存活则删除重建。
- 单连接单请求,行分隔 JSON;`payload` 非空 = 交互事务(CMGS 等),复用 `runSMSTransaction`。
- 读请求后统一设写时限(10s),防半开连接泄漏协程。
- 预算关系:锁等待 200s + 执行 180s ≤ at-client 交换超时 400s。

## 4. 前端架构(React)

源码在 `development/simpleadmin/frontend-react/`:React 19 + TypeScript + Vite + Tailwind CSS v4 + shadcn/ui。
旧 Vue3 无构建 SPA(`www/js/*`、`window.SimpleAdmin` 命名空间)与旧 Tailwind 源码目录
`development/simpleadmin/frontend/` 已全部退役删除。

### 4.1 目录结构

```
frontend-react/
├── index.html / login.html     # Vite MPA 双入口(保留 __SA_VERSION__/__SA_THEME__ 占位符)
├── vite.config.ts              # 双入口构建 + manualChunks 分包 + dev proxy
├── src/
│   ├── app/                    # App.tsx / LoginApp.tsx / providers.tsx / routes.tsx(导航单一来源)
│   ├── lib/
│   │   ├── api/                # gateway.ts(/api/ws 网关)+ endpoints.ts(API 封装)+ auth.ts
│   │   ├── i18n/               # react-i18next + locales/{zh-CN,en}/ 18 个命名空间 JSON
│   │   ├── theme/              # 亮暗主题读写
│   │   └── utils.ts
│   ├── components/
│   │   ├── ui/                 # shadcn/ui 基础组件
│   │   ├── layout/             # 侧栏/顶栏/页面骨架
│   │   ├── common/             # 空态/错误重试/危险区等跨页组件
│   │   └── charts/             # ECharts 按需封装
│   ├── features/<id>/          # 16 个功能域:page.tsx + hooks.ts + lib.ts + components/
│   ├── stores/                 # zustand:ui / confirm / reboot
│   └── styles/                 # tokens.css(设计令牌)+ globals.css(Tailwind 入口)
├── e2e/                        # Playwright 端到端测试(make web-e2e)
└── scripts/                    # sync-www.sh(dist→www 同步)+ migrate-i18n.mjs(迁移溯源)
```

- 16 个 feature 域 = 15 条路由(dashboard/sysmon/signal/network/celllock/netdetail/netconfig/
  firewall/sms/atcommands/console/diag/automation/settings/deviceinfo)+ login。
- 路由用 **HashRouter**;`src/app/routes.tsx` 是导航单一来源(菜单/页面标题/浏览器标题/路由 id),
  feature 页经 `lazy` 懒加载,Vite 按页分 chunk(echarts/xterm/i18n 独立公共 chunk)。
- 数据层用 **TanStack Query**:查询/变更/轮询集中在各域 `hooks.ts`;
  全局 UI 状态(侧栏、确认框、重启倒计时)用 **zustand**(`src/stores/`);
  动画用 motion;toast 用 sonner。

### 4.2 API 传输(协议不变)

- 所有 `/api/*` 业务请求仍走 **`/api/ws` WebSocket 网关**,协议与旧版一致;
  客户端实现在 `src/lib/api/gateway.ts`(请求 id 匹配、断线重连退避、单请求超时)。
- 保活:每 **15 分钟**调一次 `/api/get_uptime`,保持会话与连接活性。
- 401 处理:把当前 `location.hash` 存入 `sessionStorage` 后跳 `/login.html`,登录成功后回跳原页面。
- 例外:`/api/module_model` 为公开端点,直接 HTTP fetch(不走网关)。
- 新增 API 封装统一加在 `src/lib/api/endpoints.ts`,禁止绕过网关直连 HTTP。

### 4.3 交互约定

- 操作反馈一律用 toast(sonner),禁用浏览器 alert。
- 危险操作走 `stores/confirm.ts` 确认框;重启类流程用 `stores/reboot.ts`,
  **失败分支必须取消倒计时**(否则倒计时结束会误弹成功提示)。
- 加载失败必须有可见失败态 + 重试按钮,**不得静默显示空/默认值**;
  后台轮询救回数据时要复位失败标志。
- 长时操作用 mutation pending 态锁按钮,`finally`/`onSettled` 复位。

### 4.4 i18n

- **react-i18next 命名空间 JSON**:`src/lib/i18n/locales/{zh-CN,en}/` 各 18 个命名空间
  (common/nav/login + 每个 feature 域一个,含新增 `diag`);命名空间经 `locales/manifest.json` 注册。
- 新增可见文案必须在同一命名空间的 zh-CN 与 en **成对增键**,不得只加一边。
- 迁移脚本 `scripts/migrate-i18n.mjs`:从旧 `www/js/simpleadmin-lang.js` 生成命名空间 JSON;
  源字典已随旧管线退役,脚本保留作键溯源。
- 语言持久化:`localStorage` + 后端 `/api/set_language`(设备级配置 `www/config/get_language.json`)。

### 4.5 主题与占位符

- 亮暗主题经 `lib/theme` + `data-bs-theme` 属性;设备级默认主题存 `www/config/get_theme.json`。
- 占位符契约(服务端响应时替换,**源码与构建产物必须保留字面量**):
  - `__SA_VERSION__`:新前端经 `<meta name="sa-version">` 读取。
  - `__SA_THEME__`:位于入口 HTML 的 `data-bs-theme`,配合防闪脚本,
    无本地主题记录的浏览器首屏即按设备默认主题渲染。
- 认证白名单含 **`/assets/` 前缀**(Vite 内容寻址产物,登录页依赖,不含会话数据);
  HTML 入口仍受会话保护——见后端 `server_auth.go` 的 `isPublicAuthPath`。

### 4.6 构建与同步

- 构建链:`frontend-react/` → `npm run build`(tsc --noEmit + vite build)→ `dist/` →
  `scripts/sync-www.sh` → `development/simpleadmin/www/`(**提交入库**,设备直接服务,运行时无构建)。
- `sync-www.sh` 清空 www/ 后全量复制,但 **`www/config/` 整目录保留**(运行时可写区:
  `get_theme.json`/`get_language.json`);缺失时创建并写入默认值;同步后校验 `www/index.html` 存在。
- 体积预算:www 由约 824KB 增至约 2.2MB(React+ECharts+xterm 分包 + 路由级懒加载);
  `make web-size` 检查初始壳层/echarts chunk/xterm chunk/总量四道预算闸门,任一超标即失败。
  LAN 场景(本地加载、不走公网)的权衡已在设计文档记录。

### 4.7 测试策略

- 单元/组件测试:**vitest + @testing-library/react**(jsdom),测试文件与源码同目录 `*.test.ts(x)`;
  解析/校验纯函数抽在各域 `lib.ts` 便于独立测试。`make web-test` = typecheck + lint + vitest。
- 端到端:**Playwright**(`e2e/`,`make web-e2e`);webServer 自起 `make dev-mock`
  (linux 原生 mock 二进制,:18080)。
- 旧 `windows-test/frontend_smoke.js`(模块装配冒烟)已删除,由 vitest + Playwright 取代。

### 4.8 开发回路

```
make dev-mock-build && make dev-mock   # linux 原生 mock 后端 :18080
make web-dev                           # Vite dev server :5173,/api 与 /console 代理到 :18080(含 WS)
```

Windows 侧等价 mock 后端为 `run_windows_test.bat`。开发时 dev server 热更新即时生效;
提交前跑 `make web-test`/`make web-size`,再 `make web` 同步产物入库。

### 4.9 新增页面流程

1. `src/features/<id>/page.tsx`:页面组件 **default export**(按需配 `hooks.ts`/`lib.ts`/`components/`)。
2. `src/app/routes.tsx`:在 `PAGES` 表注册(id/path/groupId/icon/titleKey/lazy)。
3. `locales/{zh-CN,en}/<id>.json`:双语**成对增键**,并在 `manifest.json` 注册命名空间。
4. `src/lib/api/endpoints.ts`:新增 API 封装(经网关)。
5. 验证:`make web-test` + `make web-size`;涉及新路由/关键交互补 `e2e/` 用例跑 `make web-e2e`。

## 5. 构建与验证

```
make all          # 默认目标(arm + windows)
make arm          # ARMv7 交叉编译 → development/simpleadmin/simpleadmin-httpd.armv7
make windows      # Windows amd64 预览版 → windows-test/bin/
make test         # Go 全量单测(-count=1)
make vet          # go vet
make fmt-check    # gofmt -l 必须无输出
make clean        # 清理产物
make web-build    # 只构建前端(frontend-react → dist)
make web          # 构建 + sync-www.sh 同步 www/(保留 www/config/)
make web-test     # 前端 typecheck + lint + vitest
make web-dev      # Vite dev server :5173(/api、/console 代理到 :18080)
make web-e2e      # Playwright 端到端测试
make web-size     # www 产物体积预算检查
make dev-mock-build / make dev-mock   # linux 原生 mock 二进制(:18080)
```

(旧 `make smoke` / `make css` / `make css-check` 已随旧前端管线删除。)

- 版本号:`git describe --tags --always --dirty` 注入 `main.appVersion`,
  服务把 HTML 中 `__SA_VERSION__` 占位符替换为该值(新前端经 `<meta name="sa-version">`
  读取;部署验证项之一);`__SA_THEME__` 占位符替换为设备默认主题。
- 构建参数固定:`-buildvcs=false -trimpath -ldflags "-s -w"`(见 Makefile 注释,勿改)。

### 提交前检查清单

1. `make test` / `make vet` / `make fmt-check` 全过。
2. 改前端 → `make web-test` 通过;新增文案在 locales zh-CN/en 成对增键;
   `make web-size` 预算通过;`make web` 同步并提交 `www/` 产物(`www/config/` 保留,勿手改产物)。
3. 改路由/关键交互 → 补 Playwright 用例,`make web-e2e` 通过。
4. 新增 AT 命令 → 检查 `at_cache_policy.go` 分类与 `isATActionCommand` 模式;
   补 `mock_at_responses.go` 的模拟(**按真实固件行为**)。
5. 新增状态文件 → 补 `runtime` 变量可注入 + 原子写 + 卸载清理。
6. `make arm windows` 双平台通过(验证 stub 完整与交叉编译)。

## 6. 测试约定

- 表驱动;纯函数抽出来测;副作用函数用 `runtimeXxx` 变量注入临时路径(`t.TempDir()` + 恢复)。
- e2e:`e2eTestServer(t)` 起 `-mock` 服务,`e2eWebSocketCall` 走 WS 网关。
  注意:若新测试涉及状态文件写入,需重定向 `runtimeXxxStateFile`,避免写真实路径。
- 固件怪癖必须有对应测试锚定(如 `page_readonly_error_test.go` 的尾部 ERROR 容忍、
  mock 对 DHCPV4DNS 裸查询回 ERROR)。
- 已知盲区(迭代时优先补):`applyDNSUpstream` 回滚、两个 reconcile、
  `mac_bind_*`/`dns_upstream*` 的 handler 级测试。

## 7. 部署

详见 **DEPLOY.md**。摘要:

| 方式 | 适用 | 命令 |
|---|---|---|
| A | Windows + adb | `toolkit.bat` |
| B | 任意 adb 主机 | 手工 `adb push` + 安装脚本 |
| C | SSH 可达(推荐) | `development/deploy_via_ssh.sh [--dry-run\|--rollback]` |

- 安装统一由设备端 `install_simpleadmin_go.sh` 完成:停旧实例 → 装 `/usrdata/simpleadmin/`
  → systemd 优先、失败降级 post_boot 自启动;认证文件升级保留。
- 方式 C 自动:构建 → 备份(设备 `.deploy-backup/`)→ 上传(tar/cat over ssh)→ 安装
  → 5 项验证 → 失败自动回滚。
- ⚠️ **不要手工 `scp`**:设备 dropbear 无 sftp-server,新版 OpenSSH scp 默认 SFTP 协议必失败。
- 部署后若安装脚本报 `REBOOT_REQUIRED=1`(QCMAP mobileap 配置变更),需择机重启设备。

## 8. 固件事实与坑清单(实机验证,血泪教训)

设备:RG520N-CN,固件 `RG520NCNAAR02A02M4G_XM_01.001.01.001`(电信定制),
LAN `192.168.5.1/24`,AT 通道 `/dev/smd11`。

### AT 命令

| 事实 | 影响/对策 |
|---|---|
| `AT+QMAP="DHCPV4DNS"` 查询**与**设置恒返回 ERROR | 电信定制固件移除了该功能;V4 DNS 代理由 QCMAP 硬编码恒启用(`dhcp-option-force=6`)。UI 显示"未知"、不提供按钮;`dnsV4QueryFailed` 标记驱动 |
| `AT+QMAP="DHCPV6DNS"` 查询/设置均正常 | 正常使用 |
| `AT+QNWPREFCFG="ue_capability_band"` 返回**受限子集**(LTE 仅 1:3:5:8、NSA 仅 78、SA 1:8:28:78) | **不代表硬件能力,不得作为频段表依据**;已确认的完整硬件能力:LTE `1:3:5:8:34:38:39:40:41`、NSA/SA `1:8:28:41:78`(band_map.js 与 mock 以此为准) |
| `AT+QMAP="MAC_bind"` 为固件私有命令(官方手册未收录) | 槽位 1-10;查询 `+QMAP: "MAC_bind",idx,"MAC","IP"`(无绑定只有 OK);设置 `...,idx,"MAC","IP"`;删除 `...,idx,"",""`;固件写 `/etc/data/dhcp_hosts` 但不重载 dnsmasq |
| 组合命令某子命令失败 → 中止后续并以 ERROR 收尾 | 解析必须容忍尾部 ERROR;把易失败的子命令放组合末尾 |
| `AT+CFUN=1,1` 重启无应答是常态 | 用 `atResponseRebootOK` 判定;触发 35s 读保护期 |
| 写命令要加入 `isATActionCommand` 模式 | 否则保护期内被当读命令延迟执行,结果与响应背离 |
| `AT+QSCAN` 三种模式均不返回小区结果:`=1,1`/`=3,1` 立即 OK,`=2,1` 同步约 3 分钟后 OK,全程无 `+QSCAN:` 行(裸串口抓取证实);`=1,2`/`=1,3` ERROR(第二参数仅 0/1);`AT+QENG="neighbourcell"` 仅支持 LTE/WCDMA 邻区且 NR5G 驻网空闲时无任何返回 | RG520N 家族已知扫描缺陷(社区有"扫射频死机/锁不住小区"求固件帖),非本项目代码问题;`scan` 动作以 `empty: true` 标记零结果,前端显示固件受限提示;扫描命令超时 180s,前端 WS 请求超时 240s,必须大于后端等待上限;固件升级是唯一根治途径 |
| `AT+QNWCFG="nr5g_meas_info"` 本机可用(官方手册未收录):返回服务小区+NR 邻区测量,如 `+QNWCFG: "nr5g_meas_info",1,428910,277,-64,-11`(NR-ARFCN/PCI/RSRP/RSRQ,不含频段号);`AT+QENG="neighbourcell"` 仅 LTE 驻网时返回 LTE 邻区(手册部分收录),NR5G-SA 驻网时为空 | 小区锁定页"邻区扫描"(`Neighbour Scan`)模式基于这两条命令实现(`AT+QNWCFG="nr5g_meas_info";+QENG="neighbourcell"`,频段由 `nrARFCNToBand` 按 38.104 下行频点区间推导),是 QSCAN 失效固件上可用的扫描来源;命令超时 3s(`atSingleCommandTimeoutMS`) |

### 系统与网络

| 事实 | 影响/对策 |
|---|---|
| dnsmasq 2.81 由 `dnsmasq_service@0.service` 管理;ExecReload=`killall -HUP` | SIGHUP 只重读 hosts/dhcp-hostsfile;配置链变更必须重启单元;重启前 `dnsmasq --test` |
| QCMAP 开机重写 `/var/run/data/dnsmasq.conf.bridge0`、`/etc/data/hosts`、`dhcp_hosts`;`/etc/data/dnsmasq.conf` 出厂后不被重写 | 注入点选 `/etc/data/dnsmasq.conf` 末尾标记块 |
| 运营商屏蔽 ICMP | 连通性判断一律用 HTTP(如 `curl http://223.5.5.5/`),禁用 ping |
| 设备无电池时钟,掉电回 1980;`ntp-sync.service` 开机校时 | 文件时间戳不可信;勿开 `AT+CTZU`(会把本地时间当 UTC) |
| 设备端传输 | 用 tar/cat over ssh;scp(SFTP)不可用;adb 仅 USB 场景 |
| 厂商服务由多路拉起 | 不做任何服务停用/mask(有砖机历史);只动 `/usrdata` 与 `/etc/data` 可写区 |

### 本项目历史回归(均有锚定测试,勿重蹈)

1. 状态端点按 `atResponseOK` 判"读取失败" → 尾部 ERROR 的正常响应被拒,页面永久卡"状态获取中"。
   修复:只有**完全无数据行**才算失败(`networkConfigStatusReadFailed`)。
2. DNS 代理状态前端默认 `true` → 加载失败时显示假的"已启用"。修复:默认 `false` + 未加载显示"未知/获取中"。
3. `csvFields` 保留空字段后,四字段空头 `+CMGL: 3,2,,47` 把 PDU 长度当日期 → 乱码假短信。
   修复:日期回退加形状闸门。
4. `atResponseOK` 按行判定后,重启类命令超时文本被误判失败 → `set_imei` 误报"操作失败"。
   修复:`atResponseRebootOK`。
5. IMEI/IP 透传失败分支未取消重启倒计时 → 40 秒后误弹"重启结束"。修复:`Reboot.cancelCountdown()`。

## 9. 新功能迭代流程(检查单)

以"新增一个设备能力页面/动作"为例:

1. **实机探查**(只读;真机写操作红线见 DEPLOY.md):`ssh root@192.168.5.1` + `at-client` 验证 AT 语义;
   `strings` 固件二进制确认功能归属;确认命令是查询还是动作、失败形态。
2. **后端**:`page_<x>.go` 加 action(遵循 §3.4 契约)→ `at_cache_policy.go`/
   `isATActionCommand` 归类 → `mock_at_responses.go` 按真实行为模拟 → 纯函数+单测。
3. **原生层**(如涉及系统命令):`native_<x>.go` + `native_<x>_stub.go` 成对;
   状态文件用原子写 + `runtime` 变量;需要开机自愈的加 reconcile。
4. **前端**:按 §4.9 流程——`features/<id>/page.tsx`(default export)→ `routes.tsx` 注册 →
   locales zh-CN/en 成对增键 → `endpoints.ts` 封装 → 失败态/重试/确认框齐备。
5. **验证**:`make test vet fmt-check` + `make web-test web-size web-e2e` + `make web` +
   `make arm windows`;写操作路径一律先在 dev-mock(linux)/`run_windows_test.bat`(Windows)验证。
6. **部署**:首选 `development/deploy_via_ssh.sh`;按 DEPLOY.md 验收;
   出问题时 `--rollback`。
7. **记录**:固件新事实补本文档 §8;部署/流程变化更新 DEPLOY.md。

## 10. 文档索引

| 文档 | 内容 |
|---|---|
| 本文档 | 架构/约定/迭代流程 |
| `DEPLOY.md` | 部署三方式、构建产物、实机验收清单、回滚 |
| `AT_PROXY.md` | AT 代理协议(退出码/交互事务/示例),供外部程序与 AI 解析 |
| `/opt/cpe-doc/README.md` | 设备资料库总览与维护记录 |
| `/opt/cpe-doc/01-device-profile.md` | 设备硬件/固件/系统档案 |
| `/opt/cpe-doc/02-shell-access.md` | SSH/Web 控制台接入 |
| `/opt/cpe-doc/03-at-interface.md` | AT 通道使用(含 at-client 首选说明) |
