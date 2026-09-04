package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type smsWebhookConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

var (
	smsWebhookMu          sync.Mutex
	smsWebhookHighWater   = map[string]int{}
	smsWebhookInitialized = map[string]bool{}
)

// smsWebhookPost 可注入，便于测试替换为记录器，不产生真实网络请求
var smsWebhookPost = defaultSMSWebhookPost

// smsWebhookRetryDelays 投递失败后的重试间隔序列，总尝试次数为 len+1。
// 声明为包级变量以便测试注入缩短间隔；生产值为约 2s、8s。
var smsWebhookRetryDelays = []time.Duration{2 * time.Second, 8 * time.Second}

// smsWebhookPollInterval 后台轮询器主动拉取短信列表的间隔。
// 声明为包级变量以便测试注入缩短间隔。
var smsWebhookPollInterval = 30 * time.Second

// smsWebhookPollWait 等待一个轮询间隔，stop 关闭时立即返回 false。
// 可在测试中替换，便于验证循环节奏与停止语义而无需真实等待。
var smsWebhookPollWait = func(d time.Duration, stop <-chan struct{}) bool {
	select {
	case <-time.After(d):
		return true
	case <-stop:
		return false
	}
}

// smsWebhookPollFetchDual 拉取后台轮询用的双存储短信列表，返回合并数据与
// 按存储拆分的有效性，可在测试中替换为罐头数据。生产路径复用与页面同一条
// 已验证的双存储读取(含收尾回置 CPMS 到 ME)：历史滞留 SM 的短信也能被轮询
// 器检测到，而不是只在用户打开短信页时才通知；读取经 AT 缓存，前端同时在
// 轮询时直接命中，不额外占用串行通道。
var smsWebhookPollFetchDual = func(s *simpleAdminServer) (map[string]any, map[string]bool) {
	return s.fetchSMSListDualStorage(false)
}

// smsWebhookPoller 生命周期状态：轮询循环仅在配置启用且 URL 非空时运行，
// 关闭开关后立即停止，不保留空转的常驻循环。
var (
	smsWebhookPollerMu   sync.Mutex
	smsWebhookPollerStop chan struct{}
)

func smsWebhookConfigPath() string {
	return filepath.Join(filepath.Dir(runtimeTTLValueFile), "sms_webhook.conf")
}

func readSMSWebhookConfig() (smsWebhookConfig, error) {
	data, err := os.ReadFile(smsWebhookConfigPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return smsWebhookConfig{}, nil
		}
		return smsWebhookConfig{}, err
	}
	var cfg smsWebhookConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return smsWebhookConfig{}, err
	}
	cfg.URL = strings.TrimSpace(cfg.URL)
	return cfg, nil
}

func writeSMSWebhookConfig(cfg smsWebhookConfig) error {
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.URL != "" && !strings.HasPrefix(cfg.URL, "http://") && !strings.HasPrefix(cfg.URL, "https://") {
		return fmt.Errorf("webhook url must start with http:// or https://")
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// 原子写避免掉电撕裂配置(半截 JSON 回退缺省会悄悄关闭短信转发)。
	return atomicWriteFile(smsWebhookConfigPath(), data, 0600)
}

func defaultSMSWebhookPost(url string, body []byte) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}
	return nil
}

func smsWebhookField(msg map[string]any, key string) string {
	if value, ok := msg[key]; ok && value != nil {
		return fmt.Sprint(value)
	}
	return ""
}

// smsWebhookMaxIndex 返回所有消息中的最大索引；无有效索引时 found 为 false。
func smsWebhookMaxIndex(messages []map[string]any) (maxIndex int, found bool) {
	maxIndex = -1
	for _, msg := range messages {
		for _, idx := range toIntSlice(msg["indices"]) {
			if !found || idx > maxIndex {
				maxIndex = idx
			}
			found = true
		}
	}
	return maxIndex, found
}

