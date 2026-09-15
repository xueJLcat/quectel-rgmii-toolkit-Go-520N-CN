package main

// Server酱 短信推送通道(https://sct.ftqq.com)。对接官方公开 API:
//   - Turbo(SCT 开头的 SendKey):POST https://sctapi.ftqq.com/{SendKey}.send
//   - Server酱³(sctp{uid}t… 开头):POST https://{uid}.push.ft07.com/send/{SendKey}.send
//
// 请求体 JSON {"title","desp"}:title 必填、不得含换行、最长 32 字符(超出
// 服务端截断,这里先按 rune 截断);desp 为 Markdown 正文。应答 JSON 的
// code==0 为成功,非 0 时 message 说明原因(额度用尽/频率限制等)。
// 端点推导规则与官方 SDK(serverchan-sdk)一致,按 SendKey 前缀自动识别。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type smsServerChanConfig struct {
	Enabled bool   `json:"enabled"`
	SendKey string `json:"sendKey"`
}

// serverChanPost 可注入,便于测试替换为记录器,不产生真实网络请求。
var serverChanPost = defaultServerChanPost

// serverChanSctpKeyPattern 匹配 Server酱³ SendKey:sctp{uid}t…,uid 为数字。
var serverChanSctpKeyPattern = regexp.MustCompile(`^sctp(\d+)t\S+$`)

const (
	serverChanTitleMaxRunes = 32
	serverChanPostTimeout   = 15 * time.Second
)

func smsServerChanConfigPath() string {
	return filepath.Join(filepath.Dir(runtimeTTLValueFile), "sms_serverchan.conf")
}

func readSMSServerChanConfig() (smsServerChanConfig, error) {
	data, err := os.ReadFile(smsServerChanConfigPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return smsServerChanConfig{}, nil
		}
		return smsServerChanConfig{}, err
	}
	var cfg smsServerChanConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return smsServerChanConfig{}, err
	}
	cfg.SendKey = strings.TrimSpace(cfg.SendKey)
	return cfg, nil
}

func writeSMSServerChanConfig(cfg smsServerChanConfig) error {
	cfg.SendKey = strings.TrimSpace(cfg.SendKey)
	if cfg.SendKey != "" {
		if _, err := serverChanSendURL(cfg.SendKey); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// 原子写与 webhook 配置同理由:半截 JSON 回退缺省会悄悄关闭推送。
	return atomicWriteFile(smsServerChanConfigPath(), data, 0600)
}

// serverChanSendURL 按 SendKey 前缀推导推送端点(官方 SDK 口径):
// sctp{uid}t… → https://{uid}.push.ft07.com/send/{key}.send;
// 其余(Turbo) → https://sctapi.ftqq.com/{key}.send。
// key 会拼进 URL 路径,含空白或 URL 保留字符即拒绝,防注入。
func serverChanSendURL(sendKey string) (string, error) {
	key := strings.TrimSpace(sendKey)
	if key == "" {
		return "", errors.New("SendKey 不能为空")
	}
	if strings.HasPrefix(key, "sctp") {
		matched := serverChanSctpKeyPattern.FindStringSubmatch(key)
		if matched == nil {
			return "", errors.New("sctp SendKey 格式无效，应为 sctp{数字}t… 形态")
		}
		return "https://" + matched[1] + ".push.ft07.com/send/" + key + ".send", nil
	}
	if strings.ContainsAny(key, " \t\r\n/?#%") {
		return "", errors.New("SendKey 含空白或非法字符")
	}
	return "https://sctapi.ftqq.com/" + key + ".send", nil
}

// sanitizeServerChanTitle 标题不得含换行(官方 FAQ 明确要求),并按 rune
// 截断到 32 字符上限,避免服务端截断产生半截多字节字符。
func sanitizeServerChanTitle(title string) string {
	title = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(title)
	title = strings.TrimSpace(title)
	if runes := []rune(title); len(runes) > serverChanTitleMaxRunes {
		title = string(runes[:serverChanTitleMaxRunes])
	}
	return title
}

// buildServerChanSMSMessage 由转发载荷生成推送标题与 Markdown 正文。
// desp 各段以空行分隔:Markdown 单换行不分段(官方接入示例口径)。
func buildServerChanSMSMessage(payload map[string]any) (title string, desp string) {
	sender := smsWebhookField(payload, "sender")
	if sender == "" {
		sender = "未知发件人"
	}
	text := smsWebhookField(payload, "text")
	if text == "" {
		text = "（无内容）"
	}
	title = sanitizeServerChanTitle("新短信：" + sender)
	desp = fmt.Sprintf("**发件人**：%s\n\n**时间**：%s\n\n**内容**：%s\n\n存储 %s · 索引 %s",
		sender,
		smsWebhookField(payload, "date"),
		text,
		smsWebhookField(payload, "storage"),
		smsWebhookField(payload, "index"),
	)
	return title, desp
}

func defaultServerChanPost(apiURL string, body []byte) error {
	client := &http.Client{Timeout: serverChanPostTimeout}
	resp, err := client.Post(apiURL, "application/json", bytes.NewReader(body))
	if err != nil {
		// url.Error 的文本包含完整 URL(内嵌 SendKey),剥掉外层只保留底层
		// 网络错误,防止密钥经日志/接口错误文本泄露。
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("无法解析 Server酱 应答: %s", strings.TrimSpace(string(data)))
	}
	if result.Code != 0 {
		reason := strings.TrimSpace(result.Message)
		if reason == "" {
			reason = "未返回原因"
		}
		return fmt.Errorf("Server酱 返回 code=%d: %s", result.Code, reason)
	}
	return nil
}

// deliverSMSServerChan 推送一条消息:单次请求失败/应答 code 非 0 均视为失败,
// 按 smsWebhookRetryDelays 重试(与 webhook 通道同节奏),全部失败返回含
// 尝试次数的错误。
func deliverSMSServerChan(sendKey string, title string, desp string) error {
	apiURL, err := serverChanSendURL(sendKey)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{
		"title": sanitizeServerChanTitle(title),
		"desp":  desp,
	})
	if err != nil {
		return err
	}
	attempts := len(smsWebhookRetryDelays) + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		if lastErr = serverChanPost(apiURL, body); lastErr == nil {
			return nil
		}
		if i < len(smsWebhookRetryDelays) {
			time.Sleep(smsWebhookRetryDelays[i])
		}
	}
	return fmt.Errorf("共尝试 %d 次均失败: %w", attempts, lastErr)
}

func currentSMSServerChanStatus() map[string]any {
	cfg, err := readSMSServerChanConfig()
	if err != nil {
		log.Printf("读取短信 Server酱 配置失败: %v", err)
	}
	return map[string]any{
		"enabled": cfg.Enabled,
		"sendKey": cfg.SendKey,
	}
}
