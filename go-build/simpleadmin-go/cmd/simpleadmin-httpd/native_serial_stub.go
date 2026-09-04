//go:build !linux
// +build !linux

package main

import (
	"fmt"
	"os"
	"time"
)

type atCommandSession struct{}

func openNativeTTY(path string) (*os.File, error) {
	return nil, fmt.Errorf("native serial device %s is only available on Linux module builds", path)
}

func openATCommandSession(path string) (*atCommandSession, error) {
	return nil, fmt.Errorf("native AT device %s is only available on Linux module builds", path)
}

func closeATSession(session *atCommandSession) error { return nil }

func writeATSessionString(session *atCommandSession, payload string) error {
	return fmt.Errorf("native AT writes are only available on Linux module builds")
}

func drainTTY(f *os.File, maxDuration time.Duration) {}

func drainATSession(session *atCommandSession, maxDuration time.Duration) {}

func readTTYUntilTokens(f *os.File, maxDuration time.Duration, terminalTokens ...string) (string, error) {
	return "", fmt.Errorf("native serial reads are only available on Linux module builds")
}

func readATSessionUntilTokens(session *atCommandSession, maxDuration time.Duration, echoMarker string, terminalTokens ...string) (string, error) {
	return "", fmt.Errorf("native AT reads are only available on Linux module builds")
}

func lockGlobalATFile() (func(), error) { return func() {}, nil }

func shouldRecoverBusyATDevice(device string, err error) bool { return false }

func recoverBusyATDevices() {}

func markATSessionDirty(session *atCommandSession) {}

func runSMDReaderCommand(args []string) {
	fmt.Fprintln(os.Stderr, "smd reader helper is only available on Linux module builds")
	os.Exit(1)
}