// deliverSMSWebhook 投递一条 webhook 通知：单次请求超时/非 2xx 均视为失败，
// 失败后按 smsWebhookRetryDelays 重试，共尝试 len(smsWebhookRetryDelays)+1 次；
// 全部失败时返回含尝试次数的错误。重试在调用方（后台 goroutine）内完成。
func deliverSMSWebhook(url string, body []byte) error {
	attempts := len(smsWebhookRetryDelays) + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		if lastErr = smsWebhookPost(url, body); lastErr == nil {
			return nil
		}
		if i < len(smsWebhookRetryDelays) {
			time.Sleep(smsWebhookRetryDelays[i])
		}
	}
	return fmt.Errorf("共尝试 %d 次均失败: %w", attempts, lastErr)
}

// notifyNewSMSByWebhook 扫描短信列表，对新到达的消息投递 webhook 通知。
// scanValid 表示本次扫描来自真实有效的 AT 应答（含 OK 行且无终结错误行）；
// 开机保护期 pending 文本、运行器错误文本解析出的空列表均为无效扫描。
// messages 携带 "storage"(ME/SM) 标记；高水位/初始化按存储独立维护——ME 与
// SM 索引空间各自独立，不能共用单一高水位，否则一个存储的高索引会漏报另一个
// 存储的新消息。扫描覆盖的存储从消息标记推导；某存储是否被覆盖以该存储自身
// 应答有效性为准（不能以"任一存储有效"的整体有效性替代），有效扫描的空收件
// 箱据此建立基线。
// 高水位语义（逐存储）：列表非空且当前最大索引低于高水位时，判定存储被清空/
// 索引复用，将高水位重置为当前最大索引-1，使当前最大索引的消息仍触发一次通知
// （删除后新消息复用旧索引时宁可重复一次，不可漏报；此后新增消息正常触发）；
// 该存储列表为空（或无有效索引）时：无效扫描保持高水位不变、不消耗首扫，等待
// 首次真实数据建立基线（否则保护期 pending→空会消耗首扫，导致存量消息
// 全量重推）；有效扫描且该存储尚未初始化说明收件箱确为空，以高水位 -1 建立
// 基线——否则空收件箱设备到达的第一条短信会被当成基线消耗而永不通知。
// notifyNewSMSByWebhook 保持两参形态供既有测试调用:把整体有效性 scanValid
// 施加到 ME 与 messages 中出现的每个存储上。生产路径(页面/轮询)改用按存储
// 区分有效性的 notifyNewSMSByWebhookStorages。
func notifyNewSMSByWebhook(messages []map[string]any, scanValid bool) {
	validStorages := map[string]bool{"ME": scanValid}
	for _, msg := range messages {
		validStorages[smsWebhookStorage(msg)] = scanValid
	}
	notifyNewSMSByWebhookStorages(messages, validStorages)
}

