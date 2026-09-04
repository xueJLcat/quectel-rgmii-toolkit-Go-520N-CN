package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *simpleAdminServer) handleSMSData(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(requestValue(r, "action"))
	switch action {
	case "", "list":
		force := boolQuery(r, "force", false)
		data, validStorages := s.fetchSMSListDualStorage(force)
		notifyNewSMSByWebhookStorages(smsDataMessages(data), validStorages)
		writeJSON(w, http.StatusOK, data)
	case "list_meta":
		force := boolQuery(r, "force", false)
		data, validStorages := s.fetchSMSListDualStorage(force)
		notifyNewSMSByWebhookStorages(smsDataMessages(data), validStorages)
		writeJSON(w, http.StatusOK, smsListMetaOnly(data))
	case "delete_all":
		// ME 与 SM 两个存储分别清空,任一失败如实报告。
		okME := atResponseOK(s.runPageAction(`AT+CPMS="ME","ME","ME";+CMGD=,4`))
		okSM := atResponseOK(s.runPageAction(`AT+CPMS="SM","SM","SM";+CMGD=,4`))
		writeJSON(w, http.StatusOK, map[string]any{"ok": okME && okSM, "me": okME, "sm": okSM})
	case "delete_indices":
		values := requestValues(r, "indices")
		if len(values) == 1 && strings.Contains(values[0], ",") {
			values = strings.Split(values[0], ",")
		}
		if len(values) == 0 || (len(values) == 1 && strings.TrimSpace(values[0]) == "") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "missing indices"})
			return
		}
		writeJSON(w, http.StatusOK, s.deleteSMSIndicesByToken(values))
	case "sim_status":
		raw := s.fetchPageAT(atKeySIMStatus, true, true)
		// 开机/模块重启保护期内应答是 pending 占位文本,不是设备状态证据;
		// 若按 smsSIMInserted 判"未插卡",发送流程会以误导性的"未检测到 SIM 卡"
		// 拦截用户。此时返回 inserted+pending,交由发送流程继续,发送流程里有
		// 精确的"模块未就绪,请稍后重试"错误(见 normalizeSMSNumber/isCIMINotReady)。
		if strings.Contains(raw, atCachePendingText) {
			writeJSON(w, http.StatusOK, map[string]any{"inserted": true, "pending": true})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"inserted": smsSIMInserted(raw)})
	case "send":
		number := strings.TrimSpace(requestValue(r, "number"))
		message := requestValue(r, "message")
		result := s.sendSMSBusiness(number, message)
		// 契约:输入校验失败→400;AT/运行时执行失败(网络拒发、结果未知等)→
		// 200+ok:false。sendSMSBusiness 用 validation 标记区分两类,写出前移除该键。
		status := http.StatusOK
		if ok, _ := result["ok"].(bool); !ok {
			if _, isValidation := result["validation"]; isValidation {
				status = http.StatusBadRequest
			}
			delete(result, "validation")
		}
		writeJSON(w, status, result)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unsupported action"})
	}
}

// smsSIMInserted 依据 AT+QSIMSTAT?;+CPIN? 组合命令的原始应答判定是否已插卡。
// 要求正向证据:出现 +CPIN: 状态行且状态非空、非 NOT INSERTED 才判已插卡
// (READY 或 SIM PIN/SIM PUK 等需解锁的已知状态均表示卡在位)。
// 无证据(空响应、后台未就绪的 pending 文本、运行器错误文本)或出现明确
// 缺卡标志时判未插卡。旧实现仅靠“未出现缺卡标志”取反,空/错误文本会误报已插卡。
func smsSIMInserted(raw string) bool {
	upper := strings.ToUpper(raw)
	if strings.Contains(upper, "SIM NOT INSERTED") || strings.Contains(upper, "+CME ERROR: 10") {
		return false
	}
	for _, rawLine := range strings.Split(upper, "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "+CPIN:") {
			continue
		}
		state := strings.TrimSpace(strings.TrimPrefix(line, "+CPIN:"))
		return state != "" && state != "NOT INSERTED"
	}
	return false
}

