//go:build linux
// +build linux

// native_system_power.go 设备整机重启/关机:执行系统 reboot / poweroff 命令
// (systemd 环境下二者均为 systemctl 的符号链接)。
package main

import (
	"fmt"
	"os/exec"
)

const (
	powerActionReboot   = "reboot"
	powerActionPoweroff = "poweroff"
)

// nativeDevicePower 异步启动 reboot/poweroff 命令:systemd 关机流程需要数秒
// (先停服务再断电),不等待命令返回,先应答前端;进程被系统停止前响应已发出。
func nativeDevicePower(action string) error {
	if action != powerActionReboot && action != powerActionPoweroff {
		return fmt.Errorf("unsupported power action: %s", action)
	}
	path, err := exec.LookPath(action)
	if err != nil {
		return fmt.Errorf("%s 命令不可用: %v", action, err)
	}
	cmd := exec.Command(path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %v", action, err)
	}
	// 异步回收子进程;系统停机过程中 Wait 大概率被打断,忽略其错误。
	go func() { _ = cmd.Wait() }()
	return nil
}