// notifyNewSMSByWebhookStorages 按存储独立的有效性推进高水位并投递通知。
// validStorages[storage] 为 true 表示该存储本次被真实有效地扫描过:即使其
// 收件箱为空(无消息)也要纳入 scanned 以建立基线。关键修复:ME 是否被覆盖
// 只能以 ME 自身应答有效性为准,不能用"ME 或 SM 任一有效"的整体有效性——
// 当 ME 读取失败而 SM 有效时,若仍强制 ME 已扫描,会为 ME 建立空基线
// (高水位 -1),随后首次真正的 ME 有效扫描会把整个存量收件箱全部当成新
// 到达逐条推送(全量误通知)。
func notifyNewSMSByWebhookStorages(messages []map[string]any, validStorages map[string]bool) {
	cfg, err := readSMSWebhookConfig()
	if err != nil {
		log.Printf("读取短信 webhook 配置失败: %v", err)
		return
	}
	if !cfg.Enabled || cfg.URL == "" {
		return
	}

	byStorage := map[string][]map[string]any{}
	scanned := map[string]bool{}
	for _, msg := range messages {
		storage := smsWebhookStorage(msg)
		byStorage[storage] = append(byStorage[storage], msg)
		scanned[storage] = true
	}
	// 有效扫描过的存储即使收件箱为空也视为已覆盖,据此建立基线。
	for storage, valid := range validStorages {
		if valid {
			scanned[storage] = true
		}
	}

	smsWebhookMu.Lock()
	defer smsWebhookMu.Unlock()

	for storage := range scanned {
		newMessages, newIndexes := smsWebhookAdvanceLocked(storage, byStorage[storage], validStorages[storage])
		for i, msg := range newMessages {
			payload := map[string]any{
				"sender":  smsWebhookField(msg, "sender"),
				"date":    smsWebhookField(msg, "date"),
				"text":    smsWebhookField(msg, "text"),
				"index":   newIndexes[i],
				"storage": storage,
			}
			body, err := json.Marshal(payload)
			if err != nil {
				log.Printf("短信 webhook 消息序列化失败: %v", err)
				continue
			}
			go func(body []byte) {
				if err := deliverSMSWebhook(cfg.URL, body); err != nil {
					log.Printf("短信 webhook 通知失败（重试已用尽）: %v", err)
				}
			}(body)
		}
	}
}

// smsWebhookStorage 归一化消息的存储标记，缺省按 ME 处理（兼容旧数据）。
func smsWebhookStorage(msg map[string]any) string {
	if s, ok := msg["storage"].(string); ok && s == "SM" {
		return "SM"
	}
	return "ME"
}

// smsWebhookAdvanceLocked 推进单个存储的高水位并返回新到达消息。
// 调用方必须持有 smsWebhookMu。
func smsWebhookAdvanceLocked(storage string, messages []map[string]any, scanValid bool) ([]map[string]any, []int) {
	// 无效扫描(保护期 pending、超时截断的部分列表、+CMS ERROR 终结的半截
	// 应答)一律不触碰高水位与初始化状态:部分列表的最大索引若被采纳,
	// 会把高水位拉低,下一次有效扫描将已存在的更高索引消息全部当成新到达
	// 重推(重复通知);无效首扫消耗基线则会造成漏报。无论列表是否为空,
	// 统一等待有效扫描再推进。
	if !scanValid {
		return nil, nil
	}
	currentMax, found := smsWebhookMaxIndex(messages)
	if !found {
		if !smsWebhookInitialized[storage] {
			// 该存储收件箱确为空：以高水位 -1 建立基线，此后到达的任何消息
			// （索引 > -1）都判为新到达并通知。
			smsWebhookInitialized[storage] = true
			smsWebhookHighWater[storage] = -1
		}
		// 已初始化后的空扫描保持高水位不变。
		return nil, nil
	}
	highWater := smsWebhookHighWater[storage]
	firstCall := !smsWebhookInitialized[storage]
	smsWebhookInitialized[storage] = true
	if !firstCall && currentMax < highWater {
		// 重置为当前最大索引-1 而非直接到最大索引：删除后新消息复用旧索引
		// （≤高水位）时，该条真正的新消息仍能触发一次通知；
		// 宁可重复一次，不可漏报。列表中更低的旧索引不会补发。
		log.Printf("短信 webhook[%s]: 当前最大索引 %d 低于高水位 %d，判定存储被清空/索引复用，重置高水位为 %d（当前最大索引的消息仍会通知一次）", storage, currentMax, highWater, currentMax-1)
		highWater = currentMax - 1
	}

	newMessages := []map[string]any{}
	newIndexes := []int{}
	if !firstCall {
		for _, msg := range messages {
			indices := toIntSlice(msg["indices"])
			msgMax := -1
			isNew := false
			for _, idx := range indices {
				if idx > msgMax {
					msgMax = idx
				}
				if idx > highWater {
					isNew = true
				}
			}
			if isNew && msgMax >= 0 {
				newMessages = append(newMessages, msg)
				newIndexes = append(newIndexes, msgMax)
			}
		}
	}
	smsWebhookHighWater[storage] = currentMax
	return newMessages, newIndexes
}