func smsDataMessages(data map[string]any) []map[string]any {
	messages, _ := data["messages"].([]map[string]any)
	return messages
}

func smsListMetaOnly(data map[string]any) map[string]any {
	meta := map[string]any{}
	if centers, ok := data["serviceCenters"]; ok {
		meta["serviceCenters"] = centers
	}
	if pending, ok := data["pending"]; ok {
		meta["pending"] = pending
	}
	if errMsg, ok := data["error"]; ok {
		meta["error"] = errMsg
	}
	rawMessages, _ := data["messages"].([]map[string]any)
	messages := make([]map[string]any, 0, len(rawMessages))
	for _, msg := range rawMessages {
		item := map[string]any{}
		for _, key := range []string{"sender", "date", "indices", "storage", "concatTotal", "concatRef", "concatSeq"} {
			if value, ok := msg[key]; ok {
				item[key] = value
			}
		}
		messages = append(messages, item)
	}
	meta["messages"] = messages
	return meta
}

// parseSMSDeleteToken 解析删除令牌:"<storage>:<index>"(如 SM:2)或裸索引
// (兼容旧前端,按 ME 处理)。返回存储名与纯数字索引;非法返回空索引。
func parseSMSDeleteToken(token string) (string, string) {
	token = strings.TrimSpace(token)
	storage := "ME"
	if i := strings.Index(token, ":"); i > 0 {
		prefix := strings.ToUpper(token[:i])
		if prefix == "ME" || prefix == "SM" {
			storage = prefix
			token = token[i+1:]
		}
	}
	return storage, stripNonDigits(token)
}

// fetchSMSListDualStorage 分别读取 ME(模组)与 SM(SIM)两个存储的短信并合并。
// 固件按 CPMS mem3 路由入站短信,历史消息可能滞留 SM 而 ME 为空
// (借鉴 QManager 的 ME+SM 双存储路由),两个存储都读、按存储标记合并。
// 读取顺序刻意"先 SM 后 ME",并在收尾显式回置 +CPMS="ME","ME","ME":
// 每条列表命令会把自己的存储写回 CPMS,若不回置,缓存命中/并发交错可能让
// 入站短信路由停在容量仅 40 槽的 SM,抵消开机自愈(借鉴 QManager 触碰 SM 后
// 重新断言 ME 的做法)。返回合并数据与按存储拆分的有效性(各自以自身应答的
// atResponseOK 判定,供 webhook 逐存储推进高水位)。SM 读取失败不影响 ME 结果。
// 合并数据附带 pending/error 键(与其余聚合页接口语义一致):开机/模块重启
// 保护期内 ME 应答是 pending 占位文本,解析为空列表,若不标记,收件箱会
// 静默显示"无短信";读取完全失败时标记 error,前端据此显示失败横幅而不是
// 用空列表覆盖既有内容。
func (s *simpleAdminServer) fetchSMSListDualStorage(force bool) (map[string]any, map[string]bool) {
	rawSM := s.fetchPageAT(atKeySMSListSM, force, true)
	rawME := s.fetchPageAT(atKeySMSList, force, true)
	// 回置入站路由到 ME(轻量命令,确保收尾不停留在 SM)。失败重试一次,
	// 仍失败则记日志:入站路由停留在 SM(40 槽)时新短信会滞留 SIM 存储。
	if !s.cfg.mockMode {
		restore := `AT+CPMS="ME","ME","ME"`
		if resp := s.runPageAction(restore); !atResponseOK(resp) {
			if retry := s.runPageAction(restore); !atResponseOK(retry) {
				log.Printf("短信存储路由回置 ME 失败,入站短信可能滞留 SM: %s", outputSummary(retry))
			}
		}
	}
	me := parseSMSListAT(rawME, "ME")
	sm := parseSMSListAT(rawSM, "SM")
	merged, _ := mergeSMSListDual(me, sm)
	// 按存储拆分有效性,供 webhook 逐存储推进高水位:ME 是否被有效扫描只能以
	// ME 自身应答判定,不能用"ME 或 SM 任一有效"的整体有效性替代——否则 ME
	// 读取失败而 SM 有效时,webhook 会误以为 ME 已被覆盖(见
	// notifyNewSMSByWebhookStorages),为 ME 建立假基线后全量误推存量短信。
	validStorages := map[string]bool{
		"ME": atResponseOK(rawME),
		"SM": atResponseOK(rawSM),
	}
	pending := strings.Contains(rawME, atCachePendingText)
	merged["pending"] = pending
	if !pending && atReadFailed(rawME) {
		merged["error"] = "AT 数据读取失败，请检查模块或稍后重试"
	}
	return merged, validStorages
}

