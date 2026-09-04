//go:build linux
// +build linux

package main

import (
	"syscall"
	"time"
)

// setSystemTime 直接步进修改系统时间(等价 settimeofday),供时间同步使用。
// 需要 CAP_SYS_TIME,进程由 systemd 以 root 运行。
func setSystemTime(now time.Time) error {
	tv := syscall.NsecToTimeval(now.UnixNano())
	return syscall.Settimeofday(&tv)
}