// syncSMSWebhookPoller 按配置同步轮询器运行状态：启用且 URL 非空时启动
// （幂等），关闭或 URL 清空时停止循环（幂等）。启动时与保存配置后调用。
func syncSMSWebhookPoller(s *simpleAdminServer) {
	cfg, err := readSMSWebhookConfig()
	if err != nil {
		log.Printf("读取短信 webhook 配置失败: %v", err)
		return
	}
	if cfg.Enabled && cfg.URL != "" {
		startSMSWebhookPoller(s)
		return
	}
	stopSMSWebhookPoller()
}

// startSMSWebhookPoller 启动短信转发后台轮询（幂等）：无人打开短信页时
// 新到达检测也会周期性执行，webhook 不再依赖浏览器停留短信页。循环先等
// 一个间隔再首次检测（与看门狗一致），开机保护期内的拉取返回 pending，
// 由 notifyNewSMSByWebhook 的空列表语义安全消化，不会误报。
func startSMSWebhookPoller(s *simpleAdminServer) {
	smsWebhookPollerMu.Lock()
	defer smsWebhookPollerMu.Unlock()
	if smsWebhookPollerStop != nil {
		return
	}
	stop := make(chan struct{})
	smsWebhookPollerStop = stop
	go func() {
		for smsWebhookPollWait(smsWebhookPollInterval, stop) {
			smsWebhookPollTick(s)
		}
	}()
}

// stopSMSWebhookPoller 停止后台轮询循环（幂等）：等待间隔期间也能立即退出。
func stopSMSWebhookPoller() {
	smsWebhookPollerMu.Lock()
	defer smsWebhookPollerMu.Unlock()
	if smsWebhookPollerStop == nil {
		return
	}
	close(smsWebhookPollerStop)
	smsWebhookPollerStop = nil
}

// smsWebhookPollerRunning 返回轮询循环当前是否在运行。
func smsWebhookPollerRunning() bool {
	smsWebhookPollerMu.Lock()
	defer smsWebhookPollerMu.Unlock()
	return smsWebhookPollerStop != nil
}

// smsWebhookPollTick 执行一次后台轮询：轮询器仅在启用时运行，此处仍复查
// 配置作为关闭竞态下的防御——未启用或未配置 URL 时直接跳过，不产生任何
// AT 流量；否则经双存储读取拉取并解析短信列表，复用
// notifyNewSMSByWebhookStorages 的逐存储高水位判断。空应答（保护期
// pending、运行器错误文本）解析为空列表且该存储有效性为假，高水位逻辑保持
// 不变不消耗首扫。
func smsWebhookPollTick(s *simpleAdminServer) {
	cfg, err := readSMSWebhookConfig()
	if err != nil {
		log.Printf("读取短信 webhook 配置失败: %v", err)
		return
	}
	if !cfg.Enabled || cfg.URL == "" {
		return
	}
	data, validStorages := smsWebhookPollFetchDual(s)
	notifyNewSMSByWebhookStorages(smsDataMessages(data), validStorages)
}

func currentSMSWebhookStatus() map[string]any {
	cfg, err := readSMSWebhookConfig()
	if err != nil {
		log.Printf("读取短信 webhook 配置失败: %v", err)
	}
	smsWebhookMu.Lock()
	defer smsWebhookMu.Unlock()
	lastIndex := -1
	for storage, initialized := range smsWebhookInitialized {
		if !initialized {
			continue
		}
		if hw := smsWebhookHighWater[storage]; hw > lastIndex {
			lastIndex = hw
		}
	}
	return map[string]any{
		"enabled":           cfg.Enabled,
		"url":               cfg.URL,
		"lastNotifiedIndex": lastIndex,
	}
}