// mergeSMSListDual 合并 ME 与 SM 两个存储的解析结果:消息按 ME 在前、SM 在后
// 拼接,各自保留 "storage" 标记。SM 无有效消息(读取失败或确为空)时只返回 ME。
// 第二个返回值表示 SM 是否读到了有效消息。
func mergeSMSListDual(me, sm map[string]any) (map[string]any, bool) {
	if me == nil {
		me = map[string]any{}
	}
	meMsgs, _ := me["messages"].([]map[string]any)
	if meMsgs == nil {
		meMsgs = []map[string]any{}
	}
	if sm != nil {
		if smMsgs, ok := sm["messages"].([]map[string]any); ok && len(smMsgs) > 0 {
			merged := make([]map[string]any, 0, len(meMsgs)+len(smMsgs))
			merged = append(merged, meMsgs...)
			merged = append(merged, smMsgs...)
			me["messages"] = merged
			if _, hasSC := me["serviceCenters"]; !hasSC {
				me["serviceCenters"] = sm["serviceCenters"]
			}
			return me, true
		}
	}
	me["messages"] = meMsgs
	return me, false
}

// smsDeleteRun 删除命令执行器,声明为变量便于测试注入。
var smsDeleteRunForTest = (*simpleAdminServer).runPageAction

// deleteSMSIndicesByToken 按存储感知令牌("<存储>:<索引>"或裸索引按 ME)分组,
// 逐存储逐条删除并聚合结果。全部成功才 ok:true;任一存储部分失败时 ok:false
// 且 error 汇总已删数量。
func (s *simpleAdminServer) deleteSMSIndicesByToken(values []string) map[string]any {
	byStorage := map[string][]string{}
	order := []string{}
	for _, value := range values {
		storage, index := parseSMSDeleteToken(value)
		if index == "" {
			continue
		}
		if _, ok := byStorage[storage]; !ok {
			order = append(order, storage)
		}
		byStorage[storage] = append(byStorage[storage], index)
	}
	if len(order) == 0 {
		return map[string]any{"ok": false, "error": "missing indices"}
	}
	result := map[string]any{"ok": true, "deleted": 0, "total": 0, "response": ""}
	for _, storage := range order {
		part := runSMSDeleteIndices(byStorage[storage], func(command string) string {
			return smsDeleteRunForTest(s, command)
		}, storage)
		result["deleted"] = toInt(fmt.Sprint(result["deleted"])) + toInt(fmt.Sprint(part["deleted"]))
		result["total"] = toInt(fmt.Sprint(result["total"])) + toInt(fmt.Sprint(part["total"]))
		if resp := fmt.Sprint(part["response"]); resp != "" {
			if prev := fmt.Sprint(result["response"]); prev != "" {
				result["response"] = prev + "\n" + resp
			} else {
				result["response"] = resp
			}
		}
		if ok, _ := part["ok"].(bool); !ok {
			result["ok"] = false
			result["error"] = part["error"]
		}
	}
	return result
}

