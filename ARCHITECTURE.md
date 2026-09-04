# 架构与模块详解

> 本文档是 SimpleAdmin Go 的模块/函数级参考文档:目录结构、各文件与函数说明、主要运行流程、运行调试命令。
> 面向用户的介绍与安装说明见 [README.md](README.md);开发迭代指南见 [DEVELOPMENT.md](DEVELOPMENT.md)。

## 目录结构

```text
Simple_Admin_GO/
├── toolkit.bat
├── uninstall.bat
├── run_windows_test.bat
├── Makefile
├── .github/workflows/ci.yml
├── adb.exe
├── AdbWinApi.dll
├── AdbWinUsbApi.dll
├── development/
│   ├── install_simpleadmin_go.sh
│   ├── uninstall_simpleadmin_go.sh
│   └── simpleadmin/
│       ├── simpleadmin-httpd.armv7
│       ├── simplepasswd
│       ├── mobileap_bridge0_mac.sh
│       ├── frontend/
│       ├── systemd/simpleadmin-httpd.service
│       └── www/
├── go-build/
│   ├── build_simpleadmin_go.bat
│   └── simpleadmin-go/
├── windows-test/
│   ├── build_windows_test.bat
│   ├── run_windows_test.bat
│   ├── frontend_smoke.js
│   ├── bin/simpleadmin-httpd-windows-amd64.exe
│   └── data/
└── README.md
```

## 根目录文件说明

| 文件 | 作用 |
|---|---|
| `toolkit.bat` | Windows 一键安装入口。等待 ADB 设备，按需推送安装文件（`install_simpleadmin_go.sh`、`uninstall_simpleadmin_go.sh` 与 `simpleadmin/` 下的二进制、`www`、`systemd`、`simplepasswd`、`mobileap_bridge0_mac.sh`，不再推送 `frontend/` 构建源码）到模块，并调用模块端安装脚本。 |
| `uninstall.bat` | Windows 一键卸载入口。通过 ADB 调用模块端卸载脚本。 |
| `run_windows_test.bat` | Windows 本地测试入口，启动 `windows-test` 中的测试服务；批处理窗口输出中文提示，使用 GBK/CP936 编码和 CRLF 换行，避免 Windows cmd 误解析中文行。 |
| `adb.exe` | Windows ADB 可执行文件。 |
| `AdbWinApi.dll` / `AdbWinUsbApi.dll` | Windows ADB 运行所需动态库。 |
| `README.md` | 项目说明、目录说明、函数说明、开发约束。 |

## 模块端安装目录 `development/`

### `development/install_simpleadmin_go.sh`

模块端安装脚本，负责把 Go 后端、Web 文件、认证文件、TTL 状态文件和独立的 mobileap/bridge0 固定 MAC 脚本安装到 `/usrdata`；重复安装时会先停止旧的 systemd 服务、旧的 `/usrdata` 后台进程，并清理旧版 `/dev/ttyIN`、`/dev/ttyOUT`、`socat` 桥接残留；随后覆盖当前版本文件，避免未卸载直接安装时旧进程继续占用 AT 口；优先把 systemd 服务文件写入 `/lib/systemd/system` 并启动服务，systemd 服务 active 时不修改 `/etc/init.post_boot.sh`；如果 `/lib/systemd/system` 不可写或服务无法 active，才自动改用 `/usrdata/simpleadmin/start_simpleadmin.sh` 后台启动方式，并在 `/etc/init.post_boot.sh` 可写时写入带标记的 post_boot 自启块；启动 SimpleAdmin 前会先执行 `/usrdata/simpleadmin/prepare_simpleadmin_ports.sh`，停止旧 `simpleadmin-httpd` 和设备原厂 `/usr/sbin/lighttpd -f /data/lighttpd.conf` 运行进程以释放 80 端口；如果 80 端口仍被其他进程占用，会在 bat 窗口用中文提示并输出占用进程信息，同时把安装结果写为失败，外层 bat 会停止成功提示；脚本通过 Windows bat 安装/卸载窗口可见的日志和提示使用 UTF-8 中文输出，由 bat 启动时统一设置的 UTF-8 代码页显示；写入配置文件、结果 env 文件、生成的后台启动脚本和 post_boot 运行日志等非 bat 窗口直接显示内容仍保持英文。

| 函数 | 功能 |
|---|---|
| `log()` | 输出普通安装日志。 |
| `warn()` | 输出警告日志。 |
| `fail()` | 输出错误、把本次安装结果写为失败并终止脚本。 |
| `write_install_failure_result()` / `write_install_success_result()` | 写入 `/tmp/simpleadmin-install-result.env` 中的 `INSTALL_STATUS`，供 Windows bat 判断远端安装是否真正成功。 |
| `remount_rw()` | 尝试把根文件系统重新挂载为可写。 |
| `remount_ro()` | 尝试把根文件系统重新挂载为只读。 |
| `require_file()` | 检查安装包中的必需文件是否存在。 |
| `is_writable_dir()` / `find_systemd_dir()` | 检测 `/lib/systemd/system` 是否可写；可写时才走 systemd，不可写时走 `/usrdata` 后台启动和 post_boot 兜底。 |
| `link_unit()` | 在可写的 `multi-user.target.wants` 中创建 systemd 服务软链接。 |
| `install_at_device_config()` | 写入 `/usrdata/simpleadmin/at_devices.conf`，只保留 Go AT 设备 `/dev/smd11`。 |
| `cleanup_legacy_at_bridges()` | 只清理旧版 `/dev/ttyIN`、`/dev/ttyOUT`、`socat` 桥接残留，不清理系统 `port_bridge`，也不触碰 `/dev/smd7`。 |
| `cleanup_legacy_frontend_sources()` | 清理旧版 `toolkit.bat` 整目录上传遗留的前端构建源码：删除 `/tmp/development/simpleadmin/frontend`（含 `node_modules`），以及安装目录 `/usrdata/simpleadmin/frontend`、`/usrdata/simpleadmin/node_modules`（若存在）；当前版本不再使用设备端前端构建源码，重复安装时避免无用文件占用 `/usrdata` 空间。 |
| `stop_existing_simpleadmin_runtime()` | 重复安装前停止旧 systemd 服务、旧后台 PID、旧 `simpleadmin-httpd` 进程，清理旧版 AT 桥接残留，然后删除旧 systemd unit；安装前不清理 post_boot，避免在 systemd 路径安装时改动 `/etc/init.post_boot.sh`。 |
| `install_ttl_state()` | 初始化 `/usrdata/simpleadmin/ttlvalue`，仅复用 Go 版本自身的 TTL 状态；安装时不读取或清理旧版 SimpleAdmin TTL 残留。 |
| `install_mobileap_helper_script()` | 把独立的 `mobileap_bridge0_mac.sh` 安装到 `/usrdata/simpleadmin/mobileap_bridge0_mac.sh`。 |
| `maybe_install_bridge0_mac_config()` | 调用独立的 mobileap/bridge0 固定 MAC 脚本，读取 `/tmp/simpleadmin-mobileap-result.env`，并把是否实际修改 `mobileap_cfg.xml` 的结果传给安装流程。 |
| `write_reboot_marker_if_mobileap_cfg_touched()` | 只有独立脚本返回实际恢复或修改了 `mobileap_cfg.xml` 时，才写出本次安装结果 `/tmp/simpleadmin-install-result.env` 为 `REBOOT_REQUIRED=1`，并兼容写出 `/tmp/simpleadmin-reboot-required` 标记；如果配置中的目标字段已经和 `bridge0_mac` 一致，则写 `REBOOT_REQUIRED=0`，不写重启标记；Windows 安装工具只相信本次安装结果文件，不再因为旧标记文件残留而误触发重启。目标字段选择顺序为：先检查 `APMACAddress`，存在则只写它；不存在时才检查 `EarlyEthMode` 和 `EarlyEthMACAddr`，两者都存在才写入 `EarlyEthMode=1` 和目标 `EarlyEthMACAddr`；都不存在则不写并提示。安装脚本自身不直接 reboot，由 `toolkit.bat` 在显示安装成功后按需直接重启。 |
| `install_simpleadmin_files()` | 安装 `simpleadmin-httpd`、Web 静态文件、认证文件、`/usrdata` 后台启动/停止脚本，并在可写时安装 systemd 服务文件。 |
| `install_fallback_scripts()` | 在 `/usrdata/simpleadmin` 下生成 `prepare_simpleadmin_ports.sh`、`start_simpleadmin.sh` 和 `stop_simpleadmin.sh`；`prepare_simpleadmin_ports.sh` 用于 systemd 和 fallback 启动前释放 80 端口，只停止旧 SimpleAdmin 进程和设备原厂 `/usr/sbin/lighttpd -f /data/lighttpd.conf` 运行进程，不删除 lighttpd 配置文件；`start_simpleadmin.sh` / `stop_simpleadmin.sh` 用于只读根分区环境的后台启动和停止；启动前只清理旧版 AT 桥接残留，不处理系统 `port_bridge`。 |
| `install_post_boot_autostart()` / `remove_post_boot_autostart()` | 仅在 systemd 不可用或服务无法 active 并进入 fallback 安装时，才在 `/etc/init.post_boot.sh` 中写入或更新带标记的 SimpleAdmin 自启块；systemd active 路径不修改 post_boot。 |
| `install_systemd_unit()` | 尝试把服务文件安装到 `/lib/systemd/system`，并清理旧的 `/etc/systemd/system` 同名 unit，成功后设置 systemd 启动标记；失败时不终止安装。 |
| `start_fallback_service()` | 当 systemd 不可用或服务启动失败时，用 `nohup` 启动 `/usrdata/simpleadmin/simpleadmin-httpd`，参数使用当前二进制支持的 `-static`、`-auth-file` 和 `-at-devices-file`；启动前调用端口准备脚本释放 80 端口，端口仍被其他进程占用时输出占用进程信息并明确提示；默认不启用 `-at-debug` 串口调试日志。 |
| `restart_services()` | 优先通过 `/lib/systemd/system` 下的 systemd unit 启动；systemd active 时不修改 post_boot；`/lib/systemd/system` 不可写或服务不 active 时才写入 post_boot 并退回 `/usrdata` 后台启动方式。 |
| `main()` | 安装流程入口，按顺序执行挂载、旧运行实例清理、文件安装、配置写入、服务启动。 |

### `development/uninstall_simpleadmin_go.sh`

模块端卸载脚本，只删除 SimpleAdmin Go 版本自身安装的 systemd 服务、`/usrdata` 后台启动进程、Web 文件、认证辅助文件和配置文件；脚本通过 Windows bat 卸载窗口可见的日志和提示使用 UTF-8 中文输出，由 bat 启动时统一设置的 UTF-8 代码页显示；脚本内部变量、systemd 操作、删除逻辑和非 bat 窗口直接显示内容仍保持英文。

| 函数 | 功能 |
|---|---|
| `log()` | 输出卸载日志。 |
| `warn()` | 输出卸载警告。 |
| `remount_rw()` | 尝试把根文件系统重新挂载为可写。 |
| `remount_ro()` | 尝试把根文件系统重新挂载为只读。 |
| `stop_service()` | 停止指定 systemd 服务。 |
| `stop_fallback_process()` | 停止 `/usrdata` 后台启动的 `simpleadmin-httpd` 进程并删除 PID 文件，同时只清理旧版 AT 桥接残留，不处理系统 `port_bridge`。 |
| `remove_post_boot_autostart()` | 卸载时从 `/etc/init.post_boot.sh` 中删除 SimpleAdmin 自启块。 |
| `remove_unit()` | 停止、禁用并从 `/etc/systemd/system`、`/lib/systemd/system` 中尽量删除指定 systemd 服务文件和 wants 软链接；路径只读时忽略错误。 |
| `clear_go_ttl_rules()` | 清除 Go 管理的 TTL iptables 规则。 |
| `remove_simpleadmin_go_files()` | 删除 SimpleAdmin Go 服务、Go 版本 Web 目录、`simpleadmin-httpd`、认证文件、历史证书文件、AT 设备配置、TTL 状态和 bridge0 MAC 记录；同时清理旧版可能遗留的 `socat-armel-static` 文件。 |
| `reload_systemd()` | 重新加载 systemd 并 reset failed 状态。 |
| `main()` | 卸载流程入口。 |

### `development/simpleadmin/simpleadmin-httpd.armv7`

模块端运行的 Linux ARMv7 Go 后端二进制。由 `go-build/simpleadmin-go` 交叉编译生成，项目约束要求使用 Go 1.26.3 编译；在模块上优先由 systemd 启动，只读 squashfs 根分区环境下由 `/usrdata/simpleadmin/start_simpleadmin.sh` 后台启动。

### `development/simpleadmin/simplepasswd`

认证辅助程序，用于更新或生成页面登录使用的账号密码文件。


### 安装/卸载清理边界

安装脚本会覆盖 SimpleAdmin Go 当前版本需要的运行文件和配置；重复安装前会停止旧 `simpleadmin-httpd`、删除旧 SimpleAdmin systemd unit，并只清理旧版 `/dev/ttyIN`、`/dev/ttyOUT`、`socat` 桥接残留；安装前不清理 post_boot，systemd active 路径也不修改 post_boot，只有 fallback 自启安装时才写入或更新 SimpleAdmin 自己的 post_boot 标记块。启动 SimpleAdmin 前会停止设备原厂 `/usr/sbin/lighttpd -f /data/lighttpd.conf` 运行进程以释放 80 端口，但不删除或修改 `/data/lighttpd.conf`、`/WEBSERVER/www/`、CGI、simpleupdates、旧 TTL 脚本或 `/usrdata/simplefirewall` 等历史文件目录，也不会清理系统 `port_bridge`。卸载脚本只卸载 SimpleAdmin Go 自身内容。

### 安装过程会操作的文件和入口

重复安装时的顺序为：先尝试 remount 根分区为可写；停止并禁用旧 `simpleadmin-httpd.service`；执行旧的 `/usrdata/simpleadmin/stop_simpleadmin.sh`；删除旧 PID；`killall simpleadmin-httpd`；清理旧版 `/dev/ttyIN`、`/dev/ttyOUT`、`socat` 桥接残留；删除 SimpleAdmin 自己写入的 systemd unit；备份有效的登录认证文件；然后再覆盖当前版本文件。覆盖后会恢复有效认证文件；如果认证文件不存在、为空或格式无效，则重置为 `admin:admin` 并设置 `0600` 权限。该流程不清理 post_boot。

安装过程会创建或覆盖：

- `/usrdata/simpleadmin/simpleadmin-httpd`
- `/usrdata/simpleadmin/www/`
- `/usrdata/simpleadmin/systemd/simpleadmin-httpd.service`
- `/usrdata/simpleadmin/prepare_simpleadmin_ports.sh`
- `/usrdata/simpleadmin/start_simpleadmin.sh`
- `/usrdata/simpleadmin/stop_simpleadmin.sh`
- `/usrdata/simpleadmin/mobileap_bridge0_mac.sh`
- `/usrdata/simpleadmin/at_devices.conf`（默认只写入 `/dev/smd11`）
- `/usrdata/simpleadmin/ttlvalue`
- `/usrdata/root/bin/simplepasswd`
- `/usrdata/simpleadmin/simpleadmin.auth`，不存在、空文件或格式无效时重置为 `admin:admin`；已存在且格式有效时会备份到 `/tmp/simpleadmin.auth.backup` 后恢复，保留用户已改过的有效密码。

安装过程可能创建或更新：

- `/lib/systemd/system/simpleadmin-httpd.service` 和 `/lib/systemd/system/multi-user.target.wants/simpleadmin-httpd.service`，仅当 `/lib/systemd/system` 可写且 systemd 路径可用时。
- `/etc/init.post_boot.sh` 中的 SimpleAdmin 标记块，仅当 systemd 不可用或服务无法 active 时。
- 默认安装会启用 bridge0 MAC 固定流程，优先使用 `/usrdata/etc/data/mobileap_cfg.xml`，不存在时兜底 `/etc/data/mobileap_cfg.xml`；脚本先检查 `APMACAddress`，存在时只写 `APMACAddress`，没有 `APMACAddress` 时才检查 `EarlyEthMode` 和 `EarlyEthMACAddr`，两者都存在时写入 `EarlyEthMode=1` 和目标 `EarlyEthMACAddr`；都不存在时不修改配置并提示，同时写入 `/usrdata/simpleadmin/bridge0_mac`。
- 如需跳过 QCMAP `mobileap_cfg.xml` 修改，可在执行安装脚本前明确设置 `SIMPLEADMIN_FIX_BRIDGE0_MAC=0`。
- `/tmp/simpleadmin-httpd.log`、`/tmp/simpleadmin-postboot.log` 等运行日志。

安装过程不会清理 `port_bridge`，不会主动触碰 `/dev/smd7`，不会删除旧版 Lighttpd/CGI/simpleupdates/simplefirewall 目录；只会在启动 SimpleAdmin 前停止使用 `/data/lighttpd.conf` 的原厂 lighttpd 运行进程来释放 80 端口。

### bridge0 固定 MAC 安装行为

默认安装会调用独立的 `/usrdata/simpleadmin/mobileap_bridge0_mac.sh` 执行 bridge0 MAC 固定流程：生成或复用一个 `06:3F:B1:xx:xx:xx` 格式的 MAC，保存到 `/usrdata/simpleadmin/bridge0_mac`，并优先检查 `/usrdata/etc/data/mobileap_cfg.xml`，不存在时才使用 `/etc/data/mobileap_cfg.xml`。字段选择顺序为：先检查 `APMACAddress`，存在时只写 `APMACAddress`；如果没有 `APMACAddress`，再检查 `EarlyEthMode` 和 `EarlyEthMACAddr`，两者都存在时写入 `EarlyEthMode=1` 和目标 `EarlyEthMACAddr`；如果两套字段都不存在，则不写 `mobileap_cfg.xml` 并提示。目标字段已经和 `bridge0_mac` 一致时不会写配置，也不会触发重启提示；只有文件缺失/为空并从 `.simpleadmin.bak` 恢复，或者实际替换写入目标字段时，才在本次安装结果 `/tmp/simpleadmin-install-result.env` 写 `REBOOT_REQUIRED=1` 并兼容写出 `/tmp/simpleadmin-reboot-required`；Windows `toolkit.bat` 只读取本次安装结果文件，避免旧标记残留导致重复安装误触发重启，并在显示安装成功后直接发送重启命令，不再询问确认或提供跳过选项。首次修改前会把原配置备份为同路径 `.simpleadmin.bak`；写入使用临时文件校验后替换，并恢复为 `radio:radio` 和 `0755` 权限。为避免安装过程中重启 QCMAP 影响 AT 串口和网络状态，安装脚本不自动重启 `QCMAP_ConnectionManagerd.service`，也不在 shell 脚本内部直接 `reboot`。如果确实不想处理 `mobileap_cfg.xml`，可以在执行安装脚本前设置 `SIMPLEADMIN_FIX_BRIDGE0_MAC=0`。


### `development/simpleadmin/mobileap_bridge0_mac.sh`

独立的 QCMAP `mobileap_cfg.xml` / bridge0 固定 MAC 处理脚本。默认由安装脚本调用，也可以单独在模块上运行；通过 Windows bat 安装窗口可见的日志使用 UTF-8 中文输出，写入结果 env 文件和脚本内部字段名仍保持英文。

| 函数 | 功能 |
|---|---|
| `is_simpleadmin_bridge0_mac()` | 校验 MAC 是否符合安装脚本管理的 `06:3F:B1:xx:xx:xx` 格式。 |
| `select_mobileap_cfg_file()` | 优先选择 `/usrdata/etc/data/mobileap_cfg.xml`，不存在时兜底 `/etc/data/mobileap_cfg.xml`。 |
| `restore_mobileap_cfg_from_backup_if_needed()` | 当前配置缺失或为空且 `.simpleadmin.bak` 存在时，先从备份恢复，并标记本次需要重启。 |
| `read_saved_bridge0_mac()` | 优先从 `/usrdata/simpleadmin/bridge0_mac` 读取已保存 MAC；没有时从 `mobileap_cfg.xml` 的 `APMACAddress` 读取已设置 MAC；仍没有时从 `EarlyEthMACAddr` 读取符合 SimpleAdmin 前缀的 MAC。 |
| `generate_bridge0_mac()` | 生成 `06:3F:B1` 开头、后三字节来自随机数的本地管理 MAC。 |
| `set_mobileap_bridge0_mac()` | 先检查 `APMACAddress`，存在则只更新该字段；没有时再检查 `EarlyEthMode` 和 `EarlyEthMACAddr`，两者存在则写入 `EarlyEthMode=1` 和目标 `EarlyEthMACAddr`；都不存在则不写并提示。实际写入时使用临时文件生成新配置、校验非空和目标节点后再替换原文件，并恢复 `radio:radio` 与 `0755` 权限。 |
| `apply_bridge0_mac_now()` | 在当前系统已存在 `bridge0` 时立即尝试应用目标 MAC。 |
| `write_result()` | 把本次是否实际恢复/修改 `mobileap_cfg.xml` 写入 `/tmp/simpleadmin-mobileap-result.env`，供安装脚本决定是否写重启标记。 |

### `development/simpleadmin/systemd/simpleadmin-httpd.service`

模块端 systemd 服务文件，启动 `/usrdata/simpleadmin/simpleadmin-httpd`。启动前通过 `ExecStartPre=/usrdata/simpleadmin/prepare_simpleadmin_ports.sh` 释放 80 端口，停止旧 SimpleAdmin 进程和设备原厂 `/usr/sbin/lighttpd -f /data/lighttpd.conf` 运行进程；服务参数使用当前二进制支持的 `-static`、`-auth-file`、`-http`、`-no-tls` 和 `-at-devices-file`；默认不启用 `-at-debug` 串口调试日志；模块端默认只启用 HTTP 管理界面，不启用 HTTPS，也不提供 CA 下载入口；默认 AT 发送直接使用 `/dev/smd11`，不再创建 `socat`、`/dev/ttyIN` 或 `/dev/ttyOUT` 桥接。如果 `/lib/systemd/system` 不可写或服务无法 active，安装脚本会保留该文件到 `/usrdata/simpleadmin/systemd/` 作为备份，实际用 `/usrdata` 后台方式启动并按需写入 post_boot。

