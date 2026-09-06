# Go Build

这里是 SimpleAdmin Go 源码和 Windows 编译入口。

普通安装不需要运行本目录脚本；只有修改 Go 源码后需要重新生成 `simpleadmin-httpd.armv7` 时才运行：

```bat
build_simpleadmin_go.bat
```

当前 Go 源码内置：

- Go 原生 AT 读写；AT 通道默认直接使用 `/dev/smd11`，并对 SMD 设备使用进程内常驻读取端和短生命周期写入端，不再创建 `socat`、`/dev/ttyIN` 或 `/dev/ttyOUT` 桥接
- AT 文件锁和设备 mutex
- Go 原生 TTL 管理
- Go 原生 Web 控制台
- `/api/*` handler，兼容 `/cgi-bin/*` 别名
- React（Vite 构建产物 `www/`）静态页面托管

默认 AT 候选固定为：

```text
/dev/smd11
```

## Windows 编译说明

`build_simpleadmin_go.bat` 支持 Go 1.26.3。脚本会设置 `GOOS=linux`、`GOARCH=arm`、`GOARM=7`、`CGO_ENABLED=0`、`GOTOOLCHAIN=local`，并使用 `-buildvcs=false -trimpath -ldflags "-s -w"` 编译模块端 ARMv7 二进制。

编译完成后脚本会执行 `go version -m` 输出产物信息，便于确认实际使用的 Go 版本和目标架构。
