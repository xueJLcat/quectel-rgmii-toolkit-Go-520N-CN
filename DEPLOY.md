# SimpleAdmin 实机部署资料

> 生成时间:2026-08-30 · 构建版本:`222c0b2-dirty`(git describe 自动注入,替换前端 `__SA_VERSION__` 占位符)

## 一、已编译产物

| 产物 | 路径 | 用途 |
|---|---|---|
| ARMv7 二进制 | `development/simpleadmin/simpleadmin-httpd.armv7` | 设备端主程序(静态链接、已 strip,7.5MB) |
| Windows amd64 | `windows-test/bin/simpleadmin-httpd-windows-amd64.exe` | 本机预览测试(可选) |
| 前端产物 | `development/simpleadmin/www/css/tailwind.css` | 已构建并与源码一致(`make css-check` 通过) |

构建参数:`GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 -trimpath -ldflags "-s -w"`(Makefile `make arm`)。

## 二、部署前已通过的验证

- `make test`:Go 全量单测通过(含 e2e 提供真实前端)
- `make css` / `make css-check`:样式产物一致,0 个 `!important`
- `make smoke`:23 个前端模块装配、8 个页面工厂注册完整
- 模板绑定↔页面工厂交叉核对:0 缺失;DOM id 核对:0 缺失
- i18n 漏翻扫描:0 真实缺口;`go vet` / `gofmt` 通过

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
development/deploy_via_ssh.sh --rollback # 回滚到设备上最近一次部署备份
```

- 目标设备经环境变量覆盖:`CPE_HOST`(默认 `192.168.5.1`)、`CPE_USER`(root)、`CPE_PORT`(22)、`CPE_SSH_KEY`(默认 `~/.ssh/cpe_id_ed25519`)。
- 每次部署前把当前二进制与前端备份到设备 `/usrdata/simpleadmin/.deploy-backup/`,`--rollback` 随时可恢复。
- 部署后自动验证:服务存活、AT 代理 Socket、HTTP 可达、前端版本占位符已替换、`at-client ATI`;任一失败自动回滚。
- 安装脚本提示 `REBOOT_REQUIRED=1` 时(QCMAP mobileap 配置变更)脚本只告警、不自动重启。
- ⚠️ 传输用 **tar/cat over ssh**,不要用 `scp` 手工推送:设备端 dropbear 无 sftp-server,新版 OpenSSH `scp` 默认走 SFTP 协议会直接失败。

### 本机预览(可选,部署前)
`run_windows_test.bat` 用 Windows 版二进制 + 同一份 `www/` 起本地服务,浏览器先看效果。

## 四、实机验收清单(本次重设计重点)

1. **明暗主题**:侧栏底部"暗夜模式"切换,9 个页面逐页看——暗色下所有按钮文字可读(旧版"应用/锁定"按钮曾不可见)
2. **中英文**:系统设置→界面偏好切换语言,重点看 蜂窝网络(锁频/锁小区)、短信、设备信息贡献者区块
3. **移动端**:窗口 ≤992px,左上"菜单"打开侧栏,应有半透明遮罩、点遮罩可关闭;总览页脚不再横向溢出
4. **危险操作确认**:短信删除、恢复全部频段、解锁小区、防火墙清空/删除、删除锁频配置档,均应弹确认框
5. **重启流程**:系统设置"重启设备"、网络设置改 USB 协议、AT 命令重置,三入口均为统一的确认+倒计时遮罩
6. **功能新位置**:DMZ 在防火墙页;TTL 在蜂窝网络页;短信转发在短信服务页
7. **通知**:所有操作反馈为右上角 toast(不再有浏览器原生 alert)
8. **静态地址绑定**:网络设置页底部新面板,添加/删除均弹确认与 toast;添加后设备重新获取地址应为固定 IP;重启设备后绑定仍在;上限 10 条、同 MAC/同 IP 有拦截提示
9. **自定义上游 DNS**:DNS 代理面板内新区域,保存时提示将短暂重启 DNS 服务(约 1 秒);启用后客户端 `nslookup` 走自定义服务器,恢复运营商后回到下发 DNS;仅 DNS 代理开启时影响客户端
10. **AT 代理**(详见 `AT_PROXY.md`):服务运行时 `/usrdata/simpleadmin/simpleadmin-httpd at-client ATI` 退出码 `0` 且输出含 `OK`;服务停止时 `at-client` 退出码 `2`;`--timeout-ms -5` 退出码 `2`

## 五、回滚与卸载

- 卸载:`adb shell "bash /usrdata/simpleadmin/uninstall_simpleadmin_go.sh"`(若已安装)或推送 `development/uninstall_simpleadmin_go.sh` 执行
- 认证文件 `/usrdata/simpleadmin/simpleadmin.auth` 在升级时自动备份保留,不会被重置
- 卸载脚本会自动清理 dnsmasq 注入(静态绑定、上游 DNS 标记块与状态文件),无需手工处理
- 本次改动新增后端 `/api/network_config_data` 的 `mac_bind_*` 与 `dns_upstream*` action 及 dnsmasq 原生层,前后端需同步升级/回退,不能仅重推旧版 `www/` 目录

## 六、账号与安全

- 默认账号:`admin / admin`(首装生成,升级保留)
- 修改密码:系统设置→账户安全;改后会提示"退出并重新登录"
- 连续登录失败会触发限流倒计时