// runSMSDeleteIndices 逐条删除指定存储内的短信(每条独立一条完整命令,
// 前置 +CPMS 把 mem1 切到目标存储,保证删除命中正确存储)。
// 全部成功才 ok:true;部分失败时 ok:false 且 error 含已删数量。
// run 注入命令执行器,便于单测。
func runSMSDeleteIndices(indices []string, run func(command string) string, storage string) map[string]any {
	if storage != "SM" {
		storage = "ME"
	}
	responses := make([]string, 0, len(indices))
	failedIndices := []string{}
	for _, index := range indices {
		resp := run(fmt.Sprintf(`AT+CPMS="%s","%s","%s";+CMGD=%s`, storage, storage, storage, index))
		responses = append(responses, resp)
		if !atResponseOK(resp) {
			failedIndices = append(failedIndices, index)
		}
	}
	deleted := len(indices) - len(failedIndices)
	result := map[string]any{
		"ok":       len(failedIndices) == 0,
		"response": strings.Join(responses, "\n"),
		"deleted":  deleted,
		"total":    len(indices),
	}
	if len(failedIndices) > 0 {
		result["error"] = fmt.Sprintf("部分删除失败: 已删 %d/%d，失败索引(%s): %s", deleted, len(indices), storage, strings.Join(failedIndices, ","))
	}
	return result
}

// assertSMSStorageRoutingAtStartup 开机自愈(借鉴 QManager 的 boot oneshot):
// 固件按 CPMS mem3 路由入站短信,缺省可能指向 SM,新消息会滞留 SIM 而 ME 为空、
// 收件箱看似无短信。列表命令虽每次读取都会重置 +CPMS="ME","ME","ME",但无人
// 打开短信页时不执行;开机后主动断言一次,确保入站短信从一开始就落在 ME。
// 与开机保护期协同:等待保护期结束再执行,失败仅日志不阻断。
func (s *simpleAdminServer) assertSMSStorageRoutingAtStartup() {
	if s.cfg.mockMode {
		return
	}
	atCommandCache.Start(s.cfg.mockMode)
	if delay := atCommandCache.startupDelayRemaining(); delay > 0 {
		time.Sleep(delay)
	}
	resp := s.runPageAction(`AT+CPMS="ME","ME","ME"`)
	if !atResponseOK(resp) {
		log.Printf("开机短信存储路由自愈失败(不影响使用,列表读取会再次断言): %s", outputSummary(resp))
	}
}

func (s *simpleAdminServer) sendSMSBusiness(number string, message string) map[string]any {
	message = strings.TrimSpace(message)
	number = strings.TrimSpace(number)
	if number == "" || message == "" {
		return map[string]any{"ok": false, "error": "missing number or message", "validation": true}
	}
	// 号码含字母是纯输入错误(校验类),在任何 AT 交互前拦截并标记,
	// 与稍后运行时失败(网络拒发/结果未知)区分状态码。
	if smsNumberLetterRE.MatchString(number) {
		return map[string]any{"ok": false, "error": fmt.Sprintf("号码包含字母，无法发送短信: %s", number), "validation": true}
	}
	number, err := normalizeSMSNumber(number, s.fetchPageAT(atKeyIMSI, true, true))
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	if number == "" {
		return map[string]any{"ok": false, "error": "missing number or message", "validation": true}
	}

	uid := int(time.Now().UnixNano() % 255)
	if uid <= 0 {
		uid = 1
	}
	segments := splitSMSVendorSegments(message)
	if len(segments) == 0 {
		return map[string]any{"ok": false, "error": "empty message", "validation": true}
	}

	responses := make([]string, 0, len(segments))
	for i, segment := range segments {
		sendCmd := buildSMSVendorSendCommand(number, uid, i+1, len(segments))
		var resp string
		var err error
		if s.cfg.mockMode {
			resp = mockSMSSendResponse(number)
		} else {
			resp, err = runSMSTransaction(sendCmd, segment)
		}
		responses = append(responses, resp)
		if err != nil && strings.TrimSpace(resp) == "" {
			return map[string]any{"ok": false, "error": err.Error(), "segment": i + 1}
		}
		// 正文已写入模块但未收到终结应答(结果未知)是哨兵语义:短信可能已发送。
		// 旧实现因 resp 非空而落进 !atResponseOK 分支,被归一成 "UNKNOWN",
		// 丢掉了"可能已发送"的信息,诱导用户盲目重发造成重复短信。
		// 必须先于 atResponseOK 判定,单独返回"请勿重复发送"提示。
		if errors.Is(err, errSMSResultUnknown) {
			return map[string]any{"ok": false, "error": "发送结果未知，短信可能已发送，请勿重复发送", "segment": i + 1, "result_unknown": true}
		}
		if !atResponseOK(resp) {
			return map[string]any{"ok": false, "error": smsSendErrorKind(resp), "segment": i + 1}
		}
	}
	return map[string]any{"ok": true, "segments": len(segments), "number": number}
}

