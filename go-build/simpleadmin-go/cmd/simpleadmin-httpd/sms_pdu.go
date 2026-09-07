package main

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

type smsPDUDecoded struct {
	ServiceCenter string
	Sender        string
	Date          string
	Text          string
	ConcatRef     string
	ConcatTotal   int
	ConcatSeq     int
}

// smsVendorSegmentUnits 是厂商文本模式单段正文的 UTF-16 码元上限。
// 文本模式无 UDH 开销,长短信重组由固件按 uid/seq/total 自行完成,
// 单段可承载完整 70 码元(140 字节)。
const smsVendorSegmentUnits = 70

// splitSMSVendorSegments 按 UTF-16 码元切分正文(每段 70 码元),
// 返回每段的 UCS2 大端十六进制串;代理对不被拆到两段。
// UCS2 分段必须按码元计算:emoji 等增补字符占 2 个码元,按 rune 切分
// 会让单段超出上限。
func splitSMSVendorSegments(message string) []string {
	segments := splitSMSUTF16Units(message, smsVendorSegmentUnits, smsVendorSegmentUnits)
	out := make([]string, 0, len(segments))
	for _, units := range segments {
		out = append(out, encodeUCS2Units(units))
	}
	return out
}

// buildSMSVendorSendCommand 拼装电信定制固件的文本模式发送命令(实机验证流程,
// 见 03-at-interface.md 与历史工具 cpe_sms.sh;标准 PDU 模式在本固件被拒绝,
// 返回 +CMS ERROR: 350)。初始化序列必须随每次发送执行:
//   - +CSMS=1/+CMGF=1/+CSCS="UCS2" 缺一即命令形态错误;
//   - +CSMP=17,167,0,8 为文本模式提交参数;
//   - +CMGS 必须携带厂商三元组 "<号码的UCS2十六进制>",uid,seq,total,
//     缺三元组报 CMS 305,号码不做 UCS2 编码报 304/ERROR。
//
// 报文体在 "> " 提示符后写入(正文 UCS2 十六进制 + Ctrl-Z,见 runSMSTransaction)。
// 长短信各段共用同一 uid,seq 从 1 递增,固件据此重组。
func buildSMSVendorSendCommand(number string, uid, seq, total int) string {
	return fmt.Sprintf(`AT+CMEE=1;+CSMS=1;+CMGF=1;+CSCS="UCS2";+CSDH=0;+CSMP=17,167,0,8;+CNMI=2,1,0,0,0;+CPMS="ME","ME","ME";+CMGS="%s",%d,%d,%d`,
		encodeUCS2(number), uid, seq, total)
}

// splitSMSUTF16Units 按 UTF-16 码元切分短信正文。总长不超过 singleLimit
// 时整体作为单段;否则按 multiLimit 分段,且避免把代理对拆到两段。
func splitSMSUTF16Units(message string, singleLimit, multiLimit int) [][]uint16 {
	units := utf16.Encode([]rune(message))
	if len(units) == 0 {
		return nil
	}
	if singleLimit <= 0 {
		singleLimit = 70
	}
	if multiLimit <= 0 {
		multiLimit = 67
	}
	if len(units) <= singleLimit {
		return [][]uint16{units}
	}
	segments := [][]uint16{}
	for len(units) > 0 {
		end := multiLimit
		if len(units) < end {
			end = len(units)
		}
		// 不把高代理项单独留在段尾,避免产生孤立代理码元。
		if end < len(units) && end > 0 && units[end-1] >= 0xD800 && units[end-1] <= 0xDBFF {
			end--
		}
		if end <= 0 {
			// 退化分段参数(如 multiLimit==1 且段首为高代理项)下回退会
			// 产生空段且不消耗输入,形成死循环;兜底保证每段至少前进一个码元。
			end = 1
		}
		segments = append(segments, units[:end])
		units = units[end:]
	}
	return segments
}

// encodeUCS2Units 把 UTF-16 码元序列编码为 UCS2 十六进制串。
func encodeUCS2Units(units []uint16) string {
	var b strings.Builder
	for _, unit := range units {
		fmt.Fprintf(&b, "%04X", unit)
	}
	return b.String()
}

