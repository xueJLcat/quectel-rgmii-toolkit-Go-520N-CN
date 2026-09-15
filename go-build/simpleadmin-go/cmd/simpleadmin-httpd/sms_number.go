package main

import (
	"fmt"
	"regexp"
	"strings"
)

var smsNumberCleanupRE = regexp.MustCompile(`[^0-9+]`)

// smsNumberLetterRE 命中输入中的字母:vanity 号码(如 +1-800-FLOWERS)
// 静默剔除字母后会变成错误号码(+1800),必须显式报错。
var smsNumberLetterRE = regexp.MustCompile(`[A-Za-z]`)

func normalizeSMSNumber(number string, cimiRaw string) (string, error) {
	trimmed := strings.TrimSpace(number)
	if smsNumberLetterRE.MatchString(trimmed) {
		return "", fmt.Errorf("号码包含字母，无法发送短信: %s", trimmed)
	}
	clean := smsNumberCleanupRE.ReplaceAllString(trimmed, "")
	if strings.HasPrefix(clean, "00") {
		digits := stripNonDigits(clean[2:])
		if digits == "" {
			// "00"/"00-" 等退化输入不得拼出裸 "+",按空号码拦截。
			return "", nil
		}
		return "+" + digits, nil
	}
	if strings.HasPrefix(clean, "+") {
		digits := stripNonDigits(clean)
		if digits == "" {
			return "", nil
		}
		return "+" + digits, nil
	}
	clean = stripNonDigits(clean)
	if clean == "" {
		return "", nil
	}
	// 客服/应急短号(10001、10086、95588、110 等)不加国家码,
	// 加前缀后运营商无法路由,会返回 +CMS ERROR。
	if len(clean) <= 6 {
		return clean, nil
	}
	// 国内长途/本地拨号带一个前导 0(如 010...):归一化为国际格式时
	// 剥离一个前导 0,避免拼出 +86010... 这类不可路由号码。
	if strings.HasPrefix(clean, "0") {
		clean = clean[1:]
	}

	imsi := parseCIMIResponseIMSI(cimiRaw)
	if len(imsi) < 3 {
		if isCIMINotReady(cimiRaw) {
			return "", fmt.Errorf("模块未就绪，请稍后重试")
		}
		return "", fmt.Errorf("AT+CIMI 未返回有效 IMSI，无法自动添加国家/地区代码")
	}
	mcc := imsi[:3]
	callingCode := mccToCallingCode(mcc)
	if callingCode == "" {
		return "", fmt.Errorf("AT+CIMI 返回的 MCC %s 未配置国家/地区代码", mcc)
	}
	return "+" + callingCode + clean, nil
}

// isCIMINotReady 判定 AT+CIMI 应答是否属于"模块未就绪"而非真无 SIM:
// 开机保护期/模块重启保护期的后台占位文本、空应答、纯运行器错误文本
// 均视为未就绪;含 SIM 缺失标志(+CME ERROR: 10、SIM NOT INSERTED 等)
// 时按真无 SIM 处理,保留原文案分支。
func isCIMINotReady(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return true
	}
	upper := strings.ToUpper(trimmed)
	if isSIMAbsentEvidence(upper) {
		return false
	}
	if strings.Contains(trimmed, atCachePendingText) {
		return true
	}
	// 过滤掉占位/运行器错误后无任何模块数据行,视为未就绪。
	return len(atLines(raw)) == 0
}

func parseCIMIResponseIMSI(raw string) string {
	for _, line := range atLines(raw) {
		value := stripNonDigits(line)
		if regexp.MustCompile(`^\d{10,20}$`).MatchString(value) {
			return value
		}
	}
	return ""
}