func smsSendErrorKind(text string) string {
	up := strings.ToUpper(strings.TrimSpace(text))
	if m := regexp.MustCompile(`\+(CMS|CME) ERROR:\s*([0-9]+)`).FindStringSubmatch(up); len(m) > 2 {
		kind := m[1] + " " + m[2]
		if hint := smsSendErrorHint(m[1], m[2]); hint != "" {
			return kind + "（" + hint + "）"
		}
		return kind
	}
	if strings.Contains(up, "ERROR") {
		return "ERROR"
	}
	return "UNKNOWN"
}

// smsSendErrorHint 把发送失败的 CMS/CME 错误码映射为可操作的中文提示。
// 350 为本定制固件上最常见的发送拒绝:命令形态已被模块接受(出现 "> " 提示符
// 且正文被写入),但提交被网络/运营商即时拒绝——与短信中心号码选择无关
// (实测三组电信短信中心均同样拒绝),通常是 SIM 卡短信业务未开通/被限制
// (物联卡常见)或网络侧短信通道未就绪,需向运营商核实,而非本机配置问题。
func smsSendErrorHint(family, code string) string {
	if family != "CMS" {
		return ""
	}
	switch code {
	case "304":
		return "固件不接受该 PDU 形态,请使用厂商文本模式"
	case "305":
		return "文本模式参数不完整"
	case "310":
		return "SIM 卡未插入"
	case "330":
		return "短信中心号码缺失或错误"
	case "331":
		return "无网络服务"
	case "332":
		return "网络超时"
	case "341", "342":
		return "SIM 短信存储满或忙"
	case "350":
		return "提交被网络/运营商拒绝,请核实 SIM 卡短信业务是否开通(物联卡常被限制),或稍后重试"
	}
	return ""
}

// smsProtocolLinePrefixes 是短信列表组合命令应答中可能夹带的协议行前缀。
// 文本模式收集正文时只跳过这些已知协议行;其余以 "+" 开头的行
// (如以国际区号开头的正文 "+1 2345...") 必须保留为正文。
var smsProtocolLinePrefixes = []string{
	"+CMGL:", "+CMGR:", "+CMGS:", "+CMGD:", "+CSCA:", "+CPMS:",
	"+CSMS:", "+CSDH:", "+CNMI:", "+CMGF:",
	"+CME ERROR:", "+CMS ERROR:",
}

func isSMSProtocolLine(line string) bool {
	up := strings.ToUpper(strings.TrimSpace(line))
	for _, prefix := range smsProtocolLinePrefixes {
		if strings.HasPrefix(up, prefix) {
			return true
		}
	}
	return false
}