func parseSMSDeliverPDU(pdu string) (smsPDUDecoded, bool) {
	data, ok := cleanHexBytes(pdu)
	if !ok || len(data) < 2 {
		return smsPDUDecoded{}, false
	}
	pos := 0
	smscLen := int(data[pos])
	pos++
	decoded := smsPDUDecoded{}
	if smscLen > 0 {
		if pos+smscLen > len(data) || smscLen < 1 {
			return smsPDUDecoded{}, false
		}
		toa := data[pos]
		raw := data[pos+1 : pos+smscLen]
		decoded.ServiceCenter = decodePDUAddress((smscLen-1)*2, toa, raw)
		pos += smscLen
	}
	if pos >= len(data) {
		return smsPDUDecoded{}, false
	}
	firstOctet := data[pos]
	pos++
	if firstOctet&0x03 != 0x00 {
		return smsPDUDecoded{}, false
	}
	if pos+2 > len(data) {
		return smsPDUDecoded{}, false
	}
	addrDigits := int(data[pos])
	pos++
	addrTOA := data[pos]
	pos++
	addrOctets := (addrDigits + 1) / 2
	if pos+addrOctets+10 > len(data) {
		return smsPDUDecoded{}, false
	}
	decoded.Sender = decodePDUAddress(addrDigits, addrTOA, data[pos:pos+addrOctets])
	pos += addrOctets
	pos++ // PID
	dcs := data[pos]
	pos++
	decoded.Date = decodePDUTimestamp(data[pos : pos+7])
	pos += 7
	udl := int(data[pos])
	pos++
	if pos > len(data) {
		return smsPDUDecoded{}, false
	}
	userData := data[pos:]
	if len(userData) == 0 && udl > 0 {
		return smsPDUDecoded{}, false
	}
	decoded.Text, decoded.ConcatRef, decoded.ConcatTotal, decoded.ConcatSeq = decodePDUUserData(firstOctet, dcs, udl, userData)
	return decoded, true
}

func decodePDUUserData(firstOctet byte, dcs byte, udl int, userData []byte) (string, string, int, int) {
	udhi := firstOctet&0x40 != 0
	content := userData
	udhSeptets := 0
	udhOctets := 0
	concatRef := ""
	concatTotal := 0
	concatSeq := 0
	if udhi && len(userData) > 0 {
		udhl := int(userData[0])
		if len(userData) >= udhl+1 {
			udh := userData[1 : udhl+1]
			concatRef, concatTotal, concatSeq = parseSMSConcatUDH(udh)
			content = userData[udhl+1:]
			udhSeptets = ((udhl+1)*8 + 6) / 7
			udhOctets = udhl + 1
		}
	}
	// UDL 计数含 UDH 的全部用户数据八位组:按声明截断 content,畸形/尾部
	// 带杂质的 PDU 多余字节不参与 ucs2/8bit 解码。
	if maxContent := udl - udhOctets; maxContent >= 0 && len(content) > maxContent {
		content = content[:maxContent]
	}
	switch smsDCSAlphabet(dcs) {
	case "ucs2":
		return decodeUCS2Bytes(content), concatRef, concatTotal, concatSeq
	case "gsm7":
		septets := udl
		if udhi {
			septets -= udhSeptets
			if septets < 0 {
				septets = 0
			}
			// 截断 PDU(AT 应答半截且截断后仍为合法偶数 hex 时头部边界
			// 检查可通过)里 UDL 声明的 septet 数可能超出实际数据:越界位
			// 按 0 继续拼字会伪造 '@' 填充,钳制到实际数据可支撑的数量。
			if avail := len(userData)*8/7 - udhSeptets; septets > avail {
				septets = avail
				if septets < 0 {
					septets = 0
				}
			}
			return decodeGSM7(userData, septets, udhSeptets), concatRef, concatTotal, concatSeq
		}
		if avail := len(content) * 8 / 7; septets > avail {
			septets = avail
		}
		return decodeGSM7(content, septets, 0), concatRef, concatTotal, concatSeq
	case "8bit":
		return strings.ToUpper(hex.EncodeToString(content)), concatRef, concatTotal, concatSeq
	default:
		return decodeMaybeUCS2(strings.ToUpper(hex.EncodeToString(content))), concatRef, concatTotal, concatSeq
	}
}

