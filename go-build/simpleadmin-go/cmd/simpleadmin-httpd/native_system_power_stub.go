//go:build !linux
// +build !linux

package main

import "errors"

const (
	powerActionReboot   = "reboot"
	powerActionPoweroff = "poweroff"
)

// nativeDevicePower 非 Linux 平台桩:不支持整机重启/关机(本地测试走 mock 模式)。
func nativeDevicePower(action string) error {
	return errors.New("device power control is only supported on linux")
}