func parseSMSServiceCenters(raw string) ([]string, []string) {
	serviceCenters := []string{}
	lines := strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "+CSCA:") {
			parts := csvFields(strings.TrimPrefix(line, "+CSCA:"))
			if len(parts) > 0 {
				serviceCenters = append(serviceCenters, decodeMaybeUCS2(parts[0]))
			}
		}
	}
	return serviceCenters, nil
}
func parseSMSListAT(raw string, storage string) map[string]any {
	if storage != "SM" {
		storage = "ME"
	}
	entries := []map[string]any{}
	lines := strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n")
	serviceCenters := []string{}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "+CSCA:") {
			parts := csvFields(strings.TrimPrefix(line, "+CSCA:"))
			if len(parts) > 0 {
				serviceCenters = append(serviceCenters, decodeMaybeUCS2(parts[0]))
			}
		}
		if !strings.HasPrefix(line, "+CMGL:") {
			continue
		}
		parts := csvFields(strings.TrimPrefix(line, "+CMGL:"))
		if len(parts) < 1 {
			continue
		}
		idx, _ := strconv.Atoi(parts[0])
		sender := ""
		if len(parts) > 2 {
			sender = decodeMaybeUCS2(parts[2])
		}
		date := ""
		if len(parts) > 4 {
			date = parts[4]
			// 文本模式时间戳 yy/mm/dd,hh:mm:ss±zz 未加引号时会被 csvFields
			// 按逗号拆成两段;仅当 parts[4] 是纯日期形状时拼回,
			// 不影响 PDU 模式(头部无日期)与其它列的解析。
			if len(parts) > 5 && regexp.MustCompile(`^\d{2}/\d{2}/\d{2}$`).MatchString(date) {
				date = date + "," + parts[5]
			}
		}
		if date == "" && len(parts) == 4 {
			// 回退只服务文本模式四字段头(idx,stat,sender,date),且仅当 parts[3]
			// 呈日期形状(与上方未加引号拆分校验同一形状)时采信。回归背景:
			// 1) csvFields 改为保留空字段后,状态报告/已发送条目的四字段空 alpha 头
			//    `+CMGL: 3,2,,47` 会切出 parts[3]="47"(PDU 长度),无条件回退会把
			//    长度当日期,绕过下方 `date == ""` 闸门,把非 DELIVER PDU 解码成
			//    乱码假短信;
			// 2) ME 电话簿对号码存有姓名(非空 alpha)且条目是状态报告/已发送
			//    (非 DELIVER PDU)时,头为 `+CMGL: 3,2,<alpha>,<len>`,
			//    旧条件 `parts[2] != ""` 同样会把 parts[3](PDU 长度)当日期,
			//    产生假短信并被 webhook 推送。
			// PDU 模式(列表命令固定 +CMGF=0)的四字段头没有日期字段,
			// parts[3] 呈日期形状是确认文本模式语义的唯一依据;非 DELIVER 必须被跳过。
			if regexp.MustCompile(`^\d{2}/\d{2}/\d{2}`).MatchString(parts[3]) {
				date = parts[3]
			}
		}
		bodyLines := []string{}
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if strings.HasPrefix(next, "+CMGL:") {
				break
			}
			if next == "" || next == "OK" || isSMSProtocolLine(next) || strings.HasPrefix(strings.ToUpper(next), "AT+") {
				continue
			}
			bodyLines = append(bodyLines, next)
			i = j
		}
		if len(bodyLines) > 0 && isHexLine(bodyLines[0]) {
			pdu, ok := parseSMSDeliverPDU(bodyLines[0])
			if ok {
				if pdu.ServiceCenter != "" {
					serviceCenters = append(serviceCenters, pdu.ServiceCenter)
				}
				entry := map[string]any{"sender": pdu.Sender, "date": pdu.Date, "text": pdu.Text, "indices": []int{idx}, "storage": storage}
				if pdu.ConcatRef != "" {
					entry["concatRef"] = pdu.Sender + ":" + pdu.ConcatRef
					entry["concatTotal"] = pdu.ConcatTotal
					entry["concatSeq"] = pdu.ConcatSeq
				}
				entries = append(entries, entry)
				continue
			}
			// PDU 模式(列表头无日期)下解析失败通常是已发送/状态报告等
			// 非 DELIVER 类型,直接跳过该条目;不能回退做 UCS2 猜测解码,
			// 否则会把整条 PDU 十六进制解成乱码假短信(发送者显示为 PDU 长度)。
			// 文本模式的十六进制正文(头部带日期)仍按原有逻辑解码。
			if date == "" {
				continue
			}
		}
		text := decodeSMSBodyLines(bodyLines)
		entries = append(entries, map[string]any{"sender": sender, "date": date, "text": text, "indices": []int{idx}, "storage": storage})
	}
	messages := mergeSMSFragments(entries)
	for _, message := range messages {
		if text, ok := message["text"].(string); ok {
			normalized := normalizeSMSLineBreaks(text)
			message["text"] = normalized
			message["textLines"] = splitSMSDisplayLines(normalized)
		}
	}
	return map[string]any{"messages": messages, "serviceCenters": serviceCenters}
}