func parseSMSConcatUDH(udh []byte) (string, int, int) {
	for i := 0; i+1 < len(udh); {
		iei := udh[i]
		l := int(udh[i+1])
		i += 2
		if i+l > len(udh) {
			break
		}
		data := udh[i : i+l]
		switch {
		case iei == 0x00 && l == 3:
			return fmt.Sprintf("8:%02X", data[0]), int(data[1]), int(data[2])
		case iei == 0x08 && l == 4:
			return fmt.Sprintf("16:%02X%02X", data[0], data[1]), int(data[2]), int(data[3])
		}
		i += l
	}
	return "", 0, 0
}

func smsDCSAlphabet(dcs byte) string {
	switch dcs & 0x0C {
	case 0x08:
		return "ucs2"
	case 0x04:
		return "8bit"
	default:
		return "gsm7"
	}
}

func encodeUCS2(value string) string {
	units := utf16.Encode([]rune(value))
	var b strings.Builder
	for _, unit := range units {
		fmt.Fprintf(&b, "%04X", unit)
	}
	return b.String()
}

func decodeUCS2Bytes(data []byte) string {
	if len(data) < 2 {
		return ""
	}
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
	}
	return string(utf16.Decode(units))
}

func encodePDUPhoneNumber(digits string) string {
	if len(digits)%2 != 0 {
		digits += "F"
	}
	var b strings.Builder
	for i := 0; i+1 < len(digits); i += 2 {
		b.WriteByte(digits[i+1])
		b.WriteByte(digits[i])
	}
	return strings.ToUpper(b.String())
}

func decodePDUAddress(digitCount int, toa byte, raw []byte) string {
	// 类型号码段(TON, toa 的 bit6-4)决定地址编码方式:
	//   0x5 字母型(alphanumeric)发送者(如 "MyBank")以 GSM 7bit 压缩存放,
	//       不是 BCD 数字;旧实现 toa&0x90==0x90 同时命中字母型,按数字半八度
	//       解码得到乱码并误加 "+" 前缀。字符数 = digitCount*4/7(03.40 §9.1.2.5)。
	//   0x1 国际号码才是 BCD + "+" 前缀;其余类型按 BCD 但不加 "+"。
	typeOfNumber := (toa >> 4) & 0x07
	if typeOfNumber == 0x05 {
		septets := (digitCount * 4) / 7
		return strings.TrimRight(decodeGSM7(raw, septets, 0), "\x00\r\n ")
	}
	digits := decodePDUSemiOctets(raw, digitCount)
	if digits == "" {
		return ""
	}
	if typeOfNumber == 0x01 && !strings.HasPrefix(digits, "+") {
		return "+" + digits
	}
	return digits
}

func decodePDUSemiOctets(raw []byte, digitCount int) string {
	var b strings.Builder
	for _, value := range raw {
		if ch := pduSemiOctetChar(value & 0x0F); ch != 0 {
			b.WriteByte(ch)
		}
		if ch := pduSemiOctetChar((value >> 4) & 0x0F); ch != 0 {
			b.WriteByte(ch)
		}
	}
	out := b.String()
	if digitCount > 0 && len(out) > digitCount {
		out = out[:digitCount]
	}
	return out
}

// pduSemiOctetChar 按 TS 23.040 §9.1.2.5 映射地址半八度:0-9 为数字,
// 10='*'、11='#'、12='a'、13='b'、14='c',15=F 填充符(返回 0 丢弃)。
// 旧实现把 10-14 与填充符同等静默丢弃,*123# 类业务号码会缺字符,
// 且字符串变短后 digitCount 截断保护错位。
func pduSemiOctetChar(v byte) byte {
	switch {
	case v <= 9:
		return '0' + v
	case v == 10:
		return '*'
	case v == 11:
		return '#'
	case v == 12:
		return 'a'
	case v == 13:
		return 'b'
	case v == 14:
		return 'c'
	default:
		return 0
	}
}

