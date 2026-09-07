# SimpleAdmin 实机部署资料

> 生成时间:2026-08-30 · 构建版本:`222c0b2-dirty`(git describe 自动注入,替换前端 `__SA_VERSION__` 占位符)

## 一、已编译产物

| 产物 | 路径 | 用途 |
|---|---|---|
| ARMv7 二进制 | `development/simpleadmin/simpleadmin-httpd.armv7` | 设备端主程序(静态链接、已 strip,7.5MB) |
| Windows amd64 | `windows-test/bin/simpleadmin-httpd-windows-amd64.exe` | 本机预览测试(可选) |
| 前端产物 | `development/simpleadmin/www/`(Vite 构建产物) | 由 `make web` 构建并同步(`www/config/` 保留),`make web-test` / `make web-size` 通过 |

构建参数:`GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 -trimpath -ldflags "-s -w"`(Makefile `make arm`)。

## 二、部署前已通过的验证

- `make test`:Go 全量单测通过(含 e2e mock 与 www React 产物形态守护)
- `make web-test`:前端 typecheck + lint + vitest 全过
- `make web-size`:www 产物体积四道预算闸门(初始壳层/echarts/xterm/总量)通过
- `make web-e2e`:Playwright 端到端通过(webServer 自起 dev-mock)
- locales zh-CN/en 18 命名空间键成对;`go vet` / `gofmt` 通过

## 三、部署方式

### 方式 A:一键部署(推荐,Windows + USB)
设备开启 adb 后,直接双击仓库根目录 `toolkit.bat`:
1. 自动推送安装包到 `/tmp/development`(已精简:不推送 `frontend/` 构建源码,约 8MB)
2. 设备端执行 `install_simpleadmin_go.sh`:停旧实例 → 装到 `/usrdata/simpleadmin/` → systemd 优先,失败自动降级 post_boot 自启动
3. 脚本自动回传安装结果;若 mobileap 配置有变更会提示重启

### 方式 B:手动(任意有 adb 的主机)
```
adb shell "rm -rf /tmp/development && mkdir -p /tmp/development/simpleadmin"
adb push development/install_simpleadmin_go.sh /tmp/development/
adb push development/simpleadmin/simpleadmin-httpd.armv7 /tmp/development/simpleadmin/
adb push development/simpleadmin/www     /tmp/development/simpleadmin/www
adb push development/simpleadmin/systemd /tmp/development/simpleadmin/systemd
adb push development/simpleadmin/simplepasswd /tmp/development/simpleadmin/
adb push development/simpleadmin/mobileap_bridge0_mac.sh /tmp/development/simpleadmin/
adb shell "chmod +x /tmp/development/install_simpleadmin_go.sh && bash /tmp/development/install_simpleadmin_go.sh"
adb shell "rm -rf /tmp/development && mount -o remount,ro / >/dev/null 2>&1 || true"
```

> 更新安装时脚本会自动清理旧版遗留的前端构建源码(`frontend/`、`node_modules`);
> 当前 `toolkit.bat` 不再上传 `frontend/`(旧版曾整目录推送,约 19MB `node_modules` 属无效传输)。

### 方式 C:SSH 部署(无需 adb/Windows,推荐)
适用于有 SSH 可达设备的主机(如运维机)。脚本:`development/deploy_via_ssh.sh`。

```
development/deploy_via_ssh.sh            # 构建→备份→上传→安装→验证,失败自动回滚
development/deploy_via_ssh.sh --dry-run  # 只打印将执行的动作
development/deploy_via_ssh.sh --rollback # 回滚到设备上最近一次部署备份(备份尚存时可用)
```

- 目标设备经环境变量覆盖:`CPE_HOST`(默认 `192.168.5.1`)、`CPE_USER`(root)、`CPE_PORT`(22)、`CPE_SSH_KEY`(默认 `~/.ssh/cpe_id_ed25519`)。
- 每次部署前把当前二进制与前端备份到设备 `/usrdata/simpleadmin/.deploy-backup/`,部署成功后自动删除以释放空间;`--rollback` 仅在备份尚存(异常中断/删除失败)时可恢复。
- 部署后自动验证:服务存活、AT 代理 Socket、HTTP 可达、前端版本占位符已替换、`at-client ATI`;任一失败自动回滚。
- 安装脚本提示 `REBOOT_REQUIRED=1` 时(QCMAP mobileap 配置变更)脚本只告警、不自动重启。
- ⚠️ 传输用 **tar/cat over ssh**,不要用 `scp` 手工推送:设备端 dropbear 无 sftp-server,新版 OpenSSH `scp` 默认走 SFTP 协议会直接失败。

### 本机预览(可选,部署前)
`run_windows_test.bat` 用 Windows 版二进制 + 同一份 `www/` 起本地服务,浏览器先看效果。

## 四、实机验收清单

> **真机验证安全红线(必读)**
>
> - 真机(192.168.5.1)验收**仅允许只读检查**:登录、浏览页面、只读数据、防火墙 `status`/`status6`/`fwd_list` 展示、诊断页仅探测公网 DNS 目标与 `dns_query`、主题语言切换。
> - **严禁在真机触发任何网络配置写操作**:IP 透传、USB 协议、DNS 代理、上游 DNS、LAN IP、DHCP 绑定、DMZ、防火墙应用(`save`)、端口转发应用(`fwd_save`)、TTL、锁频、锁小区、重启、关机、IMEI、AT&F、时间同步步进。
> - 写路径一律在 mock 环境验证:linux 用 `make dev-mock`(:18080 原生 mock 二进制),Windows 用 `run_windows_test.bat`。