## Web 静态目录 `development/simpleadmin/www/`

### HTML 页面

`www` 目录只保留实际使用的 `index.html` 和 `login.html`，不再保留网络、设置、短信、设备信息或注销用的兼容跳转 HTML 文件。

| 文件 | 功能 |
|---|---|
| `index.html` | 登录成功后的单页管理界面入口，使用左侧菜单切换总览、信号详情、蜂窝网络、小区锁定、网络详情、网络设置、防火墙、短信服务、AT 命令、控制台、自动化、系统监控、系统设置、设备信息内容区（监控/网络/通信/工具/系统五组）；页面切换时使用非滚动方式在内容显示前直接重置全部滚动容器，让新页面直接从最顶端显示，标题、面板、表格行和短信摘要行会按顺序渐进显示，渐进显示期间只临时隐藏内容框内部滚动条，页面全局右侧滚动条保持显示，避免加载动画阶段内容框出现临时滚动条，刷新后也直接显示顶端且不保留上次滚动位置，并遵守系统“减少动态效果”设置；浅色模式/暗夜模式切换按钮放在每个内容页标题栏右侧，不再在菜单或标题栏上方单独占用一整行主题切换栏；内容区统一为纵向排版并跟随页面可用宽度铺开，首页顶部使用同一行四部分自适应布局，依次显示 CPU 半圆仪表盘、RAM 半圆仪表盘、RSRQ/RSRP/SINR 三个 4G/5G 信号进度条大框，以及一个合并大卡片，卡片内部竖排显示温度数值、信号百分比数值和互联网连接状态；CPU/RAM 仪表盘参考圆弧仪表样式，百分比和说明文字均在卡片中居中显示；首页总信号百分比按 RSRP 50%、RSRQ 25%、SINR 25% 加权计算，其中 RSRP 按 -120 到 -80、RSRQ 按 -20 到 -8、SINR 按 0 到 20 映射为百分比；首页将网络信息和信号信息合并到同一个“网络信息”面板，内容区“激活 SIM”行显示为“已激活卡1”这类完整状态，信号评估位于速率下方，在线时长位于面板数据列表最下方，刷新频率设置和更新时间位于面板底部并保持单行显示，刷新频率为空输入时保持原值（2-60 秒内取值并记忆到本地存储），数据请求失败时不刷新「更新时间」；首页停留在当前页面时才自动刷新首页数据，离开首页后停止刷新；首页“网络信息”标题后带有“精简显示/完整显示”按钮，设备信息页和首页只在当前页面可见时自动刷新，设置页不再自动请求或依赖设备信息接口，精简显示时整行隐藏 MCCMNC、CELL ID、eNB ID 和 TAC，并通过本地存储记住状态；首页和设备信息页的数据行改为接近系统信息页的整宽信息表，字段名称在左侧，字段数据以最长数据行形成的公共数据列为基准整体放在页面中间区域，并在该列内靠左显示；小区锁定页 LTE 手动小区锁定在未填写“小区数量”时不显示 EARFCN/PCI 输入框，填写 1-10 后按数量显示对应输入行；蜂窝网络页“网络工具”中的 APN、IP 协议类型、选择首选网络、NR5G 模式控制四项在同一横排显示；蜂窝网络页频段锁定区域横排三等分显示 LTE、NR5G-NSA、NR5G-SA 三个模式，每个模式独立显示可用频段复选框，并提供当前模式的“锁定”和“全部选中/取消选中”按钮，三列下方只保留一个全局“恢复全部”按钮用于恢复全部 LTE/NSA/SA 可用频段；蜂窝网络页获取到模块型号并映射出当前可用频段后，立即发送 `AT+QNWPREFCFG` 查询 LTE/NSA/SA 当前已锁定频段并回填勾选状态；小区锁定页的小区扫描“开始扫描 / 锁定 / 清除”按钮保持同一行显示；设置页不提供 HTTPS 证书或 CA 下载入口，管理界面默认通过 HTTP 访问；网络设置页顶部为“当前状态”概览徽章行，依次显示 IP 透传、USB 协议、DNS V4、DNS V6、DMZ 和 LAN IP 段的当前状态，状态未加载时显示“状态获取中...”，获取失败或重试耗尽时显示“状态获取失败”和重试按钮；其下 IP 透传、USB 协议、DNS 代理、DMZ 设置、LAN IP 设置和 TTL 设置六个面板使用左右两竖列显示且两侧面板高度保持一致，DNSv4 和 DNSv6 不显示左侧名称，名称并入启用/禁用按钮，DNSv4 在 DNSv6 前面显示，TTL 状态和值在同一行显示；AT 命令页 AT 终端面板内输出框使用追加式显示，命令输入框支持 ↑/↓ 键翻阅命令历史，“提交 / 复制 / 清除”按钮同一行显示，常用命令和命令历史两个面板使用左右两竖列显示，重置 AT&F 单独放在“危险操作”面板并带二次确认弹窗，各重启/重置/IMEI 确认弹窗统一使用红色确认按钮和灰色取消按钮；系统设置页的设备操作面板包含 AT命令重启、设备重启、关机三个电源动作，每个动作为一行（左侧按钮、右侧说明文字，窄屏时纵向堆叠），且均需二次确认弹窗：AT命令重启经 AT+CFUN=1,1 重启蜂窝模块（约 40 秒倒计时），设备重启经系统 reboot 命令重启整机（约 60 秒倒计时，倒计时文案为“设备重启中…”），关机经系统 poweroff 命令断电，成功后弹出“设备已关机”提示弹窗；设备操作面板其余为 IMEI 设置，和其他设置（界面语言、默认主题、登录密码、TTL）使用左右两竖列显示，界面偏好面板内界面语言与默认主题上下两张子卡片排布；默认主题（浅色模式/暗夜模式）保存为设备级默认值，服务端在输出 HTML 时把 `data-bs-theme` 上的占位符替换为该值，无本地主题记录的浏览器（含登录页）首屏即按默认主题渲染；页面标题栏右侧切换按钮的选择只写入当前浏览器本地存储，不影响设备默认值；设备操作面板的当前 IMEI 明文显示，状态指令 `+CGSN` 返回的当前 IMEI 会回填到 IMEI 设置输入框，自动化面板单独一列显示，其余面板、参数表和表单按一列从上到下显示，按钮采用内容自适应宽度；亮模式下侧边栏、标题栏、内容区、表格和控件使用全局白色主题，暗夜模式下整体切换为更亮的深灰暗色主题；短信页收件箱按单行摘要显示发件人、时间和部分内容，点击摘要行通过全局弹窗覆盖整个后台界面显示完整短信内容，弹窗自适应视口高度，长短信正文在弹窗内独立滚动，弹窗和标题/信息/正文按顺序淡入显示；短信页停留在当前页面时使用固定时间轮询强制查询收件箱索引元信息，前端记录短信存储索引集合，索引没有变化时不拉取完整短信正文也不更新页面，WebSocket 响应不包含 `text` / `textLines` 大字段；发现索引变化后先缓存元信息并等待连续轮询确认索引稳定，确认新短信分片收齐后才拉取完整短信并一次性更新到页面，离开短信页后停止自动刷新；短信页收件箱底部刷新按钮后面紧跟删除按钮，按钮间隔 5px；控制台通过右侧内容显示区内嵌打开，不会离开单页外壳进入全屏独立页面，并且控制台高度跟随右侧内容区自动撑满可用高度，控制台内部页面隐藏可见滚动条，避免 iframe 内外出现双滚动条；控制台连接后直接进入 PTY shell（复用后台登录会话与 Origin 校验，无独立终端账号密码），控制台前端支持 ANSI SGR 颜色显示，包含基础 16 色、256 色和 truecolor 前景/背景色，并在 PTY shell 环境中设置 `TERM=xterm-256color`、`COLORTERM=truecolor`，控制台横幅不再显示旧的网页登录密码设置提示；页面侧边栏品牌区和浏览器标题会通过公开接口读取 `AT+CGMM` 获取到的模块型号并显示为当前品牌名称，品牌标识取模块型号前两位字符；后端成功获取一次型号后会在本次 `simpleadmin-httpd` 运行期间缓存，后续登录页、后台主页面、设备信息页和蜂窝网络页直接使用缓存型号，不再自动重复发送 `AT+CGMM`；设备信息页会把制造商、IMEI、固件、IMSI、ICCID、号码等低频信息拆成独立静态缓存，SIM/WWAN 在线状态仍按短缓存刷新；蜂窝网络页会把频段锁定查询、网络偏好/APN/小区锁配置和实时 `QCAINFO` 分开缓存，避免每次页面刷新都重复读取不变配置；短信页自动轮询只强制读取索引元信息，短信完整正文只有索引稳定且变化时才读取；设备信息页 Project Contributors 弹框为与全站一致的自建 `modal-overlay` 模态，由设备信息页工厂 `showContributors` 状态与 `openContributors()`/`closeContributors()` 控制，底部 Close 按钮关闭；登录页也会读取同一接口显示模块型号，未读取到型号时显示 `SimpleAdmin`。自动化页包含断网自愈看门狗、每日定时重启、短信转发和时间同步四个面板，其中看门狗、定时重启、短信转发各占一格，时间同步面板提供启用开关、同步间隔（分钟，1-1440）与单一 NTP 服务器地址（缺省阿里云 ntp.aliyun.com）输入、状态/上次同步/系统时间状态徽章行，以及“保存时间同步”与“手动同步一次”按钮，手动同步进行中按钮显示“同步中…”；系统监控页位于自动化与系统设置之间，展示效果类似 htop：顶部同一行显示 CPU 半圆仪表盘卡片、内存半圆仪表盘卡片和一个负载/运行时长/进程数信息卡片，下方“进程 Top 20”面板提供按 CPU/按内存排序切换、刷新按钮、更新时间和进程表（PID、用户、进程名、CPU%、内存%、内存占用），页面停留时每 3 秒自动刷新，离开页面停止刷新。 |
| `login.html` | 页面登录入口，使用普通表单提交用户名和密码；登录页读取与主界面相同的本地主题状态，支持浅色模式和暗夜模式切换；登录成功后由后端写入 HttpOnly 会话 Cookie，再进入单页管理界面，不触发浏览器 Basic Auth 弹窗。 |

### CSS、字体和静态资源

| 文件或目录 | 作用 |
|---|---|
| `css/tailwind.css` | 唯一样式表（约 53KB，minified），由 Tailwind CSS v4 样式源码 `development/simpleadmin/frontend/` 经 `make css` 编译生成并提交入库，设备端直接服务该产物；`index.html` 只加载 `css/tailwind.css?v=__SA_VERSION__` 与 `css/Poppins.css?v=__SA_VERSION__`。样式内容包含单页侧边栏布局、统一菜单、标题栏右侧主题切换按钮、移动端标题栏菜单按钮、页面标题栏、显示前直接置顶的页面切换处理、渐进显示动画、加载动效期间仅内容框内部临时隐藏滚动条、按钮/菜单/面板轻量过渡、短信收件箱刷新/删除按钮 5px 间隔横排、跟随页面宽度铺开的单列内容栈、内容面板、首页顶部 CPU/RAM 两张自适应居中半圆仪表盘卡、信号进度条大框和右侧合并指标大卡片共同组成同一行四部分状态区，信号行改为名称贴合文本并让进度条紧跟名称，名称与进度条之间的实际间距固定为 3px、紧凑参数行、首页网络信息精简/完整显示按钮、AT 命令页输出框/常用命令网格/命令历史列表、网络设置页当前状态徽章行、首页和设备页专用的系统信息式居中数据列信息表、自适应宽度按钮、蜂窝网络页和网络设置页横向动作按钮组、短信收件箱单行摘要、覆盖整个后台界面的全局详情弹窗、自适应高度详情弹窗和弹窗渐进淡入动效、网络工具四项横排布局、网络设置/AT 命令/系统设置页左右面板等高布局、系统设置页账号区域两列布局、TTL 状态和值同行布局，以及全局浅色模式/暗夜模式主题；主题作用于整个界面而不是局部内容区。设计令牌体系：圆角令牌 `--sa-radius-sm/…/--sa-radius-pill`（面板 16px、按钮/表单 12px、弹窗 20px、胶囊 999px）、阴影令牌 `--sa-shadow-sm/…/-lg`（暗夜附加卡片顶部高光 `--sa-panel-inset`）、间距令牌 `--sa-space-1…6`、字号令牌 `--sa-text-xs…2xl`、动效令牌 `--sa-ease/--sa-dur-fast/…/-slow`；令牌为浅色/暗夜双份运行时 CSS 变量，定义在源码 `frontend/input.css` 的 `@layer base`，并经 `@theme inline` 绑定为 Tailwind 主题，`bg-surface`、`text-muted`、`rounded-md` 等工具类可直接使用且自动跟随暗夜切换；整体风格为圆润卡片化，数值统一 `tabular-nums`，进度条/仪表盘加圆头端点和平滑过渡，按钮带 `:active` 缩放与 `:focus-visible` 焦点环，内容区最大宽度 1440px 居中，登录页主色/字体/圆角与主界面对齐。 |
| `css/Poppins.css` | Poppins 字体样式声明，由 `index.html` 直接引入。 |
| `fonts/*.woff2` | 本地字体文件。 |
| `favicon.ico` | 浏览器标签页图标。 |
| `config/get_language.json` | 前端语言默认配置，保存当前界面语言，支持 `zh-CN` 和 `en`。 |
| `config/get_theme.json` | 默认主题配置，支持 `light` 和 `dark`，由 `/api/get_theme`、`/api/set_theme` 读写；服务 HTML 时注入 `data-bs-theme`，作为无本地主题记录浏览器的首屏主题。 |

### 前端样式源码目录 `development/simpleadmin/frontend/`

Tailwind CSS v4 样式源码目录，编译产物为 `www/css/tailwind.css`；产物提交入库，设备端只服务该产物，运行时无构建。

| 文件 | 作用 |
|---|---|
| `input.css` | 构建入口：`@import "tailwindcss"`；`@custom-variant dark` 对接既有 `data-bs-theme="dark"` 暗夜开关；`@theme inline` 把 `--sa-*` 运行时令牌映射为 Tailwind 主题；`@source` 扫描 `www/index.html`、`www/login.html` 与 `www/js`（覆盖 JS 动态模板中的类名）；`--sa-*` 设计令牌（浅色/暗夜双份）定义在 `@layer base`。 |
| `src/components-base.css` | Bootstrap 移除后的等价控件/工具原语子集（按钮、表单、表格等），已令牌化。 |
| `src/components-shell.css` / `src/components-controls.css` / `src/components-pages.css` | 原 `www/css/styles.css` 的外壳、控件、页面三段迁移，选择器与层叠顺序不变。 |
| `package.json` / `package-lock.json` | 构建依赖（`devDependencies`：`tailwindcss`、`@tailwindcss/cli`），安装时优先 `npm ci` 按锁文件精确还原。 |

构建纪律：

- 修改前端样式源码后必须执行 `make css` 并提交 `www/css/tailwind.css` 产物。
- `make css-check` 重新编译到临时文件并与已提交产物 diff，校验源码与产物一致；CI 执行同一检查。
- 前端不再加载 `bootstrap.min.css`（232KB）、`styles.css`（48KB）和 `bootstrap.bundle.min.js`（80KB），由约 53KB 的 `tailwind.css` 替代；前端静态资源体积约 980KB → 约 670KB（净减约 310KB）。

## 前端公共 JavaScript

### 前端公共工具命名空间 `window.SimpleAdmin`（原 `simpleadmin-core.js`，已按模块拆分）

前端公共工具命名空间，挂载到 `window.SimpleAdmin`。原 1200+ 行单文件已按命名空间模块拆分，`index.html` 按依赖顺序加载（版本号 `?v=2.95`）：

| 文件 | 模块 | 职责 |
|---|---|---|
| `js/simpleadmin-api.js` | `SimpleAdmin.Api` | `/api/ws` WebSocket 请求封装与全部页面 API 方法。单次请求 240 秒超时后关闭连接并在下次请求时自动重建（自愈；取 240 秒是因为小区扫描后端最长约 182 秒，前端超时必须大于后端等待上限，否则扫描临近完成时前端会先超时断开连接）；业务响应状态 401 时自动跳转登录页；WebSocket 握手失败时用 `/api/get_language` 探测会话状态，探测返回 401（会话过期）时跳转登录页，探测失败按网络故障处理。 |
| `js/simpleadmin-brand.js` | `SimpleAdmin.Brand` | 品牌/模块型号名称展示。 |
| `js/simpleadmin-text.js` | `SimpleAdmin.Text` | 文本与十六进制工具。 |
| `js/simpleadmin-time.js` | `SimpleAdmin.Time` | 短信时间解析与格式化。 |
| `js/simpleadmin-sms.js` | `SimpleAdmin.Sms` | 短信 UDH 拼接头等前端辅助。 |
| `js/simpleadmin-lang.js` | `SimpleAdmin.Lang` | 中英双语字典与自动翻译（MutationObserver）。 |
| `js/simpleadmin-ui.js` | `SimpleAdmin.UI` | 主题、提示、模态等 UI 工具。 |
| `js/simpleadmin-mockat.js` | `SimpleAdmin.MockAT` | 浏览器 Console 注入 mock AT 数据（仅 `--mock`）。 |
| `js/simpleadmin-logout.js` | `SimpleAdmin.Logout` | 注销跳转。 |
| `js/simpleadmin-core.js` | — | 引导文件：命名空间导出与 `Brand.init()` 启动。 |

以下为原 `SimpleAdmin` 各方法的说明（方法归属见上表）：

| 对象/方法 | 功能 |
|---|---|
| `SimpleAdmin.Api.text(url, options)` | 通过 `/api/ws` WebSocket 请求文本响应。 |
| `SimpleAdmin.Api.json(url, options)` | 通过 `/api/ws` WebSocket 请求 JSON 响应。 |
| `SimpleAdmin.Api.url(path, params)` | 根据路径和查询参数生成 URL。 |
| `SimpleAdmin.Api.getDeviceInfo(params)` | 读取设备信息结构化 JSON；支持传入 `force` 强制刷新，只由设备信息页生命周期触发。 |
| `SimpleAdmin.Api.getAT(atcmd, options)` | 兼容保留的 AT 缓存读取方法，仅供调试使用；AT 命令页手动发送已改用 `/api/at_data`，页面脚本不再调用。 |
| `SimpleAdmin.Api.refreshAT(atcmd)` | 兼容保留的 AT 强制刷新方法，仅供调试使用。 |
| `SimpleAdmin.Api.getATText(atcmd, options)` | 兼容保留的 AT 文本读取方法，仅供调试使用。 |
| `SimpleAdmin.Api.atData(params)` | POST `/api/at_data`，AT 命令页手动 AT 与重置 AT&F 使用。 |
| `SimpleAdmin.Api.networkConfigData(params)` | POST `/api/network_config_data`，网络设置页状态读取与网络配置动作使用。 |
| `SimpleAdmin.Api.firewallData(params)` | POST `/api/firewall_data`，防火墙页状态（阻止/放行规则列表与所有链统计）读取与规则保存使用。 |
| `SimpleAdmin.Api.systemData(params)` | POST `/api/system_data`，系统设置页状态（IMEI）、修改 IMEI、重启使用。 |
| `SimpleAdmin.Api.getUptime()` | 获取系统运行时间。 |
| `SimpleAdmin.Api.getPing()` | 获取网络连通性检测结果。 |
| `SimpleAdmin.Api.getTTLStatus()` | 获取 TTL 状态。 |
| `SimpleAdmin.Api.setTTL(ttlvalue)` | 设置 TTL 值。 |
| `SimpleAdmin.Api.getLanguage()` | 读取当前界面语言设置。 |
| `SimpleAdmin.Api.setLanguage(language)` | 保存界面语言设置。 |
| `SimpleAdmin.Api.setPassword(currentPassword, newPassword, confirmPassword)` | 通过 POST 表单请求修改当前登录密码。 |
| `SimpleAdmin.Logout.start()` | 调用 `/api/logout` 清除页面登录会话 Cookie，然后跳转到 `login.html`，退出过程不显示提示界面。 |
| `SimpleAdmin.Text.lines(value)` | 把文本拆成去空行后的行数组。 |
| `SimpleAdmin.Text.findStartsWith(lines, prefix)` | 在行数组中查找指定前缀的第一行。 |
| `SimpleAdmin.Text.findAllStartsWith(lines, prefix)` | 查找指定前缀的所有行。 |
| `SimpleAdmin.Text.splitCsv(value)` | 简单 CSV 拆分，兼容引号。 |
| `SimpleAdmin.Text.hexToUtf16BE(hex)` | 把 UCS2/UTF-16BE 十六进制文本转为字符串。 |
| `SimpleAdmin.Text.compactHex(value)` | 去除十六进制文本中的空白。 |
| `SimpleAdmin.Time.parseSmsDate(value)` | 解析短信时间字符串；内部 `parseCustomDate` 先剥离 `±hh` 时区后缀，解析失败返回 `null`（不回退当前时间），`parseSmsDate` 在失败时返回无效日期。 |
| `SimpleAdmin.Time.formatDateTime(date)` | 格式化日期时间。 |
| `SimpleAdmin.Sms.parseConcatHeader(hex)` | 解析短信拼接 UDH 头。 |
| `SimpleAdmin.Sms.decodeUcs2(hex)` | 解码 UCS2 短信内容。 |
| `SimpleAdmin.Sms.encodeUcs2(text)` | 编码 UCS2 短信内容。 |
| `SimpleAdmin.Lang.normalizeLanguage(language)` | 规范化语言值，只接受中文 `zh-CN` 和英文 `en`。 |
| `SimpleAdmin.Lang.getCurrentLanguage()` | 返回当前前端正在使用的语言。 |
| `SimpleAdmin.Lang.t(key)` | 根据当前语言翻译指定界面文本，支持 `当前：...`、短信发送失败等动态前缀。 |
| `SimpleAdmin.Lang.apply(rootNode)` | 把当前语言应用到指定 DOM 范围内的静态文本、表单选项、输入框/文本域占位符和常用属性。 |
| `SimpleAdmin.Lang.load()` | 从后端语言配置或静态配置读取语言，并应用到页面；公共脚本会在所有页面自动加载语言。 |
| `SimpleAdmin.Lang.setLanguage(language, options)` | 切换语言，resolve 值统一为 `{ language, localOnly }`；传入保存选项时同时写入后端配置并立即刷新当前页面文本，服务器保存失败时 `localOnly` 为 `true`，语言仍在本地生效，由调用方提示「已本地生效，服务器保存失败」。 |
| 语言 MutationObserver | 监听 Vue 动态渲染后的文本、下拉框选项、输入框/文本域占位符和提示信息变化，自动按当前语言重新翻译。 |
| 原生提示翻译 | 对页面 `alert()` / `confirm()` 中的中文提示进行当前语言转换。 |
| `SimpleAdmin.UI.setText(selectorOrElement, value)` | 设置元素文本。 |
| `SimpleAdmin.UI.initDarkMode(buttonId)` | 初始化标题栏主题切换按钮、同步 `data-bs-theme`、读写本地主题状态；兼容旧的按钮 ID 参数。 |

