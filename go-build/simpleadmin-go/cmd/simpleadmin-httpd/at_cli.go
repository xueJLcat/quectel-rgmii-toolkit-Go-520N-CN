package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

// atLockHeldErrorMarker 是全局 AT 文件锁获取超时错误的固定文案片段
// (见 native_serial.go 的 errATLockHeldByAnotherProcess)。
const atLockHeldErrorMarker = "AT lock held by another process"

// atLockContentionHint 提示命令行用户:主服务持锁时 CLI 往往抢不到
// AT 通道(或被 30s 锁超时挡住),应改用服务接口。
const atLockContentionHint = "可能有其他进程(如正在运行的 simpleadmin-httpd 服务)占用 AT 通道,建议改用服务接口"

// annotateATLockError 在 AT 全局锁获取超时的错误上附加占用提示;
// 其他错误原样返回。按错误文案匹配而非 errors.Is,
// 以便在无锁实现的非 linux 平台上也能编译。
func annotateATLockError(err error) error {
	if err != nil && strings.Contains(err.Error(), atLockHeldErrorMarker) {
		return fmt.Errorf("%w; %s", err, atLockContentionHint)
	}
	return err
}

func envBool(name string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on", "debug":
		return true
	default:
		return false
	}
}

func logATDebug(format string, args ...any) {
	if !atDebugEnabled {
		return
	}
	log.Printf("[AT-DEBUG] "+format, args...)
}

func atPayloadSummary(payload string) string {
	clean := strings.ReplaceAll(payload, "\r", "\\r")
	clean = strings.ReplaceAll(clean, "\n", "\\n")
	clean = strings.ReplaceAll(clean, "\x1a", "<CTRL-Z>")
	if len(clean) > 160 {
		clean = clean[:160] + "..."
	}
	return fmt.Sprintf("len=%d data=%q", len(payload), clean)
}

func outputSummary(output string) string {
	clean := strings.ReplaceAll(output, "\r", "\\r")
	clean = strings.ReplaceAll(clean, "\n", "\\n")
	if len(clean) > 240 {
		clean = clean[:240] + "..."
	}
	return fmt.Sprintf("len=%d data=%q", len(output), clean)
}

func logATDeviceStat(label string, devices []string) {
	if !atDebugEnabled {
		return
	}
	for _, device := range devices {
		info, err := os.Lstat(device)
		if err != nil {
			logATDebug("%s %s: stat error: %v", label, device, err)
			continue
		}
		target := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if link, linkErr := os.Readlink(device); linkErr == nil {
				target = " -> " + link
			}
		}
		logATDebug("%s %s%s: mode=%s size=%d", label, device, target, info.Mode().String(), info.Size())
	}
}

func runATCLICommand(args []string) {
	fs := flag.NewFlagSet("at", flag.ExitOnError)
	devicesFlag := fs.String("devices", "", "AT device candidates, separated by comma or space")
	devicesFile := fs.String("devices-file", defaultATDevicesFile, "AT device candidates file")
	timeoutMS := fs.Int("timeout-ms", 0, "AT command timeout in milliseconds (0 = auto by command type)")
	debugFlag := fs.Bool("debug", envBool("SIMPLEADMIN_AT_DEBUG"), "print detailed AT debug logs")
	_ = fs.Parse(args)

	command := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if command == "" {
		fmt.Fprintln(os.Stderr, "usage: simpleadmin-httpd at [--devices /dev/smd11] [--timeout-ms 1000] ATI")
		os.Exit(2)
	}

	// 未显式指定 --timeout-ms 时按命令类型取缺省超时(扫网 120s、
	// MPDN_RULE 查询 10s、其余 1s),与服务端策略一致,避免慢命令假超时。
	explicitTimeout := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "timeout-ms" {
			explicitTimeout = true
		}
	})
	effectiveTimeoutMS := *timeoutMS
	if !explicitTimeout || effectiveTimeoutMS <= 0 {
		effectiveTimeoutMS = atCommandTimeoutMS(command)
	}

	atDebugEnabled = *debugFlag
	runtimeATDevices = collectATDeviceCandidates(*devicesFlag, *devicesFile)
	log.Printf("AT CLI debug logging: %v", atDebugEnabled)
	log.Printf("AT CLI candidates: %s", strings.Join(runtimeATDevices, ", "))
	logATDeviceStat("cli candidate", runtimeATDevices)
	if len(runtimeATDevices) == 0 {
		runtimeATDevices = defaultATDeviceCandidates()
	}

	out, err := runATCommandUntilDone(command, 200, effectiveTimeoutMS)
	if strings.TrimSpace(out) != "" {
		fmt.Print(out)
		if !strings.HasSuffix(out, "\n") {
			fmt.Println()
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "AT command failed: %v\n", annotateATLockError(err))
		os.Exit(1)
	}
}