### 真机只读验收项

1. **登录与会话**:admin 登录进入单页管理界面;会话过期(401)后跳登录页并能回跳原页面(hash 恢复);未登录时 `/assets/` 静态资源可加载,登录页样式与脚本正常。
2. **明暗主题**:逐页切换亮/暗主题,暗色下所有按钮文字可读;无本地主题记录的浏览器(含登录页)首屏即按设备默认主题渲染,无闪烁。
3. **中英文**:系统设置→界面偏好切换语言,重点看 蜂窝网络(锁频/锁小区)、短信、防火墙新 Tab(IPv6/端口转发)、网络诊断、设备信息。
4. **移动端**:窄屏下侧栏抽屉打开/遮罩关闭正常,页面不横向溢出。
5. **只读数据面**:总览、信号详情、蜂窝网络、小区锁定、网络详情、系统监控(CPU/内存/负载/进程 Top 20、温度传感器面板 AT+QTEMP 17 路)、设备信息数据正常加载;加载失败有可见失败态与重试按钮,不显示假空值。
6. **防火墙只读展示**:端口规则 Tab 显示当前规则与全部 iptables 链统计(`status`);IPv6 Tab 显示 ip6tables 链只读统计(`status6`);端口转发 Tab 显示已保存 DNAT 规则与跳转挂载状态(`fwd_list`);DMZ Tab 显示当前状态。均不点击"应用/保存"。
7. **网络诊断页**:仅探测公网 DNS 目标(`http_probe`,如 223.5.5.5)与 `dns_query`(系统解析 + 指定上游服务器);不以内网设备或写路径为目标。
8. **短信只读**:收件箱摘要/详情弹窗、存储可视化(SM/ME 用量与短信中心号码)、CMS 错误码释义展示正常;短信转发页(Webhook/Server酱 两卡)显示当前配置,不保存、不点「发送测试」。
9. **控制台**:进入 `#/console` 即开 xterm 终端(懒连接),仅执行只读查看类命令。
10. **AT 代理**(详见 `AT_PROXY.md`):服务运行时 `/usrdata/simpleadmin/simpleadmin-httpd at-client ATI` 退出码 `0` 且输出含 `OK`;服务停止时 `at-client` 退出码 `2`;`--timeout-ms -5` 退出码 `2`。

### mock 环境验证项(dev-mock / run_windows_test.bat,严禁在真机执行)

1. **危险操作确认**:短信删除、恢复全部频段、解锁小区、防火墙清空/删除、端口转发删除、删除锁频配置档,均弹确认框;所有操作反馈为 toast(无浏览器原生 alert)。
2. **防火墙写路径**:`save` 应用端口阻止/放行规则;`fwd_save` 应用 DNAT 端口转发(校验、事务式应用失败回滚、`firewall_fwd.conf` 持久化、重启自动恢复)。
3. **网络设置写路径**:IP 透传(统一确认 + 40 秒倒计时遮罩,失败分支取消倒计时)、USB 协议、DNS 代理(IPv4/IPv6)、自定义上游 DNS、LAN IP、DHCP 静态绑定(上限 10 条、同 MAC/同 IP 拦截提示)、DMZ、TTL 设置。
4. **蜂窝网络写路径**:频段锁定/恢复全部、锁频配置档保存/删除、锁小区/解锁、APN 保存。
5. **系统写路径**:AT 命令重启、设备重启、关机三入口统一确认+倒计时;IMEI 设置;AT&F 重置;时间同步保存与手动同步一次。
6. **短信写路径**:发送短信(PDU 分段)、删除短信;短信转发页:Webhook 配置保存与开关(请求方式/请求头/超时/JSON 模板校验)、Server酱 SendKey 保存与开关、「发送测试」(Server酱 测试推送会真实外发一条到其官方 API,请用测试 SendKey)。

## 五、回滚与卸载

- 卸载:`adb shell "bash /usrdata/simpleadmin/uninstall_simpleadmin_go.sh"`(若已安装)或推送 `development/uninstall_simpleadmin_go.sh` 执行
- 认证文件 `/usrdata/simpleadmin/simpleadmin.auth` 在升级时自动备份保留,不会被重置
- 卸载脚本会自动清理 dnsmasq 注入(静态绑定、上游 DNS 标记块与状态文件),无需手工处理
- 后端 API(`/api/network_config_data` 的 `mac_bind_*`/`dns_upstream*`、`/api/diag_data`、`/api/firewall_data` 的 `status6`/`fwd_list`/`fwd_save` 等)与前端 `www/` React 产物配套,需同步升级/回退,不能仅重推旧版 `www/` 目录;回退旧版时设备上残留的 `firewall_fwd.conf` 不再被管理(nat 链规则重启即消失),必要时手工清理

## 六、账号与安全

- 默认账号:`admin / admin`(首装生成,升级保留)
- 修改密码:系统设置→账户安全;改后会提示"退出并重新登录"
- 连续登录失败会触发限流倒计时
