package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSessionPurgeRemovesStaleCookieIssuedRecords 锚定修复:周期清理必须同步
// 删除 sessionCookieIssuedAt 中已无有效会话的孤儿记录,否则自然过期(未登出)
// 的会话留下永久残留,长跑进程中随累计登录次数无界增长。
func TestSessionPurgeRemovesStaleCookieIssuedRecords(t *testing.T) {
	now := time.Now()
	srv := &simpleAdminServer{
		sessions: map[string]time.Time{
			"expired-token": now.Add(-time.Hour),
			"valid-token":   now.Add(time.Hour),
		},
		sessionCookieIssuedAt: map[string]time.Time{
			"expired-token": now.Add(-2 * time.Hour),
			"valid-token":   now.Add(-time.Minute),
			"orphan-token":  now.Add(-3 * time.Hour),
		},
	}

	srv.purgeExpiredSessionsAndAttempts(now)

	if _, ok := srv.sessionCookieIssuedAt["expired-token"]; ok {
		t.Fatalf("cookie issued record for expired session not purged")
	}
	if _, ok := srv.sessionCookieIssuedAt["orphan-token"]; ok {
		t.Fatalf("orphan cookie issued record not purged")
	}
	if _, ok := srv.sessionCookieIssuedAt["valid-token"]; !ok {
		t.Fatalf("cookie issued record for valid session removed")
	}
}

// TestWriteLanguageConfigAtomicRoundTrip 锚定修复:语言配置写入必须原子
// (临时文件→Sync→rename),掉电不得留下半截 JSON;非法语言仍被拒绝。
func TestWriteLanguageConfigAtomicRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "get_language.json")

	if err := writeLanguageConfig(path, "auto"); err == nil {
		t.Fatal("writeLanguageConfig must reject unsupported language")
	}

	if err := writeLanguageConfig(path, "en"); err != nil {
		t.Fatalf("writeLanguageConfig(en): %v", err)
	}
	language, err := readLanguageConfig(path)
	if err != nil || language != "en" {
		t.Fatalf("round trip: language=%q err=%v, want en", language, err)
	}

	leftovers, err := filepath.Glob(filepath.Join(dir, "nested", ".simpleadmin.lang.*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temp files not cleaned up: %v", leftovers)
	}
}

// TestWriteAuthConfigSyncsBeforeRename 锚定修复:认证文件原子写在 rename 前
// 必须先落盘;以文件内容与权限往返验证写入路径整体正确。
func TestWriteAuthConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "simpleadmin.auth")

	if err := writeAuthConfig(path, authConfig{Username: "admin", Password: "s3cret"}); err != nil {
		t.Fatalf("writeAuthConfig: %v", err)
	}
	auth, err := loadAuthConfig(path)
	if err != nil || auth.Username != "admin" || auth.Password != "s3cret" {
		t.Fatalf("round trip: auth=%+v err=%v", auth, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat auth file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("auth file perm = %v, want 0600", info.Mode().Perm())
	}
}

// TestWriteTTLValueAtomicRoundTrip 锚定修复:TTL 状态文件写入必须原子,
// 写入后可被 readTTLValue 读回,且不留下临时文件。
func TestWriteTTLValueAtomicRoundTrip(t *testing.T) {
	oldTTLFile := runtimeTTLValueFile
	dir := t.TempDir()
	runtimeTTLValueFile = filepath.Join(dir, "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = oldTTLFile })

	if err := writeTTLValue(256); err == nil {
		t.Fatal("writeTTLValue must reject out-of-range value")
	}
	if err := writeTTLValue(-1); err == nil {
		t.Fatal("writeTTLValue must reject negative value")
	}

	if err := writeTTLValue(65); err != nil {
		t.Fatalf("writeTTLValue(65): %v", err)
	}
	value, err := readTTLValue()
	if err != nil || value != 65 {
		t.Fatalf("round trip: value=%d err=%v, want 65", value, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".simpleadmin.tmp.") {
			t.Fatalf("temp file not cleaned up: %s", entry.Name())
		}
	}
}
