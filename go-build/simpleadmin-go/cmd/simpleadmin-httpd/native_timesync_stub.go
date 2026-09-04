//go:build !linux
// +build !linux

package main

import (
	"errors"
	"time"
)

// setSystemTime 非 Linux 平台桩:不支持修改系统时间(本地测试走 mock 模式)。
func setSystemTime(now time.Time) error {
	return errors.New("setSystemTime is only supported on linux")
}
