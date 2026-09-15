package main

import (
	"regexp"
	"strings"
)

func sanitizeATCommand(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	return strings.TrimSpace(value)
}

func normalizeATCommandParts(rawParts []string) []string {
	commands := make([]string, 0, len(rawParts))
	for i, part := range rawParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		upperPart := strings.ToUpper(part)
		if i > 0 && !strings.HasPrefix(upperPart, "AT") {
			if strings.HasPrefix(part, "+") || strings.HasPrefix(part, "&") {
				part = "AT" + part
			} else {
				part = "AT+" + part
			}
		}
		commands = append(commands, part)
	}
	return commands
}

func splitATCommandParts(command string) []string {
	parts := make([]string, 0, 4)
	var current strings.Builder
	inQuote := false
	for _, r := range command {
		switch r {
		case '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case ';':
			if inQuote {
				current.WriteRune(r)
				continue
			}
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	parts = append(parts, current.String())
	return parts
}

func stripNonDigits(value string) string {
	re := regexp.MustCompile(`[^0-9]`)
	return re.ReplaceAllString(value, "")
}

// atPayloadEchoMarker derives the echoed command line used to correlate a
// response with its request. Only the first line of the payload is used;
// non-AT payloads (or empty ones) have no usable echo marker.
func atPayloadEchoMarker(payload string) string {
	line := payload
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	line = strings.ToUpper(strings.TrimSpace(line))
	if line == "" || !strings.HasPrefix(line, "AT") {
		return ""
	}
	return line
}

// trimOutputBeforeEchoMarker drops everything captured before the echoed
// command line, so stale output of a previous transaction cannot pollute the
// parsed response. Without a marker (or when the echo is missing) the output
// is returned unchanged.
func trimOutputBeforeEchoMarker(out, marker string) string {
	if marker == "" {
		return out
	}
	idx := findEchoMarker([]byte(out), marker)
	if idx < 0 {
		return out
	}
	return out[idx:]
}

// findEchoMarker locates the echoed command inside raw device output. The
// match is case-insensitive and skips carriage returns, so payloads echoed
// with \r\n endings still match. The match must start at a line boundary
// (buffer start or right after a newline): the module echoes input line by
// line, and an unanchored substring match would mislocate short commands
// (AT/ATI) inside longer words of stale data (e.g. "ST"AT"US"), truncating
// the buffer mid-word and treating stale bytes as this transaction's
// response. Returns the raw index of the first marker byte, or -1 when
// absent.
func findEchoMarker(buf []byte, marker string) int {
	marker = strings.ToUpper(strings.ReplaceAll(marker, "\r", ""))
	if marker == "" {
		return -1
	}
	// 归一化(大写、去 \r)后用标准库定位,再把归一化下标映射回原始下标;
	// 避免手写匹配器在重叠前缀场景误判回显缺失。
	normalized := make([]byte, 0, len(buf))
	rawIndexOf := make([]int, 0, len(buf))
	for i, c := range buf {
		if c == '\r' {
			continue
		}
		normalized = append(normalized, upperASCIIByte(c))
		rawIndexOf = append(rawIndexOf, i)
	}
	norm := string(normalized)
	for search := 0; search+len(marker) <= len(norm); {
		pos := strings.Index(norm[search:], marker)
		if pos < 0 {
			return -1
		}
		pos += search
		if pos == 0 || norm[pos-1] == '\n' {
			return rawIndexOf[pos]
		}
		search = pos + 1
	}
	return -1
}

func upperASCIIByte(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}