### `development/simpleadmin/www/js/vue-app.js`

Vue 3 页面挂载工具。

| 方法 | 功能 |
|---|---|
| `SimpleAdmin.Vue.mount(factory, selector)` | 根据页面工厂函数创建并挂载 Vue 应用；单页模式下会按选择器记录到 `SimpleAdmin.Vue.apps`。 |
| `SimpleAdmin.Vue.apps` | 保存已挂载的页面 Vue 实例，例如 `#dashboardApp`、`#netConfigApp`、`#atCommandsApp`、`#settingsApp`。 |
| `mountSimpleAdminVueApp(factory, selector)` | 兼容旧页面写法的全局挂载函数。 |

### `development/simpleadmin/www/js/dark-mode.js`

暗夜模式初始化脚本。页面加载后调用 `SimpleAdmin.UI.initDarkMode()`，并监听 `simpleadmin:vue-mounted`，确保各页面标题栏右侧主题切换按钮在 Vue 挂载后也能重新绑定点击事件。

### `development/simpleadmin/www/js/simpleadmin-spa.js`

单页前端外壳控制脚本。

| 方法 | 功能 |
|---|---|
| `SimpleAdmin.Spa.showPage(page)` | 根据左侧菜单或 URL hash 显示指定内容区，并在首次显示时挂载对应 Vue 页面；入口幂等，请求的就是当前页面时直接返回（浏览器历史回退不会双触发）；未知 hash 纠正为 `#dashboard`；页面标题经当前语言翻译后写入；切换完成后重新应用当前语言、保持新页面直接显示在顶端、触发当前页标题/面板/表格行/短信摘要行渐进显示，并派发 `simpleadmin:page-changed` 事件，供页面脚本按可见状态启动或停止刷新。 |
| `SimpleAdmin.Spa.mountPage(page)` | 懒加载挂载指定页面的 Vue 应用；控制台内容区首次打开时才给内嵌 iframe 设置 `/console` 地址，避免进入首页时提前建立控制台连接。 |
| `SimpleAdmin.Spa.reloadConsole()` | 强制重载控制台内嵌 iframe（先清除再重新设置 `/console` 地址），用于重新建立控制台连接。 |
| `disableBrowserScrollRestore()` / `beginPageSwitch()` / `finishPageSwitchAtTop()` / `resetScrollPosition()` / `collectRevealItems(section)` / `applyProgressiveReveal(page)` / `scheduleProgressiveReveal(page)` | 禁用浏览器滚动位置恢复，页面切换时先隐藏内容区，使用 `history.pushState` / `replaceState` 更新地址栏以避免浏览器 hash 滚动，并立即通过 `scrollTop = 0` 重置全部滚动容器，再直接显示新页面顶端；刷新和 pageshow 后也重置到顶端；收集当前内容区中需要渐进显示的标题、面板、数据行和短信摘要行，按 DOM 顺序设置延迟，并在页面切换后一次性启动全部元素的渐进显示，不再等待滚动进入可视区域；渐进显示期间给当前页加 `sa-reveal-running` 状态，只临时隐藏面板/表格响应容器/收件箱/页脚等内部滚动条，不隐藏页面全局右侧滚动条，动效结束后自动恢复正常 overflow。 |
| Hash 路由 | 支持 `/#dashboard`、`/#signal`、`/#network`、`/#celllock`、`/#netdetail`、`/#netconfig`、`/#firewall`、`/#sms`、`/#atcommands`、`/#console`、`/#automation`、`/#sysmon`、`/#settings`、`/#deviceinfo`；当前 `www` 根目录只保留 `index.html` 和 `login.html`，不再依赖旧 HTML 跳转页。 |


### `development/simpleadmin/www/js/band_map.js`

模块型号与频段映射表。项目目标模块为移远 `RM520N-CN`（`TARGET_MODEL`）；默认频段集合 `DEFAULTS` 即该模块的硬件支持频段（LTE B1/3/5/8/34/38/39/40/41，NSA n41/78/79，SA n1/3/5/8/28/41/78/79，规格书口径），型号未识别或未读取到时按目标模块显示，不再回退到全频段大列表。

| 方法 | 功能 |
|---|---|
| `band_map.TARGET_MODEL` | 目标模块型号常量（`RM520N-CN`）。 |
| `SimpleAdmin.Bands.getBandsForModel(model, fallback)` | 根据模块型号返回 LTE、NSA、SA 支持频段；命中 `ENTRIES` 用该型号频段，未命中回退 `DEFAULTS`（目标模块频段）。 |

### `development/simpleadmin/www/js/populate-checkbox.js`

频段复选框生成和选中状态同步逻辑。

| 函数 | 功能 |
|---|---|
| `populateCheckboxes(lte_band, nsa_nr5g_band, nr5g_band, locked_lte_bands, locked_nsa_bands, locked_sa_bands, cellLock)` | 根据模块支持频段和当前锁定频段，同时生成 LTE、NR5G-NSA、NR5G-SA 三个模式的复选框。 |
| `addCheckboxListeners(cellLock)` | 给三个模式的频段复选框绑定变更事件，按 `data-band-mode` 同步对应模式的选中频段状态。 |


## 前端页面脚本

各页面脚本在普通模式下仍可通过 `mountSimpleAdminVueApp()` 挂载；在单页模式下会注册到 `SimpleAdmin.Pages`，由 `simpleadmin-spa.js` 在左侧菜单首次打开对应内容区时挂载。

### `development/simpleadmin/www/js/pages/index.js`

首页 Vue 数据与方法。

| 方法 | 功能 |
|---|---|
| `startDashboardRefresh()` / `stopDashboardRefresh()` / `fetchNetworkInfo()` | 仅在停留首页时通过 `/api/dashboard_data` 获取后端已解析的首页结构化 JSON；离开首页后停止自动刷新；首页 JS 不再包含 QENG/QCAINFO/QMAP 等 AT 协议解析逻辑。在线时长由响应中的 `uptimeParts` 解析承担，连通性直接使用后端 `internetConnection` 字段；请求失败时只标记失败状态，不刷新「更新时间」；响应带 `pending` 时保持旧值等待下一轮。 |
| `fetchHistory()` / `startHistoryRefresh()` / `stopHistoryRefresh()` | 停留首页时通过 `/api/history_data` 获取历史采样并每 60 秒刷新，渲染信号趋势 SVG 折线与最新下载/上传速率、时间范围；数据首次到达前显示骨架占位。 |
| `historySignalPoints()` 等 `history*` 方法 | 将历史采样换算为 SVG 坐标与展示文案。 |
| `_atCacheUpdatedHandler` | 监听后端 `at_cache_updated` 推送，防抖 800 毫秒后静默刷新首页数据，无需等待轮询间隔。 |
| `applyDashboardData(data)` | 将后端返回的 SIM、网络、信号、载波、流量、连通状态、CPU/RAM 使用量等业务字段直接同步到首页 Vue 数据，并在前端保留速率差值计算；`pending` 为 `true` 时直接返回，保持旧值等待下一轮。 |
| `formatActiveSimStatus()` | 将后端返回的 SIM 激活状态和卡槽编号组合成“已激活卡1”这类内容区展示文本，顶部状态卡不再单独显示 SIM。 |
| `clampPercent(value)` / `gaugeDashOffset(value)` / `formatPercent(value)` | 规范 CPU/RAM 百分比并驱动首页半圆仪表盘显示。 |
| `formatRamUsage()` | 将 RAM 已用量和总量组合成仪表盘副标题。 |
| `toggleNetworkCompact()` | 控制首页网络信息面板标题后的“精简显示/完整显示”按钮，精简显示时整行隐藏 MCCMNC、CELL ID、eNB ID 和 TAC，并把状态保存到浏览器本地存储。 |
| `parseUptimeParts(data)` | 从后台 uptime 文本中解析天、小时、分钟。 |
| `formatUptimeParts(parts)` | 根据当前语言格式化在线时长，英文模式显示 `days/hours/minutes`。 |
| `setUptimeParts(parts)` | 缓存已解析的在线时长，并在语言切换时重新格式化显示。 |
| `fetchTTL()` | 获取 TTL 状态。 |
| `setTTL()` | 设置 TTL 值。 |
| `init()` | 页面初始化时启动信号名称间距同步、拉取首页状态并启动自动刷新。 |

### `development/simpleadmin/www/js/pages/deviceinfo.js`

设备信息页 Vue 数据与方法。

| 方法 | 功能 |
|---|---|
| `fetchATCommand()` | 仅在停留设备信息页时通过 `/api/device_info_data` 读取设备信息；离开设备信息页后不再继续安排下一次刷新；请求不再默认强制刷新所有 AT，制造商、IMEI、固件、IMSI、ICCID、号码等低频字段走后端静态缓存，SIM/WWAN 状态按短缓存刷新；后台缓存尚未就绪或未解析到 IMEI 时会在设备信息页可见期间快速重试，平稳后按固定间隔刷新 SIM/WWAN 相关字段。 |
| `startDeviceInfoRefresh()` / `stopDeviceInfoRefresh()` | 根据 SPA 当前页面是否为设备信息页启动或停止设备信息自动刷新。 |
| `openContributors()` / `closeContributors()` | 打开或关闭 Project Contributors 弹框；弹框为与全站一致的自建 `modal-overlay` 模态，由工厂内 `showContributors` 状态驱动。 |
| `init()` | 页面首次挂载时绑定 `simpleadmin:page-changed` 事件；只有设备信息页处于当前页面时才启动自动刷新，并避免重复初始化。 |

### `development/simpleadmin/www/js/pages/network.js`

蜂窝网络页 Vue 数据与方法（频段锁定、锁频配置档、APN 等）。

| 方法 | 功能 |
|---|---|
| `getCurrentSettings()` | 通过 `action=settings` 读取当前 APN、PDP 类型、首选网络、NR5G 模式控制和已锁频段等设置；后端返回 `pending`（开机保护期未就绪）时不渲染默认值，按 3 秒间隔重试（最多 10 次）；切回本页时如此前状态未就绪自动重新拉取。 |
| `saveChanges()` | 把 PDP 类型/APN、首选网络模式、NR5G 模式控制变更通过 `action=save_settings` 一并提交；没有任何更改时提示并中止，提交后重新初始化页面。空 APN 允许提交（即「仅改 PDP 类型」合法），由后端保留模块当前 APN。 |
| `applyNetworkActionResult(result)` | 统一检查网络写操作返回；`ok === false` 或未收到响应（传输失败）均视为失败，弹出错误并中止后续成功倒计时/刷新，避免失败假装成功。 |
| `lockSelectedBandsForMode(mode)` | 按 LTE、NR5G-NSA 或 NR5G-SA 对应模式提交当前列已勾选频段进行锁定。 |
| `resetBandLocking()` | 发送全部 LTE/NSA/SA 可用频段恢复指令，并重新查询三种模式当前已锁定频段和网络状态。 |
| `getSupportedBands(force, mode)` | 查询当前已锁定频段；`mode` 为 `ALL` 时一次查询 LTE、NR5G-NSA 和 NR5G-SA 并回填三列勾选状态；`pending` 重试重建勾选时不覆盖用户已手动改动的勾选。 |
| `toggleBandCheckboxes(mode)` | 只切换指定模式频段复选框的全部选中/取消选中状态，并同步该模式准备锁定的频段列表。 |
| `saveBandProfile()` / `applyBandProfile()` / `deleteBandProfile()` / `loadBandProfiles()` | 锁频配置档：把当前三模式频段勾选、首选网络与 NR 模式控制保存到浏览器 `localStorage`（键 `simpleadmin.bandProfiles`），可命名保存、选择应用（仅填充界面不自动提交）与删除。 |
| `init()` | 蜂窝网络页初始化；直接应用硬编码型号 `RG520N-CN` 的固定频段集并渲染三模式可用频段，再并行读取当前网络设置和当前已锁定频段；切回本页时如此前设置或锁定频段未就绪则重新拉取。 |


### `development/simpleadmin/www/js/pages/celllock.js`

小区锁定页 Vue 数据与方法（小区扫描、手动锁定/解锁）。

| 方法 | 功能 |
|---|---|
| `startCellScan()` | 通过 `/api/network_data` 的 `action=scan` 执行小区扫描，把解析后的 NR5G/LTE 小区结果渲染到结果表；扫描进行中提示最长约 3 分钟；扫描失败时提示「扫描失败，请重试」，并清空已选小区与旧结果行，避免残留误导；后端返回 `empty: true`（AT 读取成功但解析出零个小区）时显示「扫描完成，模块未返回任何小区（该固件扫描功能可能受限）」，而非误导性的「未扫描到小区」；扫描响应携带 `pending: true` 时显示「模块尚未就绪，请稍后重试」。扫描模式下拉支持「邻区扫描」（`Neighbour Scan`，排在「全面扫描」前，后端执行 `AT+QNWCFG="nr5g_meas_info";+QENG="neighbourcell"`），该模式在本机固件上可用（QSCAN 失效时的可用扫描来源，推荐），表格/选择/锁定逻辑与 Full Scan 相同。 |
| `toggleCellSelection(pci, provider, type)` | 切换扫描结果小区的选中状态；Full Scan 模式下 NR5G 小区按单选、LTE 小区按多选（≤10）处理，选择项携带小区类型；Full Scan 双选 LTE+NR5G 时由锁定操作依次提交（先 NR5G 单小区锁定，成功后再 LTE 多小区锁定）。切换扫描模式时清空旧的选择与扫描结果，避免用过期小区发起锁定。 |
| `getLteCellCount()` | 读取并规范化 LTE 手动锁定的小区数量；空值、非法值或小于 1 时返回 0，最大限制为 10。 |
| `hasLteManualCellCount()` | 判断 LTE 手动锁定是否已经输入有效小区数量；未填写数量时不渲染任何 EARFCN/PCI 输入框。 |
| `visibleLteCellIndexes()` | 根据 LTE 小区数量生成可见输入组序号，只渲染当前需要的 EARFCN/PCI 输入组。 |
| `getLteManualValue(index, field)` / `setLteManualValue(index, field, value)` | 读写第 `index` 组 LTE EARFCN/PCI 输入值，兼容后端仍使用的 `earfcn1/pci1` 到 `earfcn10/pci10` 字段。 |
| `normalizeLteCellCount()` | 输入小区数量时自动限制到 1-10，并清空数量范围之外的旧 EARFCN/PCI 值。 |
| `onNetworkModeCellChange(event)` | 切换到 LTE 手动小区锁定时清空旧的小区数量和旧 EARFCN/PCI，避免上次输入残留。 |
| `getVisibleLteManualPairs(count)` | 只读取当前可见数量范围内的 LTE EARFCN/PCI 输入对，要求每一组填写完整，且 EARFCN/PCI 必须为纯数字。 |
| `cellLockEnableLTE()` / `cellLockDisableLTE()` | 设置或清除 LTE 手动小区锁定；设置时要求已填写小区数量且各组 EARFCN/PCI 填写完整、纯数字；锁定/解锁成功后复位手动锁定表单，避免误重复提交。 |
| `cellLockEnableNR()` / `cellLockDisableNR()` | 设置或清除 NR 手动小区锁定；设置时要求 PCI、EARFCN、SCS 和频段全部填写且为纯数字；锁定/解锁成功后复位手动锁定表单。 |
| `getCurrentSettings()` | 小区锁定页精简版设置读取：通过 `action=settings` 仅读取 `cellLockStatus`（小区锁定状态）；后端返回 `pending` 或 `error` 时按 3 秒间隔重试（最多 10 次），避免状态永久停在「未知」。 |
| `init()` | 小区锁定页初始化；注册 `onPageReturn('celllock')`（切回本页时如此前锁定状态未就绪则重新拉取）并读取当前小区锁定状态。 |


### `development/simpleadmin/www/js/pages/netconfig.js`

网络设置页 Vue 数据与方法。状态读写均通过 `/api/network_config_data`；各写操作共用 `isLoading` 防止重复提交。

| 方法 | 功能 |
|---|---|
| `fetchStatus(retry)` | 通过 `status` 动作读取 IP 透传、USB 协议、DNS 代理、DMZ 和 LAN IP 当前状态，用于状态概览徽章和各开关显示；请求带 `force=1` 绕过缓存；后端返回 `pending` 或识别出全默认值的不可信载荷时不渲染默认值并按指数退避自动重试（8 次，窗口覆盖 60s+ 开机保护期；此前已成功加载过时保留旧值），重试耗尽或请求失败时显示失败态，由 `retryStatus()` 提供重试入口；入口代际序号防止新旧请求/重试链竞态；状态未加载完成前页面各操作按钮禁用。 |
| `ipPassThroughEnable()` / `ipPassThroughDisable()` | 启用或禁用 IP 透传，启用需先选择 ETH/USB 模式，成功后回读状态；禁用时先提示并立即进入重启倒计时，再发送后端请求，避免第一条 `MPDN_RULE` 导致网口/WebSocket 断开时页面来不及显示重启界面。 |
| `onBoardDNSV4ProxyEnable()` / `onBoardDNSV4ProxyDisable()` / `onBoardDNSV6ProxyEnable()` / `onBoardDNSV6ProxyDisable()` | 开关 DNSv4/DNSv6 代理，统一经 `changeDNSProxy(family, enabled)` 提交，成功后回读状态。 |
| `usbNetModeChanger()` | 修改 USB 网络协议模式（RMNET/ECM/MBIM/RNDIS），成功后立即回读最新状态，再弹出重启确认弹窗；重启生效前页面显示「待重启生效」提示，倒计时结束且状态回读成功后清除。 |
| `setDMZEnable()` / `setDMZDisable()` | 启用或关闭 DMZ；启用前先用 `validIPv4()` 前端校验 IPv4 地址，非法时不发请求，成功后才更新开关状态。 |
| `setLANIP()` | 设置 LAN IP 地址范围和网关；前端校验网关 IPv4 格式、起始/结束地址为 1-254 的整数且起始不大于结束、网关末段为 1-254，并按网关前三段补全起始/结束地址后提交；保存成功后 LAN IP 保存按钮切换为“成功”状态并保持 3 秒，再恢复为“保存”，并回读状态确认已保存值。 |
| `validIPv4(ip)` | 前端 IPv4 格式校验（4 段、每段 0-255 纯数字）。 |
| `resolveDmzMode(mode, ip)` | 按后端返回的 DMZ 模式和 IP 判定页面显示的启用/禁用状态。 |
| `fetchTTL()` / `setTTL()` | 读取 TTL 状态，读取失败时显示失败态，由 `retryTTL()` 提供重试入口；设置 TTL 值（0-255，0 表示禁用），设置后重新读取状态。 |
| `handleRebootNotice(data)` | 读取后端返回的重启通知字段，收到 `reboot` 或 `rebooting` 后启动前端倒计时。 |
| `showRebootModal()` / `closeModal()` | 打开或关闭重启确认弹窗；USB 协议已修改但取消重启弹窗时标记「待重启生效」。 |
| `startRebootCountdown(seconds)` | 显示重启倒计时，按目标时间戳的真实时间计算剩余秒数（后台标签页不漂移），倒计时结束后延迟重新拉取网络设置状态。 |
| `rebootDevice()` | 调用 `/api/system_data` 的 `reboot` 动作重启模块并显示倒计时；应答为参数级错误（`ok:false` 且带 `error`）时取消倒计时并提示；发送重启命令后连接断开时继续保持倒计时。 |
| `setPageMessage(text, type)` | 显示并自动清除页面提示；消息条按语义色显示（默认错误红、`success` 成功绿、`info` 进行中主色）。 |
| `t(key)` | 调用公共语言模块翻译页面动态提示。 |
| `init()` | 网络设置页初始化；读取当前网络配置状态和 TTL 状态；注册 `simpleadmin:ip_passthrough_result` 监听，后台禁用透传失败时取消重启倒计时并提示；切回本页时自动重新拉取状态与 TTL。 |

### `development/simpleadmin/www/js/pages/firewall.js`

