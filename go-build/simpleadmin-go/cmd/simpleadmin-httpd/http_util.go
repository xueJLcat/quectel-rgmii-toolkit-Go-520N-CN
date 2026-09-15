package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
	if !strings.HasSuffix(body, "\n") {
		_, _ = w.Write([]byte("\n"))
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// splitRequestListValues 把多值参数的每个值再按分隔符展开并去首尾空白。
// 调用方可能以重复参数与分隔串混合传值(如 indices=1,2&indices=3):
// 旧实现只在"恰好一个值"时拆分,混合形态下 "1,2" 整体进入后续解析,
// 被 stripNonDigits 洗成 "12"——删除错误短信索引/锁定错误频点。
func splitRequestListValues(values []string, sep string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, sep) {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return out
}