func decodeSMSBodyLines(lines []string) string {
	compact := strings.Join(lines, "")
	decoded := decodeMaybeUCS2(compact)
	if decoded != compact {
		return normalizeSMSLineBreaks(decoded)
	}
	return normalizeSMSLineBreaks(strings.Join(lines, "\n"))
}

func normalizeSMSLineBreaks(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\v", "\n")
	text = strings.ReplaceAll(text, "\f", "\n")
	text = strings.ReplaceAll(text, "\u0085", "\n")
	text = strings.ReplaceAll(text, "\u2028", "\n")
	text = strings.ReplaceAll(text, "\u2029", "\n")
	text = strings.ReplaceAll(text, `\r\n`, "\n")
	text = strings.ReplaceAll(text, `\n`, "\n")
	text = strings.ReplaceAll(text, `\r`, "\n")
	return text
}

func splitSMSDisplayLines(text string) []string {
	return strings.Split(normalizeSMSLineBreaks(text), "\n")
}

func smsConcatRef(entry map[string]any) string {
	if value, ok := entry["concatRef"].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func mergeSMSFragments(entries []map[string]any) []map[string]any {
	merged := []map[string]any{}
	concatGroups := map[string][]map[string]any{}
	concatFirstIndex := map[string]int{}
	// 运营商重复投递会让同一条长短信的两组分片同时驻留存储,
	// 合并前按 (concatRef, concatSeq) 去重(同 seq 保留先到者),
	// 否则正文会按分片数翻倍。
	concatSeenSeq := map[string]map[int]bool{}
	for i, entry := range entries {
		ref := smsConcatRef(entry)
		if ref == "" {
			continue
		}
		if seq := toInt(fmt.Sprint(entry["concatSeq"])); seq > 0 {
			if concatSeenSeq[ref] == nil {
				concatSeenSeq[ref] = map[int]bool{}
			}
			if concatSeenSeq[ref][seq] {
				continue
			}
			concatSeenSeq[ref][seq] = true
		}
		if _, ok := concatGroups[ref]; !ok {
			concatFirstIndex[ref] = i
		}
		concatGroups[ref] = append(concatGroups[ref], entry)
	}

	group := []map[string]any{}
	flush := func() {
		if len(group) == 0 {
			return
		}
		merged = append(merged, buildSMSFragmentGroup(group))
		group = nil
	}
	for i, entry := range entries {
		ref := smsConcatRef(entry)
		if ref != "" {
			if concatFirstIndex[ref] == i {
				flush()
				merged = append(merged, buildSMSFragmentGroup(concatGroups[ref]))
			}
			continue
		}
		if len(group) == 0 || isSameSMSFragmentGroup(group[len(group)-1], entry) {
			group = append(group, entry)
			continue
		}
		flush()
		group = append(group, entry)
	}
	flush()
	return merged
}

func buildSMSFragmentGroup(group []map[string]any) map[string]any {
	if len(group) == 1 {
		return group[0]
	}
	ordered := orderSMSFragmentsByIndex(group)
	texts := make([]string, 0, len(ordered))
	indices := []int{}
	for _, item := range ordered {
		texts = append(texts, fmt.Sprint(item["text"]))
		indices = append(indices, toIntSlice(item["indices"])...)
	}
	sort.Ints(indices)
	// 元信息取首片(concatSeq==1 或最小有效 seq):分片乱序入库时
	// 存储顺序第一片未必是首片,直接取 group[0] 会导致显示时间不准。
	meta := smsFragmentGroupMeta(group)
	message := map[string]any{
		"sender":  meta["sender"],
		"date":    meta["date"],
		"text":    strings.Join(texts, ""),
		"indices": indices,
		"storage": meta["storage"],
	}
	if total := toInt(fmt.Sprint(meta["concatTotal"])); total > 1 {
		message["concatTotal"] = total
	}
	if concatRef := smsConcatRef(meta); concatRef != "" {
		message["concatRef"] = concatRef
	}
	return message
}

// smsFragmentGroupMeta 返回承载合并消息元信息(发送者/时间等)的分片:
// 优先取 concatSeq==1 的首片;无 seq==1 时取排序后最小有效 seq 的分片。
func smsFragmentGroupMeta(group []map[string]any) map[string]any {
	for _, item := range group {
		if toInt(fmt.Sprint(item["concatSeq"])) == 1 {
			return item
		}
	}
	return orderSMSFragmentsByIndex(group)[0]
}

func isSameSMSFragmentGroup(a, b map[string]any) bool {
	senderA := strings.TrimSpace(fmt.Sprint(a["sender"]))
	senderB := strings.TrimSpace(fmt.Sprint(b["sender"]))
	if senderA == "" || senderB == "" || senderA != senderB {
		return false
	}
	concatA := smsConcatRef(a)
	concatB := smsConcatRef(b)
	if concatA != "" || concatB != "" {
		return concatA != "" && concatA == concatB
	}
	if !isSMSDateWithinSeconds(fmt.Sprint(a["date"]), fmt.Sprint(b["date"]), 5) {
		return false
	}
	prevIdx := lastSMSIndex(a["indices"])
	nextIdx := firstSMSIndex(b["indices"])
	return prevIdx >= 0 && nextIdx == prevIdx+1
}

func orderSMSFragmentsByIndex(group []map[string]any) []map[string]any {
	ordered := append([]map[string]any(nil), group...)
	sort.SliceStable(ordered, func(i, j int) bool {
		seqI := toInt(fmt.Sprint(ordered[i]["concatSeq"]))
		seqJ := toInt(fmt.Sprint(ordered[j]["concatSeq"]))
		if seqI > 0 || seqJ > 0 {
			return seqI < seqJ
		}
		return firstSMSIndex(ordered[i]["indices"]) < firstSMSIndex(ordered[j]["indices"])
	})
	return ordered
}

func isSMSDateWithinSeconds(a, b string, seconds int) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	ta, okA := parseSMSDateTime(a)
	tb, okB := parseSMSDateTime(b)
	if !okA || !okB {
		return false
	}
	diff := ta.Sub(tb)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Duration(seconds)*time.Second
}

func parseSMSDateTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if len(value) < len("06/01/02,15:04:05") {
		return time.Time{}, false
	}
	base := value[:len("06/01/02,15:04:05")]
	parsed, err := time.ParseInLocation("06/01/02,15:04:05", base, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func appendIntSlices(a, b any) []int {
	result := toIntSlice(a)
	return append(result, toIntSlice(b)...)
}

func firstSMSIndex(value any) int {
	values := toIntSlice(value)
	if len(values) == 0 {
		return -1
	}
	return values[0]
}

func lastSMSIndex(value any) int {
	values := toIntSlice(value)
	if len(values) == 0 {
		return -1
	}
	return values[len(values)-1]
}

func toIntSlice(value any) []int {
	switch v := value.(type) {
	case []int:
		out := make([]int, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([]int, 0, len(v))
		for _, item := range v {
			out = append(out, toInt(fmt.Sprint(item)))
		}
		return out
	default:
		return nil
	}
}