防火墙页 Vue 数据与方法，管理 SimpleFirewall 端口阻止/放行。页面由三个面板组成：「防火墙状态」面板显示启用状态徽章（`jumpInstalled`）、管理规则数（`ruleCount`，放行规则按 1 条、阻止规则按 4 条计）和规则语义说明（放行规则对所有接口生效；阻止规则在 bridge0、eth0、tailscale0 接口上放行，其余接口的对应入站连接被阻止），加载失败时提供重试；「端口规则」面板列出当前规则（端口 + 阻止/放行类型 + 删除），添加表单由端口输入与动作下拉（阻止/放行）组成，输入校验 1-65535 且同端口不重复添加，有未应用更改时提示，「应用更改」把当前列表整体原子保存（页面同时提示应用后立即生效、配置已持久化、重启后自动恢复、同一端口不能同时阻止和放行），「清空」清除全部规则；「所有防火墙规则」面板带刷新按钮，逐链展示后端返回的设备全部 iptables 链（链名 + 默认策略 + 数据包/字节计数 + 每条规则的 #/数据包/字节/目标/协议/入接口/出接口/源地址/目的地址/详情）。状态读写均通过 `/api/firewall_data`；应用操作经 `applying` 防重。数据字段：`serverRules`/`rules`（服务端与待应用规则列表，每条为 `{port, action}`）、`newPort`/`newAction`（添加表单，动作默认阻止）、`chains`/`ruleCount`/`jumpInstalled`（链统计与状态）、`isLoading`/`applying`/`statusLoaded`/`loadFailed`（加载与应用状态）、`firewallMessage`（页面提示）。

| 方法 | 功能 |
|---|---|
| `fetchStatus()` | 通过 `status` 动作读取当前规则列表（`rules`，每条 `{port, action}`）、启用状态（`jumpInstalled`）、管理规则数（`ruleCount`）和所有链统计（`chains`）；请求失败时标记失败态，由 `retryStatus()` 提供重试入口。 |
| `retryStatus()` | 状态加载失败后重试读取。 |
| `addRule()` | 按当前选中的动作把输入端口加入待应用列表；校验 1-65535 纯数字，且同端口不重复添加（不区分动作），非法或重复时提示且不发送请求。 |
| `removeRule(index)` / `clearRules()` | 删除单条规则 / 清空列表（均为待应用状态，不直接生效）。 |
| `hasChanges()` | 按端口数值排序比较当前列表与服务端列表，判断是否有未应用更改，控制「应用更改」按钮可用性。 |
| `saveRules()` | 通过 `save` 动作把当前列表按动作拆为 `block_ports`/`accept_ports` 整体提交，后端应用规则并持久化后回读状态；失败时显示错误提示；经 `applying` 防重。 |
| `formatBytes(n)` / `formatCount(n)` | 链统计人性化显示：字节数按 1024 进制换算为 B/KB/MB/GB，数据包数加千分位分隔。 |
| `actionLabel(action)` | 把动作值翻译为「放行」/「阻止」显示文本。 |
| `setPageMessage(text)` | 显示并自动清除页面提示。 |
| `t(key)` | 调用公共语言模块翻译页面动态提示。 |
| `init()` | 防火墙页初始化；拉取当前状态。 |

### `development/simpleadmin/www/js/pages/atcommands.js`

AT 命令页 Vue 数据与方法。手动 AT 通过 `/api/at_data` 发送。

| 方法 | 功能 |
|---|---|
| `sendATCommand()` | 通过 `manual_at` 动作发送用户输入的 AT 命令，返回后以“> 命令”格式追加到输出框并写入命令历史；输入为空时不发送任何命令。 |
| `runQuickCommand(cmd)` | 填入常用命令快捷按钮的预置 AT 命令并立即发送。 |
| `appendOutput(command, response)` | 把本次命令和响应追加到输出框，超出 200 行时只保留最近内容，截断点推进到下一条以 `> ` 开头的命令记录边界，避免留下孤儿响应残留。 |
| `copyOutput()` | 复制输出框内容到剪贴板，浏览器不支持时回退 `execCommand` 复制。 |
| `clearResponses()` | 清空 AT 输出。 |
| `loadHistory()` / `saveHistory()` / `pushHistory(command)` | 从浏览器 `localStorage`（键 `simpleadmin.atHistory`）读取、持久化和更新命令历史，去重后最多保留最近 50 条。 |
| `historyPrev()` / `historyNext()` | ↑/↓ 键召回上一条/下一条历史命令，翻过最新一条时恢复召回前的输入草稿。 |
| `openResetModal()` / `closeResetModal()` / `confirmResetAT()` | 重置 AT&F 需二次确认；确认后调用 `reset_at` 动作执行 `AT&F` 并等待后端结果，成功才清空输出并弹出重启确认弹窗，失败时显示失败提示且不弹重启确认；重置期间页面各按钮经 `isLoading` 禁用，防止重复触发。 |
| `showRebootModal()` / `closeModal()` | 打开或关闭重启确认弹窗。 |
| `startRebootCountdown(seconds)` | 显示重启倒计时，按目标时间戳的真实时间计算剩余秒数（后台标签页不漂移）。 |
| `rebootDevice()` | 调用 `/api/system_data` 的 `reboot` 动作重启模块并显示倒计时；发送重启命令后连接断开时继续保持倒计时。 |
| `setPageMessage(text, type)` | 显示并自动清除页面提示；消息条按语义色显示（默认错误红、`success` 成功绿、`info` 进行中主色）。 |
| `t(key)` | 调用公共语言模块翻译页面动态提示。 |
| `init()` | AT 命令页初始化；加载命令历史。 |

### `development/simpleadmin/www/js/pages/settings.js`

系统设置页 Vue 数据与方法。设置页只负责系统类设置（设备操作电源动作、IMEI、界面语言、默认主题、登录密码、TTL）；AT 终端和网络配置分别位于 AT 命令页（`atcommands.js`）和网络设置页（`netconfig.js`），自动化功能位于自动化页（`automation.js`）。默认主题保存为设备级默认值，供无本地主题记录的浏览器首屏使用。

| 方法 | 功能 |
|---|---|
| `rebootDevice()` | AT命令重启：二次确认弹窗（标题/文案/确认按钮可经 `SimpleAdmin.Reboot.request` 覆盖）确认后调用 `/api/system_data` 的 `reboot` 动作（`AT+CFUN=1,1`）重启蜂窝模块并进入 40 秒倒计时，倒计时结束后重新初始化设置页数据；设备本身保持运行。 |
| `rebootSystem()` / `startSystemReboot()` | 设备重启：红色确认弹窗二次确认后调用 `reboot_device` 动作（后端异步执行系统 `reboot` 命令），先启动 60 秒倒计时再发送命令（设备随即停机、WS 中断属常态），仅当后端明确返回失败（命令无法启动）时取消倒计时并提示，倒计时结束后重新初始化设置页数据。 |
| `poweroffDevice()` / `startPoweroff()` | 关机：红色确认弹窗二次确认（明示关机后须手动通电）后调用 `poweroff_device` 动作（后端异步执行系统 `poweroff` 命令）；成功弹出“设备已关机”提示弹窗，后端明确失败时仅提示错误；请求发出后传输中断时延时 6 秒探测免会话登录页，设备仍可达判定命令未送达只提示重试，不可达才弹出已关机弹窗。 |
| `openImeiModal()` | 校验新的 15 位数字 IMEI（且不能与当前 IMEI 相同），并打开 IMEI 修改确认弹窗。 |
| `updateIMEI()` | 调用 `/api/system_data` 的 `set_imei` 动作写入新 IMEI，并立即进入重启倒计时；应答为确定性失败（`ok:false` 且带 `error`）时取消倒计时并提示；设置后连接断开时继续保持倒计时；已进入倒计时时模块先重启后应答返回失败属预期，不显示误导错误提示。 |
| `fetchSystemStatus(retry)` | 通过 `/api/system_data` 的 `status` 动作（带 `force=1` 绕过缓存）读取当前 IMEI 并回填到 IMEI 设置输入框；后端返回 `pending` 时不渲染默认值并按指数退避自动重试（8 次，窗口覆盖 60s+ 开机保护期），重试耗尽或请求失败时显示失败态，由 `retrySystemStatus()` 提供重试入口；入口代际序号防止新旧请求/重试链竞态。 |
| `fetchLanguageSetting()` | 读取当前界面语言设置并同步到设置页选项。 |
| `saveLanguageSetting()` | 保存用户选择的中文或英文语言，并立即应用到界面。 |
| `changeLoginPassword()` | 校验当前密码、新密码和确认密码，调用后端接口修改登录密码；修改成功后显示「退出并重新登录」按钮，点击经注销流程跳转登录页。 |
| `fetchAutomationSettings()` / `saveAutomation()` | 读取并保存自动化设置：断网自愈看门狗（开关/连续失败次数/冷却分钟）、每日定时重启（开关/时刻）、新短信转发 Webhook（开关/URL），分别经 `/api/get_*` 与 `/api/set_*` 接口持久化；保存前前端先校验失败次数 1-60、冷却分钟 1-1440、时刻为 `HH:MM` 格式；保存经 `isSavingAutomation` 防重，配置加载完成前表单项与保存按钮禁用；保存结果按项列出失败明细，全部成功后提示已保存，保存完成后回读配置同步页面。 |
| `setAutomationMessage(text)` / `setSettingsMessage(text, type)` | 显示并自动清除自动化区域或设置页提示；设置页消息条按语义色显示（默认错误红、`success` 成功绿、`info` 进行中主色）。 |
| `t(key)` | 调用公共语言模块翻译设置页动态提示。 |
| `init()` | 设置页初始化；读取界面语言、系统状态（IMEI）和自动化设置；监听 `simpleadmin:page-changed`，切回本页时自动重新拉取系统状态和自动化设置。 |

### `development/simpleadmin/www/js/pages/sms.js`

短信页 Vue 数据与方法。

| 方法 | 功能 |
|---|---|
| `clearData()` | 清空短信列表、选择状态和当前短信详情弹窗状态。 |
| `requestSMS(options)` | 请求后端已解析短信列表；支持 `force` 强制重新读取和 `silent` 静默更新；长短信由后端优先按 PDU UDH 的 `concatRef` 在全列表范围内合并，即使分片索引中间夹着旧短信也会按 `concatSeq` 拼成一条；没有 UDH 拼接信息时才兼容按同发送方、5 秒内、连续存储索引分组，并按索引升序拼接后返回；短信正文包含 LF、CR、VT、FF、Unicode 行分隔符或文本形式 `\n` / `\r\n` 时，后端统一规范为换行并返回 `textLines`，收件箱先按单行摘要显示发件人、时间和部分内容，点击摘要行后用安全文本节点和 `<br>` 弹出完整内容；文本模式正文保留 `+` 开头的行（只跳过已知协议行），时间戳被逗号拆断时自动拼回。 |
| `applySMSData(data, options)` / `pushSMSMessage(sender, date, text, indices)` | 将后端完整短信列表写入前端收件箱；自动刷新时只有确认短信索引变化且稳定后才调用，并尽量保留当前详情弹窗和勾选状态。 |
| `parseCustomDate(dateStr)` | 解析后端返回的短信时间，先剥离 `±hh` 时区后缀；解析失败返回 `null`（不回退当前时间），收件箱时间显示占位 `-`。 |
| `formatDate(date)` | 格式化短信时间。 |
| `getMessagePreview(message)` | 把短信多行正文压缩成单行摘要内容，用于收件箱列表。 |
| `openMessageDetail(index)` / `closeMessageDetail()` | 打开或关闭短信详情弹窗，弹窗显示当前短信发件人、时间和完整内容。 |
| `startSMSAutoRefresh()` / `stopSMSAutoRefresh()` / `autoRefreshSMS()` / `handlePolledSMSMeta()` / `fetchStableSMSList()` | 仅短信页处于当前页面时按固定时间轮询 `/api/sms_data` 的 `list_meta`，每次用 `force=1` 强制读取短信索引元信息；前端记录当前短信索引集合，索引没有变化时不请求完整短信正文、不更新页面，WebSocket 返回不包含 `text` / `textLines`；发现索引变化后先缓存元信息，等待连续轮询确认索引稳定和分片收齐后，再请求完整列表并一次性更新收件箱；离开短信页后停止轮询并清空待确认结果。 |
| `deleteSelectedSMS()` | 删除选中的短信，包含拼接短信的所有分段索引；前端只提交索引，后端生成删除指令；删除失败时通知失败原因。 |
| `deleteAllSMS()` | 删除全部短信；删除失败时通知失败原因。 |
| `sendSMS()` | 只提交收件人号码和短信正文，经 `isSending` 防止重复发送，由 Go 后端通过 `AT+CIMI` 读取 IMSI、取前三位 MCC 映射国家/地区呼叫码，给非 `+` 开头号码自动添加 `+呼叫码`，再切换到 `AT+CMGF=0` PDU 模式完成 UCS2 PDU 编码、分段发送和发送结果判断。 |
| `showNotification(message, type)` | 显示短信页提示。 |
| `toggleAll(event)` | 全选或取消全选短信。 |
| `init()` | 页面初始化，强制读取短信并在短信页可见时启动自动刷新。 |

### `development/simpleadmin/www/js/pages/automation.js`

自动化页 Vue 数据与方法，包含断网自愈看门狗、每日定时重启、时间同步与短信转发四个面板。

| 方法 | 功能 |
|---|---|
| `fetchWatchdog()` / `retryWatchdog()` / `saveWatchdog()` | 读取与保存看门狗配置（开关、连续失败次数 1-60、冷却分钟 1-1440、检测间隔 1-30 分钟、探测目标、自愈动作策略）；保存前逐项校验，保存后回读配置同步页面。 |
| `lastActionText()` | 组合上次自愈时间与动作类型（无线电重注册/重启模块）的展示文本，从未执行时显示“从未”。 |
| `fetchScheduler()` / `retryScheduler()` / `saveScheduler()` | 读取与保存每日定时重启配置（开关、`HH:MM` 时刻），保存前校验时刻格式。 |
| `fetchTimeSync()` / `retryTimeSync()` / `saveTimeSync()` | 读取与保存时间同步配置（开关、同步间隔 1-1440 分钟、单一 NTP 服务器地址，缺省阿里云 `ntp.aliyun.com`）；保存后回读并显示轮询器状态、上次同步时间/偏差与当前系统时间。 |
| `syncTimeNow()` | 手动同步一次：调用 `/api/timesync_now` 立即从配置的 NTP 源查询并校正系统时间，按结果显示成功（含偏差毫秒数）或失败通知，完成后回读状态；同步进行中按钮禁用并显示“同步中…”。 |
| `lastTimeSyncText()` | 组合上次同步时间与结果（成功显示偏差毫秒、失败显示“失败”）的展示文本，从未同步时显示“从未”。 |
| `fetchWebhookSetting()` / `retryWebhook()` / `saveWebhook()` | 读取与保存短信转发 Webhook 配置（开关、URL）。 |
| `init()` | 页面初始化，拉取四类配置并在页面切回时重新拉取。 |

### `development/simpleadmin/www/js/pages/sysmon.js`

系统监控页 Vue 数据与方法，展示效果类似 htop：CPU/内存占用卡片加进程 Top 20 表格。

| 方法 | 功能 |
|---|---|
| `fetchMonitor()` | 通过 `/api/system_monitor?sort=cpu|mem` 获取 CPU/内存占用、负载、运行时长、进程总数与进程 Top 20 列表并更新页面；入口按页面可见性拦截——系统监控页未显示时任何路径都不发起请求，避免后台无谓读取设备 `/proc` 增加压力；带在途请求守卫，失败时标记失败态并提供重试。 |
| `retryMonitor()` | 清除失败标记并重新拉取。 |
| `startPolling()` / `stopPolling()` | 停留系统监控页时每 3 秒自动刷新（经 `SimpleAdmin.Poll` 创建，后台标签页自动暂停）；切换到其他页面时由页面切换事件停止轮询，切回时恢复并立即刷新；轮询器每次心跳经 `fetchMonitor` 的页面可见性守卫二次拦截。 |
| `clampPercent(value)` / `gaugeDashOffset(value)` | 规范百分比并驱动 CPU/内存半圆仪表盘显示。 |
| `formatRamUsage()` | 将内存已用量和总量组合成仪表盘副标题。 |
| `formatCpu(value)` / `formatNum(value)` | 进程表 CPU 百分比（保留一位小数，多核可超 100%）与内存百分比格式化。 |
| `init()` | 页面初始化，绑定页面切换事件以启停轮询，并立即拉取一次数据。 |

## Go 后端源码 `go-build/simpleadmin-go/`

### `go-build/simpleadmin-go/go.mod`