func decodePDUTimestamp(data []byte) string {
	if len(data) < 7 {
		return ""
	}
	parts := make([]int, 6)
	for i := 0; i < 6; i++ {
		parts[i] = pduSemiOctetInt(data[i])
	}
	tzByte := data[6]
	sign := "+"
	if tzByte&0x08 != 0 {
		sign = "-"
		tzByte &^= 0x08
	}
	// 时区字段按 15 分钟刻度计数,直接 %02d 输出刻度数(北京显示 +32)
	// 会误导;换算为时:分输出,如 32 刻度=08:00 → +08:00,
	// 22 刻度=05:30 → +05:30(半小时时区也能正确表示)。
	totalMinutes := pduSemiOctetInt(tzByte) * 15
	return fmt.Sprintf("%02d/%02d/%02d,%02d:%02d:%02d%s%02d:%02d", parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], sign, totalMinutes/60, totalMinutes%60)
}

func pduSemiOctetInt(value byte) int {
	return int(value&0x0F)*10 + int((value>>4)&0x0F)
}

func cleanHexBytes(value string) ([]byte, bool) {
	clean := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
	if clean == "" || len(clean)%2 != 0 {
		return nil, false
	}
	data, err := hex.DecodeString(clean)
	if err != nil {
		return nil, false
	}
	return data, true
}

func isHexLine(value string) bool {
	clean := strings.ReplaceAll(strings.TrimSpace(value), " ", "")
	if clean == "" || len(clean)%2 != 0 {
		return false
	}
	_, err := strconv.ParseUint(clean[:2], 16, 8)
	if err != nil {
		return false
	}
	_, err = hex.DecodeString(clean)
	return err == nil
}

func decodeGSM7(data []byte, septets int, skipSeptets int) string {
	if septets <= 0 || len(data) == 0 {
		return ""
	}
	var b strings.Builder
	escape := false
	for i := 0; i < septets; i++ {
		bit := (skipSeptets + i) * 7
		value := 0
		for j := 0; j < 7; j++ {
			idx := (bit + j) / 8
			if idx >= len(data) {
				break
			}
			if data[idx]&(1<<uint((bit+j)%8)) != 0 {
				value |= 1 << uint(j)
			}
		}
		if escape {
			b.WriteRune(gsm7ExtRune(byte(value)))
			escape = false
			continue
		}
		if value == 0x1B {
			escape = true
			continue
		}
		b.WriteRune(gsm7Rune(byte(value)))
	}
	return b.String()
}

func gsm7Rune(value byte) rune {
	table := []rune{
		'@', '£', '$', '¥', 'è', 'é', 'ù', 'ì', 'ò', 'Ç', '\n', 'Ø', 'ø', '\r', 'Å', 'å',
		'Δ', '_', 'Φ', 'Γ', 'Λ', 'Ω', 'Π', 'Ψ', 'Σ', 'Θ', 'Ξ', 0, 'Æ', 'æ', 'ß', 'É',
		' ', '!', '"', '#', '¤', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', ';', '<', '=', '>', '?',
		'¡', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O',
		'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 'Ä', 'Ö', 'Ñ', 'Ü', '§',
		'¿', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
		'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 'ä', 'ö', 'ñ', 'ü', 'à',
	}
	if int(value) >= len(table) || table[value] == 0 {
		return ' '
	}
	return table[value]
}

func gsm7ExtRune(value byte) rune {
	switch value {
	case 0x0A:
		return '\f'
	case 0x14:
		return '^'
	case 0x28:
		return '{'
	case 0x29:
		return '}'
	case 0x2F:
		return '\\'
	case 0x3C:
		return '['
	case 0x3D:
		return '~'
	case 0x3E:
		return ']'
	case 0x40:
		return '|'
	case 0x65:
		return '€'
	default:
		return ' '
	}
}

func buildMockSMSDeliverPDU(sender string, text string) string {
	digits := stripNonDigits(sender)
	toa := byte(0x81)
	if strings.HasPrefix(strings.TrimSpace(sender), "+") {
		toa = 0x91
	}
	ud := encodeUCS2(text)
	tpdu := fmt.Sprintf("04%02X%02X%s000862809241030023%02X%s", len(digits), toa, encodePDUPhoneNumber(digits), len(ud)/2, ud)
	return "00" + tpdu
}