func mccToCallingCode(mcc string) string {
	switch strings.TrimSpace(mcc) {
	case "302":
		return "1"
	case "310", "311", "312", "313", "314", "315", "316":
		return "1"
	case "330", "332", "338", "342", "344", "346", "348", "350", "352":
		return "1"
	case "354", "356", "358", "360", "364", "365", "366":
		return "1"
	// MCC 362(荷属安的列斯:库拉索/博内尔 +599,圣马丁 +1-721)横跨两个
	// 编号计划,自动加 +1 对两地都无法路由(圣马丁还需 721 区号)。
	// 故不纳入映射,落入"未配置国家/地区代码"分支提示用户用国际格式输入。
	case "370", "374", "376":
		return "1"
	case "334":
		return "52"
	case "368":
		return "53"
	case "702":
		return "501"
	case "704":
		return "502"
	case "706":
		return "503"
	case "708":
		return "504"
	case "710":
		return "505"
	case "712":
		return "506"
	case "714":
		return "507"
	case "716":
		return "51"
	case "722":
		return "54"
	case "724":
		return "55"
	case "730":
		return "56"
	case "732":
		return "57"
	case "734":
		return "58"
	case "736":
		return "591"
	case "738":
		return "592"
	case "740":
		return "593"
	case "744":
		return "595"
	case "746":
		return "597"
	case "748":
		return "598"
	case "372":
		return "509"
	case "340":
		return "590"
	case "363":
		return "297"
	case "202":
		return "30"
	case "204":
		return "31"
	case "206":
		return "32"
	case "208":
		return "33"
	case "212":
		return "377"
	case "213":
		return "376"
	case "214":
		return "34"
	case "216":
		return "36"
	case "218":
		return "387"
	case "219":
		return "385"
	case "220":
		return "381"
	case "222", "225":
		return "39"
	case "226":
		return "40"
	case "228":
		return "41"
	case "230":
		return "420"
	case "231":
		return "421"
	case "232":
		return "43"
	case "234", "235":
		return "44"
	case "238":
		return "45"
	case "240":
		return "46"
	case "242":
		return "47"
	case "244":
		return "358"
	case "246":
		return "370"
	case "247":
		return "371"
	case "248":
		return "372"
	case "250":
		return "7"
	case "255":
		return "380"
	case "257":
		return "375"
	case "259":
		return "373"
	case "260":
		return "48"
	case "262":
		return "49"
	case "266":
		return "350"
	case "268":
		return "351"
	case "270":
		return "352"
	case "272":
		return "353"
	case "274":
		return "354"
	case "276":
		return "355"
	case "278":
		return "356"
	case "280":
		return "357"
	case "282":
		return "995"
	case "283":
		return "374"
	case "284":
		return "359"
	case "286":
		return "90"
	case "288":
		return "298"
	case "290":
		return "299"
	case "292":
		return "378"
	case "293":
		return "386"
	case "294":
		return "389"
	case "295":
		return "423"
	case "297":
		return "382"
	case "400":
		return "994"
	case "401":
		return "7"
	case "402":
		return "975"
	case "404", "405", "406":
		return "91"
	case "410":
		return "92"
	case "412":
		return "93"
	case "413":
		return "94"
	case "414":
		return "95"
	case "415":
		return "961"
	case "416":
		return "962"
	case "417":
		return "963"
	case "418":
		return "964"
	case "419":
		return "965"
	case "420":
		return "966"
	case "421":
		return "967"
	case "422":
		return "968"
	case "424", "430", "431":
		return "971"
	case "425":
		return "972"
	case "426":
		return "973"
	case "427":
		return "974"
	case "428":
		return "976"
	case "429":
		return "977"
	case "432":
		return "98"
	case "434":
		return "998"
	case "436":
		return "992"
	case "437":
		return "996"
	case "438":
		return "993"
	case "440", "441":
		return "81"
	case "450":
		return "82"
	case "452":
		return "84"
	case "454":
		return "852"
	case "455":
		return "853"
	case "456":
		return "855"
	case "457":
		return "856"
	case "460", "461":
		return "86"
	case "466":
		return "886"
	case "467":
		return "850"
	case "470":
		return "880"
	case "472":
		return "960"
	case "502":
		return "60"
	case "505":
		return "61"
	case "510":
		return "62"
	case "514":
		return "670"
	case "515":
		return "63"
	case "520":
		return "66"
	case "525":
		return "65"
	case "528":
		return "673"
	case "530":
		return "64"
	case "536":
		return "674"
	case "537":
		return "675"
	case "539":
		return "676"
	case "540":
		return "677"
	case "541":
		return "678"
	case "542":
		return "679"
	case "545":
		return "686"
	case "546":
		return "687"
	case "547":
		return "689"
	case "548":
		return "682"
	case "549":
		return "685"
	case "550":
		return "691"
	case "551":
		return "692"
	case "552":
		return "680"
	case "602":
		return "20"
	case "603":
		return "213"
	case "604":
		return "212"
	case "605":
		return "216"
	case "606":
		return "218"
	case "607":
		return "220"
	case "608":
		return "221"
	case "609":
		return "222"
	case "610":
		return "223"
	case "611":
		return "224"
	case "612":
		return "225"
	case "613":
		return "226"
	case "614":
		return "227"
	case "615":
		return "228"
	case "616":
		return "229"
	case "617":
		return "230"
	case "618":
		return "231"
	case "619":
		return "232"
	case "620":
		return "233"
	case "621":
		return "234"
	case "622":
		return "235"
	case "623":
		return "236"
	case "624":
		return "237"
	case "625":
		return "238"
	case "626":
		return "239"
	case "627":
		return "240"
	case "628":
		return "241"
	case "629":
		return "242"
	case "630":
		return "243"
	case "631":
		return "244"
	case "632":
		return "245"
	case "633":
		return "248"
	case "634":
		return "249"
	case "635":
		return "250"
	case "636":
		return "251"
	case "637":
		return "252"
	case "638":
		return "253"
	case "639":
		return "254"
	case "640":
		return "255"
	case "641":
		return "256"
	case "642":
		return "257"
	case "643":
		return "258"
	case "645":
		return "260"
	case "646":
		return "261"
	case "647":
		return "262"
	case "648":
		return "263"
	case "649":
		return "264"
	case "650":
		return "265"
	case "651":
		return "266"
	case "652":
		return "267"
	case "653":
		return "268"
	case "654":
		return "269"
	case "655":
		return "27"
	case "657":
		return "291"
	case "659":
		return "211"
	default:
		return ""
	}
}