Go 模块声明文件，模块名为 `simpleadmin-go`。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/main.go`

主程序入口与 `serve` 子命令启动流程。原单文件已按职责拆分为模块化结构，各文件分工如下：

| 文件 | 职责 |
|---|---|
| `main.go` | 子命令分发（`serve`/`at`/`ttl`）、启动参数、默认路径常量；启动时拉起看门狗、定时任务与时间同步轮询器。 |
| `server_routes.go` | `simpleAdminServer` 结构、HTTP 路由表、内部业务 handler 映射、静态文件版本号注入、可选直连 HTTP API（`-direct-api`）。 |
| `server_auth.go` | 会话认证中间件、登录/注销、会话 Cookie 管理、登录失败限流。 |
| `server_config.go` | 认证文件读写、改密、语言配置读写。 |
| `server_tls.go` | 保留的 HTTPS 自签证书工具（默认 HTTP-only 不使用）。 |
| `api_handlers.go` | AT 缓存、ping、短信、TTL、uptime、历史数据、看门狗/定时任务/时间同步/短信转发配置等小型 API handler。 |
| `at_cli.go` | `at` 调试子命令与 AT 调试日志工具。 |
| `at_runner.go` | AT 事务执行：超时策略、候选设备重试、短信发送事务、设备候选过滤、回显关联读取。 |
| `at_command_util.go` | AT 命令清理、组合命令拆分、回显标记定位工具。 |
| `at_cache_policy.go` | AT 命令分类与缓存分级策略（动作/静态/配置/半静态/实时）。 |
| `smd_reader.go` | `/dev/smd*` 常驻读取器子进程、缓冲与排空、回显关联读取实现（仅 Linux）。 |
| `metrics_history.go` | 信号/资源/流量历史采样环形缓冲（每分钟一条，保留 24 小时，内存保存）。 |
| `watchdog.go` | 断网自愈看门狗：周期联网检测，连续失败达阈值且过冷却期后重启模块。 |
| `scheduler.go` | 每日定时重启调度器。 |
| `timesync.go` | 时间同步：按配置间隔轮询单一 NTP 源（缺省阿里云 `ntp.aliyun.com`）并直接步进修改系统时间，另提供手动同步一次。 |
| `native_timesync.go` / `native_timesync_stub.go` | Linux `settimeofday` 修改系统时间实现与非 Linux 平台桩。 |
| `system_monitor.go` | 系统监控页 `/api/system_monitor`：CPU/内存占用、负载、进程 Top 20（按 CPU 或内存排序）。 |
| `sms_webhook.go` | 新短信到达时向配置的 Webhook 地址推送通知；含 30 秒后台轮询器（按配置启停，关闭即停），无人打开网页时也能触发转发。 |
| `version.go` | 构建注入版本号与 HTML `?v=__SA_VERSION__` 占位符替换。 |
| `console_page.go` | Web 控制台内嵌终端页面 HTML/JS 常量（仅 Linux）。 |
| `mock_at_responses.go` | mock 模式 AT 响应常量与生成函数（原型：目标模块移远 RM520N-CN）。 |
| `http_util.go` | `writeText`/`writeJSON` 等 HTTP 输出工具。 |

#### 启动与配置

| 类型/函数 | 功能 |
|---|---|
| `serverConfig` | 保存服务端静态目录、认证文件、HTTP/HTTPS 监听地址、证书文件参数、TTL 文件、防火墙规则配置文件（`-firewall-file`，默认 `/usrdata/simpleadmin/firewall_ports.conf`，新格式每行 `<action> <port>`，如 `block 80`/`accept 8080`；兼容旧格式纯端口行，视为阻止）、AT 配置、`--no-tls`、mock 模式、`--direct-api` 等启动参数；模块端默认使用 HTTP-only。 |
| `authConfig` | 保存页面登录使用的用户名和密码。 |
| `languageConfig` | 保存界面语言配置。 |
| `main()` | 命令入口，根据子命令分发到 `serve`、`at`、`ttl`。 |
| `runServeCommand(args)` | 解析服务参数，初始化认证、AT 候选、TTL、防火墙和路由，并启动断网自愈看门狗、每日定时重启调度器与时间同步轮询器（均按配置启停）；默认只启动 HTTP，传入 `--no-tls=false` 时才会走保留的 HTTPS 启动路径，HTTPS 启动时 HTTP 重定向目标在 HTTPS 监听端口非 443 时保留端口号；`--direct-api`（默认取 `SIMPLEADMIN_DIRECT_API`）为真时把业务 handler 同时注册为直连 HTTP 路由。 |

#### HTTP 路由与认证（`server_routes.go` / `server_auth.go` / `server_config.go` / `server_tls.go`）

| 函数 | 功能 |
|---|---|
| `(*simpleAdminServer).routes()` | 创建 HTTP 路由，公开 `login.html`、`/api/login`、`/api/logout`，登录后通过会话 Cookie 访问 `/api/ws`、`/api/console/ws`、控制台页面和静态文件服务；普通业务接口不再直接暴露为 HTTP 路由。 |
| `nativeAPIHandlers()` | 返回内部 `/api/*` 业务 handler 映射，供 `/api/ws` 分发复用。 |
| `legacyCGIAliases()` | 保留旧 `/cgi-bin/*` 映射函数，但默认路由不再注册兼容入口。 |
| `registerNativeAPIRoutes()` | 保留旧注册函数，默认启动流程不再调用。 |
| `registerLegacyCGIAliases()` | 保留旧注册函数，默认启动流程不再调用。 |
| `handleAPINotFound()` | 返回未知 API 错误。 |
| `handleLegacyCGINotFound()` | 返回未知 CGI 兼容接口错误。 |
| `sessionAuth()` | 为页面、静态文件、WebSocket 和业务 API 加页面登录会话校验；未登录访问页面时重定向到 `login.html`，未登录访问 API 时返回 JSON 401，不设置 `WWW-Authenticate`，避免浏览器弹出 Basic Auth 登录框。 |
| `isPublicAuthPath()` | 判断登录页、登录/注销 API 和公开模块型号接口 `/api/module_model` 是否属于免会话访问路径。 |
| `handleLoginPage()` | 输出页面登录界面；如果已有有效会话则直接进入单页管理界面。 |
| `handleLogin()` | 校验用户名和密码，成功后创建随机会话令牌并写入 HttpOnly Cookie。 |
| `handleLogout()` | 清除当前会话 Cookie；POST 请求返回 JSON，GET 或 HTML 请求重定向到 `login.html`。 |
| `writeAuthRequired()` | 未登录访问保护资源时，页面请求重定向到登录页，API 请求返回 JSON 401。 |
| `isRequestAuthenticated()` | 从请求 Cookie 中读取会话令牌并校验是否有效。 |
| `createSession()` | 生成随机会话令牌并记录过期时间。 |
| `validateSession()` | 校验会话令牌是否存在且未过期，并刷新会话有效期。 |
| `destroyRequestSession()` | 删除当前请求 Cookie 对应的服务端会话。 |
| `setSessionCookie()` / `clearSessionCookie()` | 写入或清除页面登录会话 Cookie。 |
| `constantTimeEqual()` | 常量时间字符串比较，避免认证比较泄露。 |
| `validateStaticDir()` | 检查静态目录是否存在且是目录。 |
| `ensureAuthFile()` | 确保认证文件存在，不存在时创建默认认证。 |
| `loadAuthConfig()` | 读取认证文件。 |
| `writeAuthConfig()` | 原子写入认证文件，用于保存修改后的登录密码。 |
| `validateNewPassword()` | 校验新密码非空、长度和换行符限制。 |
| `ensureManagedTLSCertificate()` | 保留的 HTTPS 证书生成工具函数；模块端默认 HTTP-only 启动流程不调用。 |
| `ensureLocalCACertificate()` | 保留的本地 CA 读写工具函数；模块端默认 HTTP-only 启动流程不调用。 |
| `loadLocalCACertificate()` | 读取本地 CA 证书和私钥，并校验 CA 属性和有效期；仅供保留 HTTPS 路径使用。 |
| `serverCertificateMatches()` | 校验服务器证书是否由当前本地 CA 签发、是否包含服务器认证用途和必需 IP SAN；仅供保留 HTTPS 路径使用。 |
| `writeServerCertificate()` | 生成新的 HTTPS 服务器证书，包含 `192.168.225.1`、本机接口 IP、`localhost` 等 SAN。 |
| `requiredTLSCertificateDNSNames()` | 返回服务器证书需要包含的 DNS SAN。 |
| `requiredTLSCertificateIPs()` | 返回服务器证书需要包含的 IP SAN，包括 `192.168.225.1` 和当前接口 IP。 |
| `languageConfigPath()` | 返回语言配置文件在静态目录中的完整路径。 |
| `normalizeLanguage(value)` | 规范化语言值，只允许 `zh-CN` 和 `en`。 |
| `readLanguageConfig(path)` | 从 JSON 文件读取语言配置，无效或缺失时使用默认语言。 |
| `writeLanguageConfig(path, language)` | 写入语言配置 JSON 文件。 |

#### API handler（`api_handlers.go` / `server_config.go`）

| 函数 | 功能 |
|---|---|
| `handleGetATCache()` | 处理 AT 缓存读取请求，优先支持 POST 表单参数，同时兼容旧 GET 查询参数，必要时触发后台队列刷新。 |
| `handleGetATCommand()` | 兼容旧 AT 接口，内部转到 AT 缓存处理。 |
| `handleGetPing()` | 返回网络连通检测结果。 |
| `handleGetSMS()` | 通过 AT 后台缓存获取短信列表。 |
| `handleSendSMS()` | 发送短信，支持 POST 表单 body 参数。 |
| `handleGetTTLStatus()` | 返回 TTL 启用状态和 TTL 值。 |
| `handleSetTTL()` | 设置 TTL 值；mock 模式走模拟应用（不执行真实 iptables），仍按与真实路径一致的逻辑持久化配置值。 |
| `handleGetUptime()` | 返回系统运行时间。 |
| `handleGetLanguage()` | 返回当前界面语言配置。 |
| `handleSetLanguage()` | 保存界面语言配置。 |
| `handleGetTimeSync()` | 返回时间同步配置与运行状态（开关、间隔、服务器、轮询器状态、上次同步时间/结果/偏差、当前系统时间）。 |
| `handleSetTimeSync()` | 保存时间同步配置（开关、间隔分钟 1-1440、单一 NTP 服务器），保存后按配置启停轮询器。 |
| `handleTimeSyncNow()` | 手动同步一次：立即从配置的 NTP 源查询并步进修改系统时间，返回本次同步结果。 |
| `handleSystemMonitor()` | 返回系统监控数据：CPU/内存占用、负载、运行时长、进程总数与进程 Top 20（`sort=cpu|mem`，非法值按 CPU 排序）。 |
| `handleSetPassword()` | 校验当前密码后更新页面登录密码。 |

#### AT 与短信（`at_cli.go` / `at_runner.go` / `at_command_util.go`）

| 函数 | 功能 |
|---|---|
| `envBool()` | 解析环境变量布尔值。 |
| `logATDebug()` | 输出 AT 调试日志。 |
| `atPayloadSummary()` | 生成 AT payload 调试摘要。 |
| `outputSummary()` | 生成 AT 输出调试摘要。 |
| `logATDeviceStat()` | 输出 AT 设备 stat 信息。 |
| `runATCLICommand()` | `simpleadmin-httpd at` 子命令入口。 |
| `atCommandTimeoutMS()` / `atGroupedCommandTimeoutMS()` / `atSingleCommandTimeoutMS()` | 根据 AT 命令类型选择超时时间；分号组合 AT 按其中各子命令估算总等待时间，避免页面提前返回 pending；含 `+CMGL=4` 的组合命令额外追加 15 秒读取预算，避免短信较多时半截响应被当作有效数据；含 `NR5G_MEAS_INFO` 的邻区测量查询给 3 秒（手册未收录命令，模块整理测量结果需要时间，默认 1 秒会假超时）。 |
| `runATCommandUntilDone()` | 后台队列实际发送 AT 命令并等待完成；收到什么 AT payload 就发送什么，不再在底层做通用分号拆分。 |
| `runATCommand()` | 后台队列实际发送 AT 命令并等待 OK/ERROR；收到什么 AT payload 就发送什么。 |
| `lockForATDevice()` | 获取指定 AT 设备的进程内互斥锁。 |
| `runATPayload()` | 发送 AT payload。 |
| `runATPayloadWaitFor()` | 发送 payload 并等待指定终止 token。 |
| `runATPayloadAcrossCandidates()` | 按候选设备尝试发送 AT；默认只有 `/dev/smd11`，失败时只按超时/错误重试，不创建或重建桥接。 |
| `existingATDeviceCandidates()` | 过滤出当前存在的 AT 设备。 |
| `tryATPayloadOnce()` | 对候选设备执行一次 AT 尝试。 |
| `nativeATWriteRead()` | 通过常驻 `/dev/smd11` 读取端和短生命周期写入端执行 AT，读取直到 OK/ERROR 或超时。 |
| `shouldTryNextATDevice()` | 判断错误是否允许尝试下一个 AT 设备。 |
| `runSMSTransaction()` | 执行 PDU 模式短信发送事务，直接通过 `/dev/smd11` 写入 `AT+CMGF=0;+CMGS=<TPDU长度>` 后等待 `>` 提示，再写入完整 PDU 和 Ctrl+Z。 |
| `trySMSTransactionOnce()` | 对候选设备执行一次短信发送尝试。 |
| `sendSMSOnDevice()` | 在指定设备上执行短信发送；开始前先发一次 ESC 清理可能滞留的 CMGS 输入态，任一环节失败后再发 ESC 让模块退回命令态，避免残留输入态吞掉后续 AT 命令。 |
| `atDeviceCandidates()` | 返回当前 AT 候选设备。 |
| `collectATDeviceCandidates()` | 合并环境变量、参数、配置文件和默认候选设备，并只保留允许的 `/dev/smd11`。 |
| `defaultATDeviceCandidates()` | 返回默认 AT 设备 `/dev/smd11`。 |
| `filterFixedATDeviceCandidates()` | 过滤允许使用的 AT 设备，只允许 `/dev/smd11`，避免把 `/dev/smd7` 作为 Go 后端 AT 通道。 |
| `uniqueDevicePaths()` | 去重设备路径。 |
| `containsAnyToken()` | 检查输出是否包含 OK、ERROR 或其它终止 token。 |
| `sanitizeATCommand()` | 清理 AT 命令输入。 |
| `splitATCommandParts()` | 仅用于估算组合 AT 的等待时间，不负责改写或拆分实际发送 payload。 |
| `sanitizeSMSCommandMeta()` | 清理短信分段元数据。 |
| `stripNonDigits()` | 去除非数字字符。 |

#### Mock 与输出工具（`mock_at_responses.go` / `http_util.go`）

| 函数 | 功能 |
|---|---|
| `mockATResponse()` | 本地测试模式 AT 响应；按命令分发到各 mock 用例，首页 AT 会优先使用 Windows 控制台或浏览器开发者控制台手动输入的测试数据。 |
| `mockDeviceIdentityResponse()` / `mockDeviceSIMStatusResponse()` / `mockWWANStatusResponse()` | mock 设备信息、SIM/WWAN 状态、WWAN 查询响应。 |
| `mockNetworkSettingsResponse()` / `mockSettingsStatusResponse()` | mock 蜂窝网络页、网络设置页组合查询响应；前者覆盖 CGDCONT、QNWLOCK、模式偏好等，后者覆盖 `MPDN_rule`、DNS 代理、usbnet、DMZ 等，并对 `AT+QMAP="DHCPV4DNS"` 区分读写：带参数的写命令返回 `OK`，裸查询仍按真实固件行为以 `ERROR` 收尾。 |
| `mockQNWPREFCFGResponse()` | mock 频段/模式偏好查询，频段列表取自真实设备（LTE `1:3:5:8:34:38:39:40:41`、NSA `1:8:28:41:78`、SA `1:8:28:41:78`）。 |
| `mockQTEMPSensorLines()` | 返回真实设备的 17 路温度传感器 mock 行。 |
| `currentMockDashboardATResponse()` | 生成首页 mock AT 返回，支持整段 AT、QCAINFO、QENG 三类手动覆盖。 |
| `setMockATPayload()` | 保存 Windows 控制台或浏览器开发者控制台输入的 AT 测试数据并让 AT 缓存失效。 |
| `currentMockDashboardParseJSON()` | 将当前 mock 首页 AT 按 `/api/dashboard_data` 逻辑解析成 JSON，供 Windows 控制台 `parse` 命令查看。 |
| `mockSMSList()` | 本地测试模式短信列表；含一条 10001 中文 UCS2 短信和一条英文短信，用于验证解析与合并逻辑。 |
| `mockSMSSendResponse()` | 本地测试模式短信发送响应。 |
| `mockUptimeText()` | 本地测试模式运行时间。 |
| `writeText()` | 输出纯文本 HTTP 响应。 |
| `writeJSON()` | 输出 JSON HTTP 响应。 |
| `writeJSONAndFlush()` | 输出 JSON 后在响应对象支持时立即 flush，供需要尽快推送响应的 handler 使用。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/system_metrics.go`

读取系统资源占用并输出首页仪表盘使用的数据。

| 函数 | 功能 |
|---|---|
| `systemResourceMetrics(mock)` | 返回首页 CPU 使用率、RAM 使用率、RAM 已用量和 RAM 总量；mock 模式返回固定测试数据。 |
| `currentCPUUsagePercent()` | 读取 `/proc/stat` 并按两次采样差值计算 CPU 使用率。 |
| `currentRAMUsage()` | 读取 `/proc/meminfo`，优先用 `MemAvailable` 计算 RAM 已用量和百分比。 |
| `humanBytesFromKiB(valueKiB)` | 将 KiB 数值格式化为 KB/MB/GB/TB 文本。 |

### 页面级结构化 API（原 `structured_api.go`，已按页面域拆分）

页面级结构化 API。前端只提交页面动作和业务参数，后端负责映射 AT、读取缓存、解析返回并输出页面可直接使用的 JSON。原 2000+ 行单文件已按页面域拆分：

| 文件 | 职责 |
|---|---|
| `page_at_commands.go` | 页面↔AT 命令组契约（`pageATCommands`）、缓存读取与动作执行基础设施。 |
| `module_model.go` | 单独型号查询（`AT+CGMM`）运行期缓存与解析。 |
| `page_dashboard.go` | 首页：`/api/dashboard_data`、`parseDashboardAT` 及 QENG/QCAINFO/QRSRP 解析、信号百分比计算。 |
| `page_signal.go` | 信号详情页：`/api/signal_data`、`parseSignalDetailAT` 解析四天线 RSRP（`+QRSRP`）、服务小区（`+QENG="servingcell"`）与载波聚合主/辅载波列表（`+QCAINFO`，`parseSignalCarriers`）。页面专属组合命令，不参与周期缓存刷新，仅该页请求时拉取。 |
| `page_deviceinfo.go` | 设备信息页：`/api/device_info_data` 与 `parseDeviceInfoAT`。 |
| `page_network.go` | 蜂窝网络页：频段/模式设置、扫网、锁小区、`parseNetworkSettingsAT`/`parseCellScanAT`。 |
| `page_at_data.go` | AT 命令页：`/api/at_data`（`manual_at` 任意 AT 透传、`reset_at` 执行 `AT&F`）。 |
| `page_network_config.go` | 网络设置页：`/api/network_config_data` 状态与 IP 透传、DNS 代理、USB 协议、DMZ、LANIP 动作、`parseNetworkConfigStatusAT`。 |
| `page_firewall.go` | 防火墙页：`/api/firewall_data`（`status`/`save`）、规则模型 `firewallRule{Port,Action}`、阻止/放行规则解析校验 `validateFirewallRules`（1-65535、同端口同动作去重、同端口冲突检测、上限 64）、配置文件读写（新格式每行 `<action> <port>`，兼容旧格式纯端口行）、`parseIPTablesChainDump` 解析 `iptables -vnL -x --line-numbers` 全部链统计（链头 policy/references 双形态、K/M/G 计数、每链规则上限 300 条）与 `mockChainsForRules` mock 合成链。 |
| `page_system.go` | 系统设置页：`/api/system_data` 状态（IMEI）、修改 IMEI、AT 重启（`reboot`）、设备整机重启（`reboot_device`，异步执行系统 `reboot`）与关机（`poweroff_device`，异步执行系统 `poweroff`，仅命令无法启动时返回失败）、`parseSystemStatusAT`。 |
| `page_sms.go` | 短信页：列表解析（PDU/UDH 拼接、合并）、发送业务、删除。 |
| `at_parse_util.go` | 共享解析工具：`atLines`/`csvFields`/`decodeMaybeUCS2`/`stringValue` 等；`atLines` 统一过滤后台未就绪提示与 AT 运行器错误文本（候选设备失败/超时/串口错误等），不再混入厂商、型号等页面数据。 |

| 函数 | 功能 |
|---|---|
| `runPageAction(command)` | 执行页面动作类 AT，强制进入 AT 后台队列并等待返回。 |
| `runPageActionsOK(commands, delay)` | 按顺序执行多条动作类 AT；每条收到 `OK` 后按指定延迟继续，任意一步失败即停止并返回已收到响应。 |
| `runDelayedPageAction(command, delay)` | 延迟执行单条动作类 AT。 |
| `runDelayedPageActionsOK(commands, startDelay, stepDelay)` | 先延迟启动，再按顺序执行多条动作类 AT；每条收到 `OK` 后按指定间隔继续，适用于先通知前端、再后台执行会断开网口的重启类流程。 |
| `settingsRebootNoticeResponse(response, rebootAfter)` | 生成重启通知 JSON（网络设置页禁用 IP 透传时使用），包含 `reboot`、`rebooting`、`rebootAfterSeconds` 和 `rebootCountdownSeconds`。 |
| `handleDashboardData()` | `/api/dashboard_data` 入口，返回首页 SIM、联网、信号、小区、载波、流量、uptime 和 CPU/RAM 使用量结构化数据；开机保护期/后台未就绪时携带 `pending` 标记，供前端保持旧值等待下轮。 |
| `parseDashboardAT(raw)` | 解析首页 AT 返回；`QSIMSTAT` 明确未插卡或未激活时作为最高优先级，调用 `applyDashboardSIMAbsent()` 清空旧网络、旧 IP 和旧信号数据；`QCAINFO` 按每条载波保留频段和带宽，不再去重相同频段/带宽；NR5G SCC 行按第二个 PCI 字段解析。LTE `servingcell` 的 EARFCN 取第 8 个字段（第 9 个是频段号）；LTE SINR 原始值按 `2X-20` 换算成 dB 再映射百分比；信号字段为 `-` 等非法值时百分比记 0，不显示虚假的“优秀”；`CSQ=99` 表示不可知显示 `-`；`QRSRP` 的 `-32768` 等天线占位值显示 `-`；`QMAP:"WWAN"` 的 `0.0.0.0`/全零 IPv6 不视为已激活。 |
| `parseCellScanAT(raw)` | 解析小区扫描结果；兼容新固件 13-16 字段与旧固件 9 字段两种 `+QSCAN` 行（核心字段 MCC/MNC/ARFCN/PCI/RSRP 索引一致，9 字段时频段置 `-`），不再因字段不足丢弃整行。 |
| `parseNeighbourCellAT(raw)` | 解析邻区扫描（模式 `Neighbour Scan`）的原始响应：NR 行 `+QNWCFG: "nr5g_meas_info"` 取 NR-ARFCN/PCI/RSRP（频段由 `nrARFCNToBand` 推导），LTE `neighbourcell intra/inter` 行取 earfcn/PCID/RSRP；结果复用现有扫描结果结构与选择→锁定流程。 |
| `nrARFCNToBand(arfcn)` | 纯函数：由下行 NR-ARFCN（NREF）推导频段；数据源为 3GPP 38.104 Table 5.4.2.3-1 各频段下行频点区间，重叠区间按优先级首匹配；仅供锁小区时填充 band 参数。 |
| `inferNRSCSFromBand(band)` | NR 锁小区未提供 SCS 时按频段推断子载波间隔（n1/n3/n5/n8/n20/n26/n28/n66 等 FDD 频段为 15，其余为 30），避免写死 30 导致 15kHz 小区锁频参数不匹配；前端不再写死 `scs: 30`。 |
| `applyDashboardSIMAbsent(data)` | 将首页状态重置为未插卡/未激活，避免 SIM 拔出后因 `QMAP/CGCONTRDP/QENG` 旧缓存继续显示已激活。 |
| `parseDeviceInfoAT(raw)` | 解析设备信息 AT 返回；以 `QSIMSTAT/CPIN` 判断 SIM 是否插入并输出 `simStatus`/`simInserted`，未插卡时只清空 IMSI、ICCID、WWAN 和号码等 SIM/蜂窝数据，制造商、固件版本、IMEI、局域网 IP 继续按 AT 返回正常显示；型号名称不再从组合 AT 中推断，由单独 `AT+CGMM` 结果填充；已插卡但 `CNUM` 没有号码时显示“无本机号码”。 |
| `handleDeviceInfoData()` | 设备信息读取接口（`set_imei` 动作兼容保留，前端页面修改 IMEI 走 `/api/system_data`）；读取时默认使用后端缓存，设备信息 AT 分为静态信息 `CGMI/CGSN/QGMR/CIMI/ICCID/CNUM`、短缓存 SIM/WWAN 状态 `QSIMSTAT/CPIN/QMAP=WWAN`、独立配置缓存 `QMAP=LANIP`；型号名称通过全局单独型号查询入口填充，后端已有成功缓存时直接复用，不再重复发送 `AT+CGMM`。 |
| `handleNetworkData()` | 蜂窝网络页设置、扫网、锁频、解锁、保存和单独型号查询接口；型号查询优先使用后端运行期缓存，缓存为空时才发送 `AT+CGMM` 并在短时间内重试；`scan` 动作支持 `Neighbour Scan` 模式（邻区扫描，执行 `AT+QNWCFG="nr5g_meas_info";+QENG="neighbourcell"`，结果由 `parseNeighbourCellAT` 解析，其余模式仍走 `+QSCAN`）；`scan` 动作 AT 读取成功但解析出零个小区时额外返回 `empty: true`，供前端区分「固件无结果」与「未扫描」；`settings` 动作在后台未就绪时同样携带 `pending` 标记；`save_settings` 空 APN 时跳过 APN 重写，保留模块当前 APN；`lock_lte_manual` 有效参数对数量与 `cellNum` 不匹配时返回 400。 |
| `fetchStandaloneModel(force)` | 全局单独型号查询入口；后端成功解析到一次型号后写入运行期缓存，后续即使前端带 `force=1` 也直接返回缓存，不再自动重复发送 `AT+CGMM`；只有缓存为空时才发送 `AT+CGMM`，强制刷新请求可短暂重试，并返回是否仍处于 pending。 |
| `parseModelAT(raw)` | 解析全局单独型号查询返回；支持 `AT+CGMM` 的纯型号行和 `+CGMM: 型号` / `+CGMM: "型号"` 这类带前缀返回，避免已收到型号但前端仍显示 `-`。 |
| `parseNetworkConfigStatusAT(raw)` | 解析网络设置页状态 AT 返回，包含 IP 透传、DNS 代理、USB 网络、DMZ 和 LANIP；USB 协议匹配大小写不敏感，未知编号保持“未知”不用空值覆盖。 |
| `parseSystemStatusAT(raw)` | 解析系统设置页状态 AT 返回（`+CGSN` 当前 IMEI）。 |
| `handleATData()` | AT 命令页接口：`manual_at` 透传 `command` 参数的任意 AT（为空时默认 `ATI`），`reset_at` 执行 `AT&F`，其余 action 返回 400。 |
| `handleNetworkConfigData()` | 网络设置页接口：`status`（IP 透传、USB 协议、DNS 代理、DMZ、LANIP 状态，开机保护期未就绪时带 `pending` 字段）、`ip_passthrough`、`dns_proxy`、`usbnet`、`dmz`、`lanip`；非法参数（未知模式、非法协议族、某段超过 255 的 IP 等）一律返回 400 和对应错误文案，其中 `dmz`/`lanip` 的 IP 参数经 `cleanIP()` 逐段校验（四段格式且每段 ≤255，与前端 `validIPv4()` 对齐）；禁用 IP 透传时先返回重启通知，再后台延迟分开发送 `MPDN_RULE`、`QMAPWAC`、`CFUN`，避免第一条命令重启网口导致前端收不到通知；后台执行完成后广播 `ip_passthrough_result` 事件（携带整体成功/失败），失败时前端取消重启倒计时并提示。 |
| `handleFirewallData()` | 防火墙页接口：`status`（读取规则配置文件并返回 `rules`（每条 `{port, action}`）、`ruleCount`（放行规则按 1 条、阻止规则按 4 条计）、`jumpInstalled`（INPUT 是否挂载指向 `SADMIN_FW` 的跳转，真实模式从链统计判断）与 `chains`（设备全部 iptables 链统计，非 mock 模式经 `firewallDumpChains()` 解析 `iptables -vnL -x --line-numbers` 输出））、`save`（参数 `block_ports`/`accept_ports`，均逗号分隔，两者均空即清空；端口校验 1-65535、去重、上限 64，同一端口同时出现在两种动作中返回 400 端口冲突；真实模式先应用 iptables 规则（放行规则整体在阻止规则之前）再原子写入配置文件，保存即时生效）；非法端口、超过上限、端口冲突或未知 action 返回 400；mock 模式不执行真实 iptables，合成与真实结构一致的链统计。 |
| `handleSystemData()` | 系统设置页接口：`status`（`+CGSN` 当前 IMEI，开机保护期未就绪时带 `pending` 字段）、`set_imei`（校验 15 位后写入新 IMEI 并重启）、`reboot`。 |
| `handleSMSData()` | 短信列表、短信索引元信息、删除、发送前 SIM 状态检查等接口；自动轮询使用 `list_meta` 只返回索引/时间/分片元信息，不返回短信正文。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/sms_number.go`

短信发送号码规范化逻辑。

| 函数 | 功能 |
|---|---|
| `normalizeSMSNumber(number, cimiRaw)` | 清理短信收件人号码；已有 `+` 或 `00` 国际前缀时保留为国际号码；位数不超过 6 位的客服/应急短号（10001、95588、110 等）原样发送不补前缀；其余普通本地号码通过 `AT+CIMI` 返回的 IMSI 前三位 MCC 查询呼叫码，并自动补成 `+呼叫码号码`。 |
| `parseCIMIResponseIMSI(raw)` | 从 `AT+CIMI` 返回中提取 IMSI 数字串。 |
| `mccToCallingCode(mcc)` | 按内置 MCC 到国家/地区呼叫码映射返回呼叫码；未知 MCC 返回空值，避免错误补号后发送。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/sms_pdu.go`

PDU 模式短信编解码逻辑。

| 函数 | 功能 |
|---|---|
| `buildSMSSubmitPDUs(number, message, ref)` | 生成 UCS2 SMS-SUBMIT PDU 分段，返回完整 PDU 和 `AT+CMGS` 需要的 TPDU 长度。分段按 UTF-16 码元计算（单条 ≤70 码元、多条每段 ≤67 码元并避免拆散代理对），保证含 emoji/增补字符的正文不超出 140 字节用户数据上限。 |
| `parseSMSDeliverPDU(pdu)` | 解析 SMS-DELIVER 原始 PDU，返回短信中心、发送方、时间、正文和长短信拼接信息。 |
| `decodePDUUserData(firstOctet, dcs, udl, userData)` | 按 DCS 判断 GSM 7-bit、UCS2 或 8-bit，并解析 UDHI/UDH。 |
| `decodeGSM7(data, septets, skipSeptets)` | 解码 GSM 7-bit 短信正文，支持扩展转义表。 |
| `encodeUCS2(value)` | 将短信正文编码为 UTF-16BE/UCS2 十六进制字符串。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/at_cache.go`

AT 后台发送与缓存管理。页面通过缓存接口读取数据；缓存缺失、过期、强制刷新或设置类命令会进入后台队列，由后台 worker 串行发送 AT，并把返回写入缓存。系统刚开机时会按 `/proc/uptime` 做 AT 启动保护：开机 35 秒内多数读取类 AT 不阻塞页面等待，只返回后台处理中，避免模块 AT 口未就绪时多条长超时命令串行拖慢后台；`AT+CGMM` 属于静态型号查询，允许绕过开机保护立即执行并由后端短暂重试；成功获取一次型号后写入 `simpleadmin-httpd` 运行期缓存，后续页面刷新、页面切换、登录页和后台主页面品牌显示都直接复用缓存，不再自动加入周期刷新或重复发送该指令。设备信息页把静态信息、SIM/WWAN 实时状态和 LANIP 拆成三组缓存；蜂窝网络页把锁定频段、网络偏好/APN/小区锁配置和实时 `QCAINFO` 拆开缓存；网络设置页 QMAP/QCFG 配置和 LANIP 使用配置缓存；短信列表不会加入后台周期刷新，只由短信页自身固定轮询触发，避免离开短信页后仍持续 `CMGL`。蜂窝网络页频段读取支持按模式或一次性查询 LTE/NSA/SA 当前已锁定频段；页面在获取型号并渲染当前可用频段后会立即强制发送查询，普通刷新仍支持 `wait=0` 非阻塞模式，首次查询未就绪时先返回 pending，前端后台重试，不再影响当前网络状态显示。执行 `AT+CFUN=1,1` 等模块重启动作后会重新套用同一段保护期：重启后读取类命令在模块 AT 口恢复前返回后台处理中，缓存补刷延迟到保护期结束后进行。只读命令刷新成功后通过 WebSocket 异步广播 `at_cache_updated` 事件（写帧带超时），页面据此即时静默刷新。缓存条目记录最近请求时间：周期刷新只覆盖页面公共命令或最近 10 分钟内请求过的命令，非页面条目超过 15 分钟无请求时从缓存驱逐，手动一次性 AT 命令不会成为永久后台刷新任务；命令执行失败不刷新缓存时效，下次读取会重新执行；开机保护期内 worker 优先执行已就绪的命令，动作命令（重启/自愈）不会被等待中的读命令卡队。首页 `dashboard_data`、蜂窝网络页 `network_data`（settings）、网络设置页、系统设置页状态接口在开机保护期未就绪时均返回 `pending` 字段，前端自动重试或保持旧值，避免显示全默认值误导用户。

| 类型/函数 | 功能 |
|---|---|
| `atCacheEntry` | 保存单条 AT 命令的缓存内容、错误、更新时间、最近请求时间、运行状态和等待者。 |
| `atCommandCacheManager` | 管理 AT 缓存表、后台队列、周期刷新、mock 模式和开机 AT 启动保护时间；周期刷新会跳过单独型号命令 `AT+CGMM` 和短信列表 `CMGL`，避免型号成功缓存后仍定时重复查询，也避免离开短信页后后台继续刷新大量短信正文；周期刷新同时驱逐超过空闲阈值且不在页面公共命令集合内的条目。 |
| `atCommandCache.Start(mockMode)` | 启动 AT 缓存 worker、周期刷新任务和首次首页预热；真实模块模式下会根据系统 uptime 跳过开机 35 秒内的不稳定读取期。 |
| `Fetch(command, force, waitOverride)` | 读取 AT 缓存，并在需要时触发后台刷新或等待后台结果；处于开机保护期的读取类 AT 会立即返回 pending，不让页面卡住 3 秒以上。 |
| `enqueue(command, force)` | 把 AT 命令加入后台队列，合并同命令并发请求。 |
| `run(command)` | 后台执行 AT 命令、更新缓存并唤醒等待者；执行失败不更新缓存时间戳（下次读取重新执行，不在缓存期内反复返回错误文本）；动作类命令完成后会让读取类缓存失效；只读命令刷新成功后向前端广播 `at_cache_updated` 事件，页面可据此即时静默刷新。 |
| `invalidateReadCache()` | 动作类 AT 完成后清空读取类缓存时间，避免页面继续显示旧状态。 |
| `cachedReadCommandsNeedingRefresh()` | 返回已缓存且需要周期刷新的读取类 AT 命令；周期刷新只刷新已经被请求过的缓存项，非页面命令仅在最近 10 分钟请求窗口内参与刷新，不再每轮塞满全部常用命令。 |
| `enqueueInitialReadCommands()` | 服务启动保护期结束后只预热首页读取命令，避免设备信息、网络、设置、短信等多条长命令在 AT 口未就绪时串行阻塞。 |
| `enqueueCachedReadCommands()` | 动作类 AT 完成后只刷新已存在的读取类缓存项，不再强制刷新所有常用命令。 |
| `waitUntilReadyFor()` / `startupDelayRemaining()` | 对读取类 AT 应用开机保护延迟；动作类 AT 和 mock 模式不受影响。 |
| `atCacheStartupDelay()` / `systemUptimeDuration()` | 读取 `/proc/uptime` 计算系统开机后剩余的 AT 保护时间；非 Linux 或无法读取时不启用延迟。 |
| `executeCachedATCommand(command, mockMode)` | 根据 mock 模式选择 mock 返回或真实 AT 执行。 |
| `handleGetATCache()` | `/api/get_atcache` 入口，页面通过 POST 读取 AT 缓存并触发后台刷新，同时保留 GET 兼容。 |
| `requestValue()` / `requestValues()` | 统一读取 POST 表单参数和旧 GET 查询参数。 |
| `boolQuery()` | 解析 `force`、`wait` 等布尔参数。 |
| `atCacheWaitTimeout(command)` | 根据 AT 命令类型计算等待后台结果的超时时间；在 AT 执行超时时间基础上额外预留页面等待时间。 |
| `maxAgeForATCacheCommand(command)` | 根据命令类型返回缓存有效时间；实时信号、温度、QENG/QCAINFO、SIM/WWAN 在线状态使用短缓存；网络偏好、频段锁定、QMAP/QCFG、LANIP 等低频配置使用配置缓存；CGMI/CGSN/QGMR/CIMI/ICCID/CNUM 等静态信息使用长缓存；短信列表只保留短缓存但不参与后台周期刷新。 |
| `isATActionCommand(command)` | 判断命令是否为设置、删除、扫描、重启等需要强制执行的动作类 AT。 |
| `commonATCacheCommands()` | 返回常用读取类 AT 命令集合；当前仅用于选择首页首次预热命令和保留统一命令清单，不包含 `AT+CGMM` 和短信列表，避免启动后自动重复查询型号或扫描短信正文。 |
| `smsListATCommand()` | 返回短信列表读取所需的 AT 初始化和 PDU 模式 `CMGL=4` 组合命令。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/smd_reader.go`

`/dev/smd*` 常驻读取器实现（仅 Linux）。`smdATReader` 维护进程内唯一的读取子进程与字节缓冲，提供排空（`drain`）、快照（`snapshot`）与按终止符读取；`readUntilTokensWithEcho` 在写入后按命令回显行关联响应，丢弃回显之前到达的上一条命令残留数据，回显迟迟不出现时（端口关闭回显）在短暂宽限期后回退旧行为，避免命令间响应串扰。`findEchoMarker` 等回显定位工具位于跨平台的 `at_command_util.go`。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/at_cache_policy.go`

AT 命令分类与缓存分级策略的唯一定义点：动作命令不缓存；静态身份（型号/IMEI/IMSI/ICCID 等）长缓存；网络设置页状态、LANIP、频段偏好中缓存；模式偏好组合查询半静态缓存；短信列表 5 秒；实时信号与其余读取命令短缓存。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/metrics_history.go`

信号/资源/流量历史采样环形缓冲。每次 `/api/dashboard_data` 解析成功后记录一条采样（CPU、RAM、总信号百分比、最佳 RSRP、NR 流量累计与平均速率），采样最小间隔 60 秒、容量 1440 点（24 小时），仅内存保存，重启清零。`/api/history_data` 返回采样数组供首页趋势图渲染。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/watchdog.go`

断网自愈看门狗。配置持久化为 `runtimeTTLValueFile` 同目录 `watchdog.conf`（enabled/failThreshold/cooldownMinutes，缺省关闭、5 次、30 分钟）。后台每 3 分钟检查一次，启动后先睡一个周期再探测，避免启动即探测时 WAN 尚未就绪而累计失败：启用时用联网探测判断在线，连续失败达到阈值且距上次自愈超过冷却期时执行 `AT+CFUN=1,1` 重启模块；恢复在线清零计数；从禁用切换为启用时清零连续失败计数，不延续禁用前累积的失败。`/api/get_watchdog` 与 `/api/set_watchdog` 提供状态读取与配置保存。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/scheduler.go`

每日定时重启调度器。配置持久化为 `scheduler.conf`（rebootEnabled/rebootTime，缺省关闭、04:00，时刻必须匹配 `HH:MM`）。后台每 30 秒检查，到达配置时刻且当天未执行时执行 `AT+CFUN=1,1` 并记录日期，次日可再次触发；执行日期与上次检查时刻持久化于 `scheduler.state`，进程重启后当日不重复触发；当天已过配置时刻但未执行、且状态文件中的上次检查时刻早于配置时刻（证明进程此前在运行、确实错过了窗口）时补跑一次，没有历史检查记录则不补跑，避免首次启动已过时刻时误触发。`/api/get_scheduler` 与 `/api/set_scheduler` 提供状态读取与配置保存。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/timesync.go`

时间同步（NTP）。配置持久化为 `runtimeTTLValueFile` 同目录 `timesync.conf`（enabled/intervalMinutes/server，缺省关闭、60 分钟、阿里云 `ntp.aliyun.com`）。同步原理为轮询：从**单一** NTP 源发送 SNTP 查询，按应答的接收/发送时间戳与本地收发时刻计算时钟偏差（`parseNTPResponse`），随后直接步进修改系统时间（`native_timesync.go` 的 `settimeofday`，Linux 实现，需 root）；偏差幅度超过约一年视为异常应答（Kiss-o'-Death 或错误报文），拒绝写时钟。轮询器按配置启停（与看门狗同一模式）：启用后先等待 30 秒再首次同步（短等 WAN 就绪；设备无电池时钟，不宜等满间隔），成功后按配置间隔（每轮从配置读取、运行中可改），失败后按 5 分钟与配置间隔的较小值快速重试（避免开机时钟错误时因一次失败长时间停留在错误时间，定时重启依赖本地时间）；禁用立即停止，不驻留空转循环。手动同步经 `/api/timesync_now` 立即执行一次，与轮询互斥（同一时刻最多一次同步）。每次同步结果（时间、成败、偏差毫秒、服务器）记入进程内状态，随 `/api/get_timesync` 返回；`/api/set_timesync` 保存配置并同步轮询器启停，提交空服务器重置回缺省源。mock 模式不访问网络、不修改时钟，模拟一次小幅校正。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/system_monitor.go`

系统监控页数据源（`/api/system_monitor`），展示效果类似 htop。CPU/内存占用复用 `system_metrics.go` 的 `/proc/stat`、`/proc/meminfo` 采样，另读 `/proc/loadavg` 与 `/proc/uptime`。进程 Top 20 按两次采样 `/proc/[pid]/stat` 的时钟节拍（utime+stime，USER_HZ=100）增量计算每进程 CPU 百分比（htop 默认的单核百分比口径，多核可超 100%），内存按 stat 的 rss 页数换算并除以总内存得百分比；进程名取首个 `(` 与末个 `)` 之间内容（容忍名字含空格/括号），用户由 `/proc/[pid]/status` 的真实 UID 经 `/etc/passwd` 映射（未知回退数字）。按 `sort=cpu|mem` 降序取前 20（次键为另一指标降序、再按 PID 升序），并返回采样到的进程总数；采样间隔约 300 毫秒。mock 模式返回固定的 22 个模拟进程（截断为 20）用于本地预览。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/sms_webhook.go`

新短信到达通知。配置持久化为 `sms_webhook.conf`（enabled/url，URL 必须为 http/https）。短信列表每次解析后按存储索引高水位判断新条目（进程重启后首个**非空**扫描只初始化水位，不重复通知；收件箱确为空的有效扫描（应答含 OK 且无终结错误）以水位 -1 建立基线，保证空收件箱设备到达的第一条短信也能通知，不被基线消耗；开机保护期 pending/错误文本解析出的空列表为无效扫描，不消耗首扫，避免保护期假空导致存量消息全量重推；当前最大索引低于高水位时判定存储被清空/索引回绕，把高水位重置为当前最大值-1，该消息仍通知一次，避免索引复用后永久漏报新消息），对新消息异步 POST `{"sender","date","text","index"}` JSON 到配置 URL（10 秒超时，投递失败按 2 秒、8 秒间隔重试 2 次，共尝试 3 次，全部失败才记日志放弃）。新到达检测有两条触发路径：前端短信页轮询 `/api/sms_data` 时顺带执行；**服务端后台轮询器**每 30 秒主动拉取一次短信列表（经 `fetchPageAT(wait)` 复用 AT 缓存，缓存未过期直接命中、不额外占用串行通道），无人打开网页时新短信同样触发转发。轮询器**按配置启停**：保存配置时启用且 URL 非空才启动循环，关闭开关立即停止（等待间隔期间也能即时退出），禁用状态不驻留任何循环、零 AT 流量；服务启动时同样按已保存配置同步。`/api/get_sms_webhook` 与 `/api/set_sms_webhook` 提供状态读取与配置保存（默认仅经 `/api/ws` 网关分发）。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/version.go`

构建注入版本号（`-ldflags "-X main.appVersion=..."`，Makefile/CI 默认取 `git describe`）。静态 HTML 中的 `?v=__SA_VERSION__` 占位符由静态文件服务与登录页在响应时替换为当前版本，发版不再需要手工修改前端缓存参数。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/console_page.go`

Web 控制台内嵌终端页面 HTML/JS 常量（仅 Linux），与 `native_console.go` 的 WebSocket/PTY 逻辑分离。


### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/native_serial.go`

Linux 下 AT 串口、文件锁和直接 `/dev/smd11` 读写实现（`/dev/smd*` 常驻读取器位于 `smd_reader.go`）；首次真实 AT 请求会为 `/dev/smd11` 启动独立 reader 子进程常驻读取，行为接近 `dd if=/dev/smd11 ... &`；每次 AT 再通过 `/bin/sh -c 'cat > "$1"'` 接收 stdin 并重定向写入 `/dev/smd11`，不使用 `printf` 拼接命令，避免部分模块在 Go 单句柄或每次重开读写句柄时写入返回 `device or resource busy`；运行时不创建 `socat`、`/dev/ttyIN`、`/dev/ttyOUT` 或读写桥接。页面停留和轮询期间保持读取端，不反复清理或重建 `/dev/smd11`。读取响应时按命令回显行关联（`readATSessionUntilTokens` 携带回显标记），写入前排空残留、成功后短暂排空尾部，避免相邻命令响应互相串扰。

| 类型/函数 | 功能 |
|---|---|
| `openNativeTTY()` | 直接打开 AT 设备，默认 `/dev/smd11`。 |
| `shouldSkipTermiosForATDevice()` | 判断设备是否跳过 termios 初始化。 |
| `configureTTYRaw()` / `configureTTYRawWithEcho()` | 按文件描述符设置 TTY raw 模式。 |
| `ioctl()` | 封装 ioctl 调用。 |
| `waitReadableFD()` | 使用 select 等待 fd 可读。 |
| `setFDSetBit()` | 设置 `syscall.FdSet` 位。 |
| `drainTTY()` | 限时读取并丢弃旧数据。 |
| `readTTYUntilIdle()` | 读取直到空闲或超时。 |
| `isTemporaryTTYReadError()` | 判断 TTY 临时读取错误。 |
| `killProcessByCmdline()` | 保留的按命令行关键字结束进程工具。 |
| `lockGlobalATFile()` | 获取跨进程 AT 文件锁；非阻塞轮询抢锁，30 秒获取上限，超时返回 `AT lock held by another process` 错误，外部进程长时间持锁时 AT 功能快速失败而不是永久挂起。 |
| `shouldRecoverBusyATDevice()` / `recoverBusyATDevices()` | 直接 `/dev/smd11` 模式下不杀读进程，仅保留兼容重试入口。 |
| `atCommandSession` | 保存一次 AT 会话；普通 TTY 使用读写文件对象，SMD 设备引用常驻读取器。 |
| `openATCommandSession()` | 打开 AT 会话；`/dev/smd11` 复用进程内常驻读取器。 |
| `closeATSession()` | 关闭普通 TTY 会话；`/dev/smd11` 常驻读取器不会随页面轮询关闭。 |
| `writeATSessionString()` | 写入 AT 会话 payload；`/dev/smd11` 每次短生命周期打开写入端。 |
| `drainATSession()` | 清理 AT 会话旧数据；`/dev/smd11` 清空常驻读取缓冲。 |
| `readATSessionUntilTokens()` | 读取 AT 会话直到 OK、ERROR 或其它终止 token；`/dev/smd11` 从常驻读取缓冲等待返回。 |
| `smdATReader` / `getSMDATReader()` / `startSMDATReader()` | 管理 `/dev/smd11` 进程内常驻读取端和返回缓冲。 |
| `openSMDReaderFile()` / `writePayload()` | reader 子进程按阻塞读方式打开 `/dev/smd11`；写入端通过 shell 重定向接收 stdin payload，不使用 `printf` 拼接命令；写入端在独立进程组中运行，写超时先杀整个进程组并回收后再返回，不残留继续持有设备的孤儿进程。 |
| `readTTYUntilTokens()` | 普通 TTY 底层读取循环；EOF 且暂无数据时睡眠 20ms 继续等待，不忙等。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/native_serial_stub.go`

非 Linux 构建的串口占位实现。用于 Windows 本地测试编译，硬件 AT 功能返回“不支持”错误或空操作。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/native_ttl.go`

Linux 下 TTL 规则管理。

| 类型/函数 | 功能 |
|---|---|
| `ttlRuleSpec` | 描述 TTL iptables 规则目标和参数。 |
| `runTTLCommand()` | TTL 子命令入口。 |
| `printLines()` | 输出日志行。 |
| `applySavedTTLAtStartup()` | 服务启动时应用保存的 TTL；状态文件不存在时初始化为 0，读取失败时保留现有文件不清零配置。 |
| `setNativeTTL()` | 设置 TTL 值并持久化；仅在规则应用成功后写入状态文件，应用失败不持久化。 |
| `applyNativeTTL()` | 应用 TTL iptables 规则。 |
| `removeNativeTTLRules()` | 删除 Go 管理的 TTL 规则。 |
| `managedTTLDeleteArgs()` | 从 iptables 规则行生成删除参数。 |
| `hasTokenPair()` | 检查 token 键值对。 |
| `hasToken()` | 检查 token 是否存在。 |
| `readTTLValue()` | 读取 TTL 状态文件。 |
| `writeTTLValue()` | 写入 TTL 状态文件。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/native_ttl_stub.go`

非 Linux 构建的 TTL 占位实现。用于 Windows 本地测试。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/native_firewall.go`

Linux 下防火墙阻止/放行规则管理。Go 服务自管专用 `SADMIN_FW` iptables 链（仅 IPv4、TCP）；本页是 RGMII Toolkit 原 `simplefirewall` 脚本端口阻止能力的 Web 化管理（扩展支持放行规则与全部链统计），不修改原脚本，如果设备同时启用了原 `simplefirewall.service`，两者规则并存互不影响，建议二选一。

| 类型/函数 | 功能 |
|---|---|
| `firewallIPTablesCommands(rules)` | 生成规则命令序列：先 `-F` 清空 `SADMIN_FW` 链；放行规则整体排在阻止规则之前（每个放行端口 1 条不限接口的 ACCEPT，确保放行优先命中）；每个阻止端口先追加在 `bridge0`、`eth0`、`tailscale0` 接口上放行的 3 条 ACCEPT，再追加对其余接口阻止的 1 条 DROP（同端口 ACCEPT 必须先于 DROP）；空规则列表即清空规则。 |
| `runFirewallCommand(args)` | 执行单条 iptables 命令，10 秒超时，失败时返回带命令上下文的错误。 |
| `ensureFirewallChain()` | 确保 `SADMIN_FW` 链存在（`-N`，已存在时忽略），且已通过 `-I INPUT 1` 挂到 INPUT 链首位（先 `-C` 检查，避免重复挂载）。 |
| `applyFirewallRules(rules)` | 确保链就绪后按命令序列重建 `SADMIN_FW` 链规则；空规则列表即清空（仅保留跳转）。 |
| `firewallDumpChains()` | 执行 `iptables -vnL -x --line-numbers`（10 秒超时）并解析设备全部链的计数与规则，供 `status` 动作返回所有防火墙规则统计。 |
| `applySavedFirewallAtStartup()` | 服务启动时从配置文件恢复已保存的阻止/放行规则，实现重启后自动恢复；失败仅告警不阻断启动。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/native_firewall_stub.go`

非 Linux 构建的防火墙占位实现。规则应用与全部链统计（`firewallDumpChains`）返回“仅 Linux 支持”错误、启动恢复为空操作，用于 Windows 本地测试。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/native_system_power.go` / `native_system_power_stub.go`

设备整机重启/关机的原生实现与非 Linux 平台桩。`nativeDevicePower(action)` 只接受 `reboot`/`poweroff` 两种动作（其余在触碰系统前即被拒绝），经 `exec.LookPath` 定位系统命令后**异步启动、不等待完成**：systemd 停机流程需数秒（先停服务再断电），进程被系统停止前 HTTP 响应已发出；子进程由后台协程回收。设备上 `reboot`/`poweroff` 均为 `systemctl` 的符号链接。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/native_console.go`

Linux 下原生 Web 控制台实现。控制台页面通过内置 ANSI SGR 渲染器显示 shell 输出颜色，支持基础 16 色、256 色、truecolor、加粗、变暗和下划线；OSC、非颜色 CSI 和字符集切换序列会被过滤或忽略，避免颜色控制码原样显示到页面。PTY shell 启动时设置 `TERM=xterm-256color` 和 `COLORTERM=truecolor`，让支持颜色的命令可以输出彩色内容。

| 类型/函数 | 功能 |
|---|---|
| `nativeWSConn` | WebSocket 连接封装。 |
| `winsize` | PTY 窗口尺寸结构。 |
| `handleNativeConsole()` | 返回控制台页面；控制台页面使用纵向 flex 布局，隐藏 body 和终端区域的可见滚动条，避免嵌入 iframe 后出现双滚动条；前端解析 ANSI SGR 颜色码并渲染为彩色文本。 |
| `handleNativeConsoleWebSocket()` | WebSocket 控制台入口，升级前会校验 `Origin` 必须与当前访问 Host 和协议一致；会话鉴权由统一 `sessionAuth` 中间件完成，WebSocket 握手后直接启动 PTY shell，无终端级账号密码。 |
| `consoleWebSocketOriginAllowed()` | 控制台 WebSocket Origin 校验，阻止跨站页面复用已登录凭据打开控制台。 |
| `normalizeOriginHost()` | 标准化 Origin/Host，兼容默认 `:80`、`:443` 端口。 |
| `websocketAcceptKey()` | 生成 WebSocket 握手 accept key。 |
| `readClientFrame()` | 读取客户端 WebSocket frame。 |
| `writeBinary()` | 写二进制 WebSocket frame。 |
| `writeClose()` | 写关闭 frame。 |
| `writeFrame()` | 写 WebSocket frame。 |
| `nativeConsoleShell` | 控制台 shell 会话。 |
| `startNativeConsoleShell()` | 启动 PTY shell，并设置 `TERM=xterm-256color`、`COLORTERM=truecolor` 以支持彩色终端输出。 |
| `close()` | 关闭 shell 会话。 |
| `nativeConsoleShellPath()` | 选择可用 shell 路径。 |
| `openConsolePTY()` | 打开控制台 PTY。 |
| `setPTYWindowSize()` | 设置 PTY 窗口大小。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/api_websocket.go`

通用业务 WebSocket API 网关。页面中的业务请求统一发送到 `/api/ws`，消息体内携带原始业务路径、方法、表单参数和请求 ID；后端复用 `nativeAPIHandlers()` 中的原有 handler 执行业务逻辑，再把状态码、响应头和响应体封装成 WebSocket JSON 返回。

| 类型/函数 | 功能 |
|---|---|
| `apiWebSocketRequest` | 浏览器发往 `/api/ws` 的请求结构，包含 `id/method/path/headers/body`。 |
| `apiWebSocketResponse` | `/api/ws` 返回结构，包含 `id/status/headers/body/error`。 |
| `handleAPIWebSocket()` | 通用业务 WebSocket 入口，执行同源 Origin 校验、握手和请求循环；读循环带 5 分钟空闲超时，半开连接超时后关闭，避免读协程永久泄漏；支持分片帧重组，单条消息（含重组后）上限 1MB，超限或协议错误直接关闭连接。 |
| `dispatchAPIWebSocketRequest()` | 将 WebSocket 消息转换为内部 HTTP request，并调用对应业务 handler；非法 method 或请求构造失败返回 400 帧（不使用会对非法 method panic 的请求构造方式），业务 handler panic 兜底为 500 帧，单次请求异常不影响服务进程。 |
| `dispatchAPIWebSocketRequestAsync()` | 在独立协程中分发单次请求，同一连接的请求并发执行，慢请求（如扫网）不再阻塞后续快请求；响应帧携带请求 `id`，前端按 `id` 匹配，乱序到达无影响。 |
| `apiWebSocketOriginAllowed()` | 校验业务 WebSocket 只能从同源页面打开。 |
| `apiWebSocketAcceptKey()` | 生成 WebSocket 握手 accept key。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/mock_at_payload.go`

Windows/mock 模式首页 AT 测试数据管理。保存整段 AT、QCAINFO、QENG 手动输入，生成 mock AT 返回，并提供控制台解析 JSON 输出。

| 函数 | 功能 |
|---|---|
| `setMockATPayload(kind, payload)` | 保存整段首页 AT、QCAINFO 或 QENG 手动测试数据，并让相关 AT 缓存失效。 |
| `clearMockATPayload(kind)` | 清除指定类型或全部手动测试数据，恢复默认 mock 数据。 |
| `getMockATPayload(kind)` / `mockATPayloadStatusMap()` | 读取当前 mock 输入和状态摘要，供控制台、网页 Console 和 `/api/mock_at` 状态接口复用。 |
| `normalizeMockATPayloadKind()` / `normalizeMockATPayloadText()` | 规范化 mock 输入类型和文本内容。 |
| `currentMockDashboardATResponse()` | 根据当前手动输入组合首页 AT 返回；整段 AT 优先，未提供时按 QCAINFO/QENG 覆盖默认片段。 |
| `currentMockDashboardParseJSON()` | 使用首页结构化解析逻辑把当前 mock 首页 AT 转为 JSON，供 Windows 命令行 `parse` 使用。 |
| `defaultMockDashboardATResponse()` / `defaultMockQCAInfoPayload()` / `defaultMockQENGPayload()` | 提供 Windows/mock 模式默认首页、QCAINFO 和 QENG 测试数据；默认载荷为示例信号数据（中国电信 46011，5G SA n1 FDD 驻留），与目标模块移远 RM520N-CN 的频段能力一致。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/mock_at_http.go`

Windows/mock 模式网页端 mock AT 数据接口。该文件提供 `/api/mock_at` 的 handler，真实模块运行时直接拒绝，只有 `--mock` 模式允许浏览器开发者工具通过 `SimpleAdmin.MockAT.*` 写入或查看测试数据。

| 函数 | 功能 |
|---|---|
| `handleMockATPayload()` | `/api/mock_at` 入口；支持 `set/save` 写入整段首页 AT、QCAINFO、QENG，`clear/reset` 清除测试数据，`parse` 返回当前首页解析结果，`show/status` 返回状态，`help` 返回浏览器 Console 可用命令。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/mock_at_console_windows.go`

Windows 本地测试命令行输入。支持 `at`、`qca`、`qeng`、`parse`、`show`、`clear`，方便不用修改 `index.js` 就能测试 AT 解析。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/mock_at_console_stub.go`

非 Windows 构建的 mock AT 控制台占位实现。模块端不读取控制台输入。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/native_console_stub.go`

非 Linux 构建的 Web 控制台占位实现。用于 Windows 本地测试。

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/main_test.go`

Go 单元测试文件。

| 测试 | 检查内容 |
|---|---|
| `TestHTMLDoesNotReferenceLegacyCGIBin` | HTML 不直接引用旧 CGI 路径。 |
| `TestAllHTMLAPIPathsHaveNativeHandlers` | 前端使用的 API 路径都有后端 handler。 |
| `TestExternalTtydRuntimeArtifactsAreNotPackaged` | 包内不包含外部 ttyd 运行物。 |
| `TestConsoleWebSocketOriginAllowed` | 控制台 WebSocket 只允许同源 Origin。 |
| `TestWebsocketAcceptKey` | WebSocket 握手 key 计算。 |
| `TestManagedTTLDeleteArgs` | TTL 规则删除参数生成。 |
| `TestManagedTTLDeleteArgsIgnoresUnrelatedRules` | TTL 删除逻辑忽略非本项目规则。 |
| `TestVuePagesHaveValidMountScripts` | Vue 页面挂载脚本存在。 |
| `TestWebFrontendUsesVue3AndNoAlpine` | 前端使用 Vue 3，不使用 Alpine。 |
| `TestCollectATDeviceCandidatesSupportsConfigAndDefaults` | AT 设备候选收集和过滤。 |
| `TestDefaultATDeviceCandidateOrderMatchesDirectSMDDesign` | 默认 AT 候选顺序保持直接 `/dev/smd11` 设计。 |
| `TestMockDashboardPayloadOverrideParsesManualInput` / `TestMockQENGPayloadReplacesDefaultDashboardLines` | Windows/mock 手动输入首页 AT、QCAINFO、QENG 后能影响结构化解析结果。 |
| `TestTargetModuleIsRM520NCN` | mock 原型与 `AT+CGMM` 返回锚定为目标模块移远 RM520N-CN。 |
| `TestMockDashboardDefaultPayloadMatchesRealCPE` / `TestMockATResponseIdentityCommands` | 默认首页 mock 载荷与身份类命令按真实设备(RG520N-CN/46011)解析。 |
| `TestMockATResponseCoversDeviceInfoPage` / `TestMockATResponseCoversNetworkSettingsPage` / `TestMockATResponseCoversNetworkConfigStatusPage` | mock AT 覆盖设备信息、蜂窝网络、网络设置三页结构化解析。 |
| `TestPageATCommandsNetworkConfigAndSystemStatusCommands` / `TestSystemStatusCommandAndParserReadsIMEI` / `TestParseNetworkConfigStatusMPDNRuleEnabled` | 网络设置/系统设置状态 AT 命令组固定不变；系统状态解析读取 `+CGSN` IMEI；网络设置状态按 `MPDN_rule` 判定 IP 透传开关。 |
| `TestMockQNWPREFCFGReturnsRealBandLists` / `TestMockSMSListContainsChineseUCS2Message` | mock 频段列表取真实值；短信列表含中文 UCS2 短信。 |
| `TestSMSListCommandUsesPDUMode` / `TestSMSListMetaOnlyRemovesTextPayload` | 短信列表使用 PDU 模式读取，元信息接口不返回正文大字段。 |
| `TestParseSMSListPDUModeUCS2KeepsNewlines` / `TestParseSMSListPDUModeAddsTextLines` | PDU 模式短信正文换行和 `textLines` 解析。 |
| `TestEnsureAuthFileResetsEmptyFile` / `TestEnsureAuthFileKeepsExistingValidFile` | 认证文件为空时重置，已有有效认证时保留。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/page_split_endpoints_test.go`

AT 命令、网络设置、系统设置三个页面端点（`/api/at_data`、`/api/network_config_data`、`/api/system_data`）的端到端测试。

| 测试 | 检查内容 |
|---|---|
| `TestSplitEndpointsRejectUnsupportedActions` | 三个端点对未知 `action` 一律返回 400。 |
| `TestSystemDataStatusReturnsMockIMEI` | `/api/system_data` 状态动作独立返回 IMEI，mock 模式不带 `pending`。 |
| `TestNetworkConfigDataStatusHasNoIMEI` | `/api/network_config_data` 状态响应不包含 IMEI 字段，USB 协议按 mock 返回 `RMNET`。 |
| `TestATDataResetAtExecutesFactoryReset` | `/api/at_data` 的 `reset_at` 动作返回 200 且 `ok` 为 true，`response` 回显包含 `AT&F`。 |
| `TestATDataManualAtFallsBackToATI` | `manual_at` 未携带 `command` 时回退执行 `ATI`，响应包含 mock 机型信息。 |
| `TestSplitEndpointsRejectInvalidParams` | 表驱动 6 条非法参数（`set_imei` 不足 15 位、DMZ IP 段超过 255、未知 `usbnet` 模式、非法 `dns_proxy` 协议族、未知 `ip_passthrough` 模式、`lanip` IP 段超过 255）一律以 400 和对应错误文案拒绝。 |
| `TestNetworkConfigDisableIPPassthroughReturnsRebootNotice` | 禁用 IP 透传返回 `ok:true`、`reboot:true`、`rebootCountdownSeconds:40`。 |
| `TestNetworkConfigDNSV4EnableSucceedsInMock` | mock 模式下启用 DNS V4 代理的写命令返回 `ok:true`。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/page_firewall_test.go`

防火墙页端点、规则模型与链统计解析测试。

| 测试 | 检查内容 |
|---|---|
| `TestParseFirewallRuleLines` | 规则文本逐行解析：新格式 `<action> <port>` 与旧格式纯端口行（视为 block），容错跳过空行、非法动作、非法端口、多余字段与缺端口行，同端口同动作去重保序，空输入合法。 |
| `TestValidateFirewallRules` | 规则校验：同端口同动作去重保序，同一端口同时阻止和放行报「端口冲突」错误，拒绝非法端口（0、70000、非数字、空）与非法动作，超过 64 条上限报错，空列表合法。 |
| `TestFirewallIPTablesCommandsAcceptBeforeBlock` | iptables 命令生成：首条为清空链，放行命令整体位于阻止命令之前，每个阻止端口的 3 条接口 ACCEPT（bridge0/eth0/tailscale0）均先于同端口的 1 条 DROP。 |
| `TestFirewallRulesFileRoundTrip` | 配置文件写入/读取往返一致（新格式 `block 80`/`accept 8080`），旧格式纯端口行按阻止读取且非法行容错跳过，文件不存在返回空列表。 |
| `TestParseIPTablesChainDump` | `-vnL` 输出样本解析：链头 policy/references 双形态、跳过列头行、K/M/G 计数换算、规则行前 10 列且其余拼接为 Extra。 |
| `TestMockModeFirewallSaveAndStatusOverWS` | mock 模式经 `/api/ws` 网关保存阻止/放行规则并读取状态：`ruleCount` 按放行×1、阻止×4 计数，`jumpInstalled` 为真，`chains` 含 `SADMIN_FW` 链且为 9 条规则条目（1 放行 + 2 阻止×4）；空参数清空后归零。 |
| `TestFirewallSaveRejectsConflictingPortsOverWS` | `block_ports` 与 `accept_ports` 同时包含同一端口时保存返回 400 且 `ok:false`，错误文案含「端口冲突」。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/api_websocket_fixes_test.go`

`/api/ws` 网关健壮性测试。

| 测试 | 检查内容 |
|---|---|
| `TestAPIWebSocketInvalidMethodReturns400WithoutCrash` | 非法 method/请求构造失败返回 400 帧，进程不崩溃。 |
| `TestAPIWebSocketValidRequestStill200` | 合法请求仍返回 200 帧。 |
| `TestAPIWebSocketConcurrentDispatchNoHeadOfLineBlocking` | 同连接慢请求不阻塞后续快请求。 |
| `TestAPIWebSocketFragmentedMessageReassembly` | 分片帧重组后正常分发。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/at_cache_fixes_test.go`

AT 缓存生命周期测试。

| 测试 | 检查内容 |
|---|---|
| `TestIdleNonPageCacheEntriesEvicted` | 非页面条目超过空闲阈值被驱逐。 |
| `TestRefreshSelectionRequiresRecentRequestForNonPageCommands` | 非页面命令仅在最近请求窗口内参与周期刷新。 |
| `TestATCacheRunFailureKeepsStaleTimestamp` / `TestATCacheRunSuccessRefreshesTimestamp` | 执行失败不刷新缓存时效，成功才刷新。 |
| `TestActionCommandNotBlockedByWaitingReadCommand` | 开机保护期内动作命令不被等待中的读命令卡队。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/ttl_api_fixes_test.go`

TTL 与短信发送接口测试。

| 测试 | 检查内容 |
|---|---|
| `TestMockModeSetTTLPersistsWithoutRealCommands` | mock 模式设置 TTL 不执行真实命令但持久化配置值。 |
| `TestSetNativeTTLApplyFailureDoesNotPersist` | 规则应用失败不持久化。 |
| `TestApplySavedTTLAtStartupInitializesMissingFile` / `TestApplySavedTTLAtStartupKeepsFileOnReadError` | 启动时文件缺失初始化为 0；读取失败保留现有文件不清零。 |
| `TestHandleSendSMSAcceptsPOSTFormBody` | `/api/send_sms` 支持 POST 表单 body。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/page_pending_fixes_test.go`

页面聚合接口 `pending` 与保存行为测试。

| 测试 | 检查内容 |
|---|---|
| `TestMockModeAggregateEndpointsExposePendingFalse` | mock 模式下聚合接口 `pending` 为 `false`。 |
| `TestMockModeSaveSettingsEmptyAPNSucceeds` / `TestNetworkSettingsCommandsEmptyAPNSkipsCGDCONT` / `TestNetworkSettingsCommandsNonEmptyAPNKeepsCGDCONT` | 空 APN 保存成功且跳过 CGDCONT 重写；非空 APN 正常重写。 |
| `TestLockLTEManualRejectsMismatchedCellCount` | 手动锁小区参数数量与 `cellNum` 不匹配返回 400。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/parse_sms_fixes_test.go`

解析加固测试。

| 测试 | 检查内容 |
|---|---|
| `TestATLinesFilterRunnerErrorText` / `TestParseDeviceInfoIgnoresRunnerErrorText` | AT 运行器错误文本被过滤，不混入设备信息页厂商等字段。 |
| `TestParseSMSListTextBodyKeepsPlusLedLines` | 文本模式正文保留 `+` 开头行。 |
| `TestParseSMSListTextModeJoinsSplitTimestamp` | 文本模式时间戳逗号截断拼回。 |
| `TestParseNetworkConfigStatusUSBNETCaseInsensitive` | USB 协议解析大小写不敏感。 |
| `TestIPPassthroughDisableResponseUnchanged` / `TestIPPassthroughResultBroadcast` | 禁用 IP 透传立即返回结构不变；后台执行完成后广播 `ip_passthrough_result`。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/automation_fixes_test.go`

自动化（Webhook/定时重启/看门狗）测试。

| 测试 | 检查内容 |
|---|---|
| `TestSMSWebhookHighWaterResetsOnWraparound` / `TestSMSWebhookHighWaterKeptOnEmptyList` | 索引回绕时重置高水位；空列表保持高水位。 |
| `TestSMSWebhookDeliveryGivesUpAfterRetries` / `TestSMSWebhookDeliverySucceedsOnRetry` / `TestSMSWebhookDeliveryRetriesOverHTTP` | 投递失败按 2s/8s 重试，重试用尽放弃、中途成功即止。 |
| `TestSchedulerCatchUpAfterMissedWindow` / `TestSchedulerPersistedStateSurvivesRestart` / `TestSchedulerNoCatchUpWithoutPriorCheck` | 错过窗口补跑；执行日期持久化重启当日不重复；无历史检查记录不补跑。 |
| `TestWatchdogReEnableResetsFailureCount` / `TestWatchdogLoopSleepsBeforeFirstCheck` | 禁用→启用清零失败计数；启动后先等一个周期再探测。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/timesync_test.go`

时间同步测试（NTP 查询与时钟写入均注入假实现，不访问网络、不修改真实时钟）。

| 测试 | 检查内容 |
|---|---|
| `TestTimeSyncConfigRoundTrip` / `TestTimeSyncConfigDefaults` / `TestTimeSyncConfigInvalidFallsBack` | 配置读写一致且 0600 权限；缺省关闭、60 分钟、`ntp.aliyun.com`；非法 JSON/间隔/服务器回退缺省。 |
| `TestNormalizeTimeSyncServer` | 服务器地址规范化：去空白、剥 `ntp://` 前缀，带端口/空格/路径/超长回退缺省。 |
| `TestParseNTPResponseOffset` / `TestParseNTPResponseRejectsBadPackets` | 偏差/往返时延计算正确；短报文、非服务端模式、Kiss-o'-Death/高 stratum、零时间戳均拒绝。 |
| `TestRunTimeSyncOnceAppliesOffset` / `TestRunTimeSyncOnceQueryFailure` / `TestRunTimeSyncOnceRejectsHugeOffset` / `TestRunTimeSyncOnceApplyFailure` / `TestRunTimeSyncOnceMockMode` | 成功时按偏差步进写时钟并记录状态；查询失败/超幅偏差/写时钟失败不修改时钟并记录错误；mock 模式不发查询直接成功。 |
| `TestTimeSyncPollerSyncsToConfig` / `TestTimeSyncTickFollowsConfig` | 轮询器按配置启停且幂等；tick 仅启用时执行同步并返回成败。 |
| `TestTimeSyncNextDelay` / `TestTimeSyncPollerFirstDelayAndRetry` | 成功后按配置间隔、失败按较小重试间隔；轮询循环先等 30 秒再首次同步、失败快速重试。 |
| `TestHandleTimeSyncAPIs` | get/set/now 三个 handler：配置持久化、非法间隔保留原值、手动同步成功、禁用后轮询器停止。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/system_monitor_test.go`

系统监控测试。

| 测试 | 检查内容 |
|---|---|
| `TestParseProcStatLine` | `/proc/[pid]/stat` 解析：名字含空格/括号取首 `(` 末 `)` 之间，utime+stime 与 rss 字段位置正确；畸形行拒绝。 |
| `TestParsePasswdUsers` | `/etc/passwd` 解析：同 UID 取首条，非法行忽略。 |
| `TestSortProcessList` | CPU/内存两种排序（主键降序、次键另一指标降序、PID 升序）。 |
| `TestMockSystemMonitorData` / `TestHandleSystemMonitorMock` / `TestHandleSystemMonitorSortParamNormalized` | mock 数据字段齐全、进程数不超 20 且按序；handler 输出 JSON 完整；非法 `sort` 参数回退按 CPU。 |
| `TestReadLoadAverageShape` | 负载输出为三个数值或 `-`。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/sms_webhook_poller_test.go`

短信转发后台轮询器测试。

| 测试 | 检查内容 |
|---|---|
| `TestSMSWebhookPollTickSkipsWhenInactive` | 未启用或 URL 为空时单次轮询跳过（关闭竞态防御），不产生 AT 流量。 |
| `TestSMSWebhookPollTickEstablishesBaselineThenNotifies` | 后台轮询独立建立基线，新索引通知一次，同列表不重复。 |
| `TestSMSWebhookPollTickPendingKeepsBaseline` | 开机保护期 pending 不消耗初始化、不移动水位，结束后照常通知。 |
| `TestSMSWebhookPollTickValidEmptyInboxNotifiesFirstArrival` | 真实空收件箱以水位 -1 建基线，首条到达短信即通知。 |
| `TestSMSWebhookPollerLoopSleepsFirst` | 循环先等待一个间隔再首次轮询。 |
| `TestSyncSMSWebhookPollerFollowsConfig` | 轮询器按配置启停：禁用/URL 空不启动，启用启动且幂等，关闭即停。 |
| `TestSMSWebhookPollerStopsDuringSleep` | 等待间隔期间收到停止信号立即退出。 |

### `go-build/simpleadmin-go/cmd/simpleadmin-httpd/serial_runner_fixes_test.go`

AT 串口与运行器测试。

| 测试 | 检查内容 |
|---|---|
| `TestLockGlobalATFileTimesOutWhileHeld` | 全局 AT 文件锁被持有时超时返回错误。 |
| `TestSMSListGroupedCommandTimeoutGetsExtraBudget` | 短信列表组合命令获得额外读取预算。 |
| `TestSMDWriterCommandRunsInOwnProcessGroup` / `TestSMDWriterTimeoutKillsProcessGroup` | SMD 写入端独立进程组，超时杀整组无孤儿。 |
| `TestHTTPSRedirectLocationKeepsNonDefaultPort` | HTTPS 非 443 端口的重定向保留端口号。 |

## 构建目录 `go-build/`

| 文件 | 作用 |
|---|---|
| `go-build/build_simpleadmin_go.bat` | Windows 下交叉编译 Linux ARMv7 `simpleadmin-httpd.armv7`；当前产物使用 Go 1.26.3、本地 toolchain、`-buildvcs=false` 和可复现构建参数生成，并可输出 `go version -m` 产物信息；批处理窗口输出中文提示，使用 GBK/CP936 编码和 CRLF 换行。 |
| `go-build/README.md` | Go 构建目录说明。 |
| `go-build/simpleadmin-go/` | Go 源码目录。 |

根目录 `Makefile` 提供与 `.bat` 等价的跨平台构建目标：`make arm`（ARMv7 产物）、`make windows`（Windows 测试产物）、`make test`（`-count=1` 单元测试）、`make vet`、`make fmt-check`（gofmt 校验）、`make smoke`（前端模块装配冒烟测试）、`make css`（安装前端依赖并编译 Tailwind CSS v4 样式产物到 `development/simpleadmin/www/css/tailwind.css`）、`make css-check`（重新编译到临时文件并与已提交产物 diff，校验源码与产物一致）、`make clean`。`make css` 安装依赖时优先 `npm ci` 按锁文件精确还原；修改前端样式源码后必须执行 `make css` 并提交产物，设备端只服务已提交的 `tailwind.css`，运行时无构建。构建时把版本号（默认取 `git describe`）通过 `-X main.appVersion` 注入，服务运行期间将静态 HTML 中的 `?v=__SA_VERSION__` 占位符替换为该版本号，覆盖示例 `make arm VERSION=2.97`。`.github/workflows/ci.yml` 在 push/PR 时执行格式检查、静态分析、单元测试、前端样式构建一致性检查（`make css-check`）、前端冒烟测试与双平台交叉编译并上传产物。

## Windows 测试目录 `windows-test/`

| 文件 | 作用 |
|---|---|
| `windows-test/run_windows_test.bat` | 启动 Windows 本地测试服务；批处理窗口输出中文提示，使用 GBK/CP936 编码和 CRLF 换行。 |
| `windows-test/build_windows_test.bat` | 编译 Windows 测试用 `simpleadmin-httpd-windows-amd64.exe`；批处理窗口输出中文提示，使用 GBK/CP936 编码和 CRLF 换行。 |
| `windows-test/frontend_smoke.js` | 前端模块装配冒烟测试：按 `index.html` 加载顺序装配 25 个模块（公共命名空间模块、`vue-app`、`populate-checkbox`、10 个页面工厂与 `simpleadmin-spa`），校验 `window.SimpleAdmin` 命名空间与关键方法完整，以及 `SimpleAdmin.Pages` 的 10 个页面工厂（含小区锁定 `celllock`）注册完整。用法 `node windows-test/frontend_smoke.js`。 |
| `windows-test/bin/simpleadmin-httpd-windows-amd64.exe` | Windows 本地测试二进制。 |
| `windows-test/data/` | Windows 本地测试数据目录，存放认证、证书、TTL 等测试文件。 |
| `windows-test/README.md` | Windows 测试说明。 |

## 主要运行流程

### 模块端服务启动流程

```text
systemd 使用 /lib/systemd/system 成功启动时
  -> /usrdata/simpleadmin/simpleadmin-httpd
  -> 解析启动参数
  -> 初始化页面登录账号密码文件
  -> 读取 AT 候选设备
  -> 应用保存的 TTL
  -> 应用保存的防火墙阻止/放行规则（重启自恢复）
  -> 启动 AT 后台缓存队列
  -> 按配置启停断网自愈看门狗轮询器、每日定时重启调度器、时间同步轮询器与短信转发轮询器
  -> 注册 login.html、/api/login、/api/logout、/api/ws 与 /api/console/ws
  -> /api/* 业务 handler 只作为 WebSocket 内部分发目标
  -> 托管静态网页
  -> 模块端默认只启用 HTTP，管理入口为 http://设备IP/；不启动 HTTPS，不提供 CA 下载或证书安装说明入口
```

### HTTP 管理访问流程

```text
simpleadmin-httpd 启动
  -> systemd 先执行 /usrdata/simpleadmin/prepare_simpleadmin_ports.sh 释放 80 端口
  -> systemd 使用 -http :80 -no-tls -static /usrdata/simpleadmin/www

只读 squashfs 根分区时
  -> 安装时写入 /usrdata/simpleadmin/start_simpleadmin.sh
  -> 先尝试写入 /lib/systemd/system/simpleadmin-httpd.service 并启动服务
  -> 如果 systemd 服务 active，不修改 post_boot，也不写入重复自启
  -> 如果 /lib/systemd/system 不可写或服务无法 active，且 /etc/init.post_boot.sh 可写，才追加 SimpleAdmin 自启块
  -> fallback 模式下开机后 post_boot 延迟 3 秒调用 /usrdata/simpleadmin/start_simpleadmin.sh
  -> start_simpleadmin.sh 先调用 prepare_simpleadmin_ports.sh 释放 80 端口
  -> start_simpleadmin.sh 放行 TCP 80 并执行 nohup /usrdata/simpleadmin/simpleadmin-httpd -http :80 -static /usrdata/simpleadmin/www ...
  -> 日志写入 /tmp/simpleadmin-httpd.log 和 /tmp/simpleadmin-postboot.log
  -> 只监听 HTTP 80 端口
  -> 浏览器访问 http://设备IP/
  -> 显示 login.html 页面登录
  -> 登录成功后写入会话 Cookie
  -> 单页管理界面加载
```

### 开机 AT 保护流程

```text
系统刚开机
  -> simpleadmin-httpd 启动 AT 缓存
  -> 读取 /proc/uptime
  -> uptime < 35 秒时进入保护期
  -> 页面读取类 AT 请求只入队并立即返回 pending
  -> worker 优先执行队列中已就绪的命令，重启/自愈等动作命令不被等待中的读命令阻塞
  -> 读命令等保护期结束后再发送真实 AT
  -> 首次只预热首页数据，其他页面按需触发
  -> 周期刷新只刷新已经被请求过的缓存项
```

目的：模块开机前约 35 秒内 AT 口可能不返回，此时不让 3 秒超时的多条 AT 命令串行堆积，避免打开后台后几分钟都没有数据。

### 普通页面结构化数据流程

```text
前端页面
  -> SimpleAdmin.Api.*Data({ action: ...业务参数... })
  -> WebSocket /api/ws
  -> 消息内 path 与当前页面对应：总览 /api/dashboard_data，设备信息 /api/device_info_data，蜂窝网络 /api/network_data，网络设置 /api/network_config_data，防火墙 /api/firewall_data，AT 命令 /api/at_data，系统设置 /api/system_data，短信 /api/sms_data
  -> Go 后端按 action 映射 AT
  -> 读取或刷新后端 AT 缓存
  -> Go 后端解析 AT 原始返回
  -> 返回页面可直接使用的业务 JSON
  -> JS 只负责显示、表单交互和提示
```

### AT 命令手动发送与兼容 AT 缓存流程

```text
AT 命令页手动发送
  -> SimpleAdmin.Api.atData({ action: 'manual_at', command })
  -> WebSocket /api/ws
  -> 消息内 path=/api/at_data
  -> handleATData()
  -> 后端 AT 队列发送并等待返回
  -> 返回原始 AT 文本，页面追加式显示并写入命令历史

兼容调试读取（页面脚本不再调用）
  -> SimpleAdmin.Api.getAT()
  -> WebSocket /api/ws
  -> 消息内 path=/api/get_atcache
  -> handleGetATCache()
  -> 后端 AT 队列发送并缓存
  -> 返回原始 AT 文本供调试查看
```

### 短信发送流程

```text
sms.js sendSMS()
  -> SimpleAdmin.Api.smsData({ action: 'send', number, message })
  -> WebSocket /api/ws
  -> 消息内 path=/api/sms_data
  -> handleSMSData()
  -> Go 后端执行 AT+CIMI 并取 IMSI 前三位 MCC
  -> mccToCallingCode() 映射国家/地区呼叫码
  -> 非 + 开头号码自动补成 +呼叫码号码
  -> Go 后端生成 UCS2 SMS-SUBMIT PDU 分段
  -> AT+CMGF=0;+CMGS=<TPDU长度>
  -> runSMSTransaction()
  -> sendSMSOnDevice()
  -> 直接 /dev/smd11 AT 会话
  -> 后端判断发送结果并返回 { ok, segments }
```

### 短信读取流程

```text
sms.js requestSMS({ force: true }) / autoRefreshSMS()
  -> 手动刷新或首次进入：SimpleAdmin.Api.smsData({ action: 'list', force: '1' })
  -> 自动轮询：SimpleAdmin.Api.smsData({ action: 'list_meta', force: '1' })
  -> WebSocket /api/ws
  -> 消息内 path=/api/sms_data
  -> 后端以 AT+CMGF=0;+CMGL=4 读取 PDU 模式短信列表
  -> list_meta 只返回 sender / date / indices / concatTotal 等元信息，不返回 text / textLines，避免短信多时 WebSocket 传输和开发者工具渲染大正文导致卡顿
  -> 索引变化并确认稳定后，前端才再请求 action=list 拉取完整短信正文
  -> 后端按 PDU DCS 字段解析 UCS2、GSM 7-bit 或 8-bit 内容
  -> 长短信优先按 PDU UDH 的 concatRef 在全列表范围内合并，兼容同发送方、5 秒内、连续索引的旧合并方式
  -> 返回 messages / serviceCenters
  -> 页面收件箱按单行摘要显示发件人、时间和部分内容
  -> 点击摘要行后弹出详情，短信正文包含 LF、CR、VT、FF、Unicode 行分隔符或文本形式 \n / \r\n 时，后端返回 textLines，前端用文本节点和 <br> 按行显示完整内容
  -> 停留在短信页时前端按固定时间轮询执行 autoRefreshSMS() 强制查询索引元信息
  -> 前端记录当前短信索引集合，索引没有变化时不更新短信内容
  -> 发现索引变化后先缓存新索引元信息，不立即显示
  -> 后续轮询确认索引元信息稳定后，再拉取完整短信并一次性更新收件箱，避免多段短信只到达一部分时先显示半条
  -> 切换到其它页面后停止轮询并清空待确认结果
```

### 禁用 IP 透传重启通知流程

```text
netconfig.js ipPassThroughDisable()
  -> 前端提示“正在禁用 IP 透传，网口会重启”，并立即 startRebootCountdown(40)，先显示“重启中...”界面
  -> /api/network_config_data action=ip_passthrough enabled=0
  -> handleNetworkConfigData() 立即返回 { ok:true, reboot:true, rebooting:true, rebootCountdownSeconds:40 }
  -> 后端延迟 1 秒后在后台执行 AT+QMAP="MPDN_RULE",0
  -> 收到 OK 后延迟 1 秒
  -> 后端发送 AT+QMAPWAC=1
  -> 收到 OK 后延迟 1 秒
  -> 后端发送 AT+CFUN=1,1
  -> 如果第一条 MPDN_RULE 导致网口/WebSocket 断开，前端 catch 分支只保持倒计时，不再显示失败
  -> 后台三步执行完成后广播 ip_passthrough_result 事件，失败时前端取消倒计时并提示
```

### TTL 设置流程

```text
前端 TTL 控件
  -> WebSocket /api/ws
  -> 消息内 path=/api/set_ttl
  -> handleSetTTL()
  -> setNativeTTL()
  -> applyNativeTTL()
  -> 写入 ttlvalue
  -> 写入 iptables TTL 规则
```

### 控制台流程

```text
浏览器 /#console
  -> 单页内容区 iframe 加载 /console
  -> iframe 高度跟随右侧内容区自动撑满
  -> 控制台内部隐藏可见滚动条，避免内外双滚动
  -> WebSocket /api/console/ws
  -> handleNativeConsoleWebSocket()
  -> 校验 Origin 与当前 Host/协议一致
  -> startNativeConsoleShell()
  -> PTY shell（TERM=xterm-256color / COLORTERM=truecolor）
  -> WebSocket frame 双向转发
  -> iframe 内 ANSI SGR 渲染器显示彩色文本
```

## 运行和调试命令

### 安装

```bat
toolkit.bat
```

### 卸载

```bat
uninstall.bat
```

### 模块端查看服务

```sh
systemctl status simpleadmin-httpd.service --no-pager
journalctl -u simpleadmin-httpd.service -n 150 --no-pager
ps | grep '[s]impleadmin-httpd'
cat /tmp/simpleadmin-httpd.log
```

### 模块端测试 AT

```sh
/usrdata/simpleadmin/simpleadmin-httpd at --debug --devices /dev/smd11 --timeout-ms 1000 ATI
```

### 模块端查看 AT 配置

```sh
cat /usrdata/simpleadmin/at_devices.conf
```

### Windows 本地测试

```bat
run_windows_test.bat
```

Windows 测试服务使用 `--mock` 模式。启动后的命令行窗口支持手动输入首页 AT 测试数据，用于验证后端结构化解析是否正常：

```text
simpleadmin-mock> at      粘贴整段首页 AT 返回，单独一行 .end 结束
simpleadmin-mock> qca     粘贴 QCAINFO 返回，单独一行 .end 结束
simpleadmin-mock> qeng    粘贴 QENG 返回，单独一行 .end 结束
simpleadmin-mock> parse   在控制台打印 /api/dashboard_data 的解析结果
simpleadmin-mock> clear   清除手动输入，恢复默认 mock 数据
```

也可以在浏览器开发者工具 Console 里输入；`index.js` 已不再内置 AT 测试常量，Windows mock 控制台输入仍然保留：

```js
SimpleAdmin.MockAT.at(`粘贴整段首页 AT 返回`)
SimpleAdmin.MockAT.qca(`粘贴 QCAINFO 返回`)
SimpleAdmin.MockAT.qeng(`粘贴 QENG 返回`)
SimpleAdmin.MockAT.parse()
SimpleAdmin.MockAT.show()
SimpleAdmin.MockAT.clear()
```

简写别名：`saAt(...)`、`saQca(...)`、`saQeng(...)`、`saParseAT()`、`saShowAT()`、`saClearAT()`。这些接口只在 `--mock` 测试模式生效，模块真实运行时会拒绝。

### Windows 重新编译 Go 后端

```bat
go-build\build_simpleadmin_go.bat
```

该脚本应使用 Go 1.26.3；AT 写入流程默认直接使用 `/dev/smd11`，首次真实 AT 请求会启动独立 reader 子进程保持读取端，写入端使用 `/bin/sh -c 'cat > "$1"'` 接收 stdin payload 并重定向写入 `/dev/smd11`，不使用 `printf` 拼接命令；reader 使用 4096 字节读取大小并在打开设备成功后再允许 writer 发送，响应完成以 AT 终止行 `OK`、`ERROR`、`+CME ERROR`、`+CMS ERROR` 为准，不靠固定 kill；运行时不创建 `socat`、`/dev/ttyIN`、`/dev/ttyOUT` 或读写桥接；查询型 `AT+QMAP="LANIP"` 只在固定页面缓存指令 `commonATCacheCommands()` / `pageATCommands()` 中独立成单条，设备信息和网络设置会按静态/配置/实时属性拆分成多条固定缓存指令；手动 AT 和底层发送函数不做通用分号拆分；页面停留和轮询期间不反复清理或重建 `/dev/smd11`。
