# Windows 本地测试 SimpleAdmin Go 服务

这个目录用于在 Windows 上测试 Go 版 SimpleAdmin 的 Web/React 页面、登录认证、路由和 `/api/*` 接口。

## 一键运行

在项目根目录双击：

```bat
run_windows_test.bat
```

浏览器会打开：

```text
http://127.0.0.1:18080/
```

默认账号密码：

```text
admin / admin
```

## 运行模式

Windows 测试使用 `--mock` 模式：

- 不访问真实 `/dev/smd11`
- 不创建 Linux PTY 造口
- 不调用 `iptables/ip6tables`
- 不安装 systemd 服务
- AT、短信、TTL、基础状态返回本地模拟数据
- `/console` 只显示 Windows 测试说明，不启动 Linux 原生 PTY shell
- 启动窗口支持手动输入首页 AT 测试数据，不需要改前端代码

这样可以在没有模块、没有 ADB、没有串口设备的 Windows 电脑上检查页面和接口是否正常。

## 手动输入首页 AT 测试数据

运行 `run_windows_test.bat` 后，在启动服务的命令行窗口输入：

```text
at
```

然后粘贴整段首页 AT 返回，最后单独输入一行：

```text
.end
```

页面下一次刷新会使用这段数据，后端会通过 `/api/dashboard_data` 返回解析后的结构化 JSON。

也可以只替换部分内容：

```text
qca     # 粘贴 QCAINFO 测试数据
qeng    # 粘贴 QENG 测试数据
parse   # 在控制台打印当前首页解析结果
show    # 查看当前是否启用手动输入
clear   # 清除全部手动输入，恢复默认 mock 数据
```

## 浏览器端 mock 数据注入

旧前端在浏览器开发者工具 Console 提供的 `SimpleAdmin.MockAT.*` / `saAt(...)` 等全局命令已随 Vue 管线退役。后端 `/api/mock_at` 接口保留（只在 `--mock` 模式生效，真实模块运行时拒绝；经 `/api/ws` 网关访问，供前端测试代码使用），日常调试请优先使用上面命令行窗口的输入方式。

## 前端测试

前端校验不再依赖本目录脚本（旧 `frontend_smoke.js` 已删除）：单元/组件测试与端到端测试位于 `development/simpleadmin/frontend-react/`——`make web-test`（typecheck + lint + vitest）、`make web-e2e`（Playwright，webServer 自起 mock 后端）。本目录只提供 Windows 手动检查页面与接口用的 mock 服务（`run_windows_test.bat`）；linux 侧等价为 `make dev-mock`。

## 手动编译

```bat
windows-test\build_windows_test.bat
```

输出文件：

```text
windows-test\bin\simpleadmin-httpd-windows-amd64.exe
```

## 本地数据

运行时数据保存在：

```text
windows-test\data\
```

包括：

- `simpleadmin.auth`
- `ttlvalue`
- `server.crt`
- `server.key`

## 与模块版本的区别

Windows 测试版只用于本地调试 Web 和 API 行为。真正安装到模块时仍然使用：

```text
toolkit.bat
```

模块运行的是：

```text
development\simpleadmin\simpleadmin-httpd.armv7
```

## 404 处理

如果浏览器显示：

```text
Access Error: 404 -- Not Found
Cannot locate document: /
```

通常不是 SimpleAdmin 页面本身的问题，而是浏览器访问到了其它旧服务，或者测试服务没有使用正确的静态网页目录。新版 `run_windows_test.bat` 已做这些处理：

- 使用 `18080` 端口，避开常见的 `8080` 冲突。
- 启动前检查 `development\simpleadmin\www\index.html` 是否存在。
- 启动时在窗口里打印实际使用的 `Static` 路径。
- Go 服务启动时会校验 `--static` 目录，不正确会直接报错退出，不会启动一个空 Web 根目录。

请使用根目录的：

```bat
run_windows_test.bat
```

然后访问：

```text
http://127.0.0.1:18080/
```
