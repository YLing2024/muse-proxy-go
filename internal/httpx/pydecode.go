package httpx

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// PyDecode 复刻 Python json.loads 的解析与报错语义，用于把旧实现
// （/usr/local/bin/muse-proxy.py）的 400 文案逐字节对齐。
//
// 为什么不直接用 encoding/json：Go 的解析错误消息与 Python 的
// JSONDecodeError 完全不同，且行列/字符偏移口径不同（Python 按 code point 计、
// 列号从 1 起）。这里移植 CPython 3.13 Modules/_json.c 的扫描器，
// 在 rune 序列上计位，因此 *PyDecodeError 的 Error() 与 Python 的
// "msg: line L column C (char N)" 完全一致。
//
// 值映射与 Python 对齐：对象→map[string]any、数组→[]any、字符串→string、
// 整数/实数→json.Number（保留字面量以便 PyStr 还原）、true/false→bool、
// null→nil、NaN/Infinity/-Infinity→float64。
func PyDecode(body []byte) (any, error) {
	runes, err := pyDecodeUTF8(body)
	if err != nil {
		return nil, err
	}
	return pyParse(runes)
}

// PyDecodeError 复刻 Python json.decoder.JSONDecodeError 的字符串形式。
type PyDecodeError struct {
	// msg 是未格式化的错误说明（如 "Expecting value"）。
	msg string
	// pos 是失败处在文档中的 rune 下标（对应 Python 的 char）。
	pos int
	// doc 是完整文档的 rune 序列，用于换算行列。
	doc []rune
}

// Error 生成与 Python 完全一致的 "msg: line L column C (char N)" 文案。
func (e *PyDecodeError) Error() string {
	line := 1
	for i := 0; i < e.pos && i < len(e.doc); i++ {
		if e.doc[i] == '\n' {
			line++
		}
	}
	lastNL := -1
	for i := e.pos - 1; i >= 0; i-- {
		if e.doc[i] == '\n' {
			lastNL = i
			break
		}
	}
	// Python: colno = pos - doc.rfind('\n', 0, pos)，未找到时为 -1。
	col := e.pos - lastNL
	return fmt.Sprintf("%s: line %d column %d (char %d)", e.msg, line, col, e.pos)
}

// pyStop 表示 Python 扫描器里的 StopIteration：最内层"找不到值"的位置，
// 由顶层统一转成 "Expecting value"。对象/数组遇到它时原样向上传播，
// 与 C 扫描器的异常传播路径一致。
type pyStop struct{ pos int }

func (pyStop) Error() string { return "stop iteration" }

// pyCodecError 复刻 Python 对非 UTF-8 字节的 UnicodeDecodeError 文案。
type pyCodecError struct{ msg string }

func (e *pyCodecError) Error() string { return e.msg }

// pyParse 在已解码的 rune 序列上实现 JSONDecoder.decode：跳过首尾空白、
// 只解析一个值，值后仍有非空白则报 "Extra data"。
func pyParse(s []rune) (any, error) {
	sc := &pyScanner{s: s}
	idx := 0
	for idx < len(s) && isJSONSpace(s[idx]) {
		idx++
	}
	v, end, err := sc.scanOnce(idx)
	if err != nil {
		if stop, ok := err.(pyStop); ok {
			return nil, &PyDecodeError{msg: "Expecting value", pos: stop.pos, doc: s}
		}
		return nil, err
	}
	// 与 Python decode 一致：raw_decode 后跳过尾随空白，剩余即 Extra data，
	// 位置取第一个非空白字符。
	for end < len(s) && isJSONSpace(s[end]) {
		end++
	}
	if end != len(s) {
		return nil, &PyDecodeError{msg: "Extra data", pos: end, doc: s}
	}
	return v, nil
}

// pyScanner 承载一次解析所需的 rune 文档。
type pyScanner struct{ s []rune }

// isJSONSpace 对应 Python 的 WHITESPACE_STR = " \t\n\r"。
func isJSONSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// scanOnce 对应 _json.c 的 scan_once_unicode：读取任意一个 JSON 值。
func (sc *pyScanner) scanOnce(idx int) (any, int, error) {
	s := sc.s
	if idx >= len(s) {
		return nil, 0, pyStop{idx}
	}
	switch s[idx] {
	case '"':
		return sc.scanString(idx+1, true)
	case '{':
		return sc.parseObject(idx + 1)
	case '[':
		return sc.parseArray(idx + 1)
	case 'n':
		if idx+3 < len(s) && s[idx+1] == 'u' && s[idx+2] == 'l' && s[idx+3] == 'l' {
			return nil, idx + 4, nil
		}
	case 't':
		if idx+3 < len(s) && s[idx+1] == 'r' && s[idx+2] == 'u' && s[idx+3] == 'e' {
			return true, idx + 4, nil
		}
	case 'f':
		if idx+4 < len(s) && s[idx+1] == 'a' && s[idx+2] == 'l' && s[idx+3] == 's' && s[idx+4] == 'e' {
			return false, idx + 5, nil
		}
	case 'N':
		if idx+2 < len(s) && s[idx+1] == 'a' && s[idx+2] == 'N' {
			return math.NaN(), idx + 3, nil
		}
	case 'I':
		if idx+7 < len(s) && s[idx+1] == 'n' && s[idx+2] == 'f' && s[idx+3] == 'i' &&
			s[idx+4] == 'n' && s[idx+5] == 'i' && s[idx+6] == 't' && s[idx+7] == 'y' {
			return math.Inf(1), idx + 8, nil
		}
	case '-':
		if idx+8 < len(s) && s[idx+1] == 'I' && s[idx+2] == 'n' && s[idx+3] == 'f' &&
			s[idx+4] == 'i' && s[idx+5] == 'n' && s[idx+6] == 'i' && s[idx+7] == 't' && s[idx+8] == 'y' {
			return math.Inf(-1), idx + 9, nil
		}
	}
	return sc.matchNumber(idx)
}

// parseObject 对应 _parse_object_unicode；idx 指向 '{' 之后。
func (sc *pyScanner) parseObject(idx int) (any, int, error) {
	s := sc.s
	obj := map[string]any{}
	for idx < len(s) && isJSONSpace(s[idx]) {
		idx++
	}
	if idx >= len(s) || s[idx] != '}' {
		for {
			if idx >= len(s) || s[idx] != '"' {
				return nil, 0, &PyDecodeError{msg: "Expecting property name enclosed in double quotes", pos: idx, doc: s}
			}
			keyVal, next, err := sc.scanString(idx+1, true)
			if err != nil {
				return nil, 0, err
			}
			idx = next
			for idx < len(s) && isJSONSpace(s[idx]) {
				idx++
			}
			if idx >= len(s) || s[idx] != ':' {
				return nil, 0, &PyDecodeError{msg: "Expecting ':' delimiter", pos: idx, doc: s}
			}
			idx++
			for idx < len(s) && isJSONSpace(s[idx]) {
				idx++
			}
			val, next, err := sc.scanOnce(idx)
			if err != nil {
				return nil, 0, err
			}
			obj[keyVal.(string)] = val
			idx = next
			for idx < len(s) && isJSONSpace(s[idx]) {
				idx++
			}
			if idx < len(s) && s[idx] == '}' {
				break
			}
			if idx >= len(s) || s[idx] != ',' {
				return nil, 0, &PyDecodeError{msg: "Expecting ',' delimiter", pos: idx, doc: s}
			}
			commaIdx := idx
			idx++
			for idx < len(s) && isJSONSpace(s[idx]) {
				idx++
			}
			if idx < len(s) && s[idx] == '}' {
				return nil, 0, &PyDecodeError{msg: "Illegal trailing comma before end of object", pos: commaIdx, doc: s}
			}
		}
	}
	return obj, idx + 1, nil
}

// parseArray 对应 _parse_array_unicode；idx 指向 '[' 之后。
func (sc *pyScanner) parseArray(idx int) (any, int, error) {
	s := sc.s
	arr := []any{}
	for idx < len(s) && isJSONSpace(s[idx]) {
		idx++
	}
	if idx >= len(s) || s[idx] != ']' {
		for {
			val, next, err := sc.scanOnce(idx)
			if err != nil {
				return nil, 0, err
			}
			arr = append(arr, val)
			idx = next
			for idx < len(s) && isJSONSpace(s[idx]) {
				idx++
			}
			if idx < len(s) && s[idx] == ']' {
				break
			}
			if idx >= len(s) || s[idx] != ',' {
				return nil, 0, &PyDecodeError{msg: "Expecting ',' delimiter", pos: idx, doc: s}
			}
			commaIdx := idx
			idx++
			for idx < len(s) && isJSONSpace(s[idx]) {
				idx++
			}
			if idx < len(s) && s[idx] == ']' {
				return nil, 0, &PyDecodeError{msg: "Illegal trailing comma before end of array", pos: commaIdx, doc: s}
			}
		}
	}
	if idx >= len(s) || s[idx] != ']' {
		return nil, 0, &PyDecodeError{msg: "Expecting value", pos: len(s) - 1, doc: s}
	}
	return arr, idx + 1, nil
}

// scanString 对应 scanstring_unicode；end 指向起始引号之后。
//
// 复刻 C 实现的几处关键细节：控制字符报错落在该字符本身；非法转义报错
// 落在反斜杠；\uXXXX 相关报错落在 'u'；以及仅在尚未处理过转义时走
// "直接切片"快路径。
func (sc *pyScanner) scanString(end int, strict bool) (any, int, error) {
	s := sc.s
	length := len(s)
	begin := end - 1
	var b strings.Builder
	for {
		next := end
		var c rune
		for ; next < length; next++ {
			c = s[next]
			if c == '"' || c == '\\' {
				break
			}
			if c <= 0x1f && strict {
				return nil, 0, &PyDecodeError{msg: "Invalid control character at", pos: next, doc: s}
			}
		}
		if c == '"' && b.Len() == 0 {
			return string(s[end:next]), next + 1, nil
		}
		// C 实现：循环正常结束（next==length）时 c 是末字符或 0，既非引号也非
		// 反斜杠，直接报未闭合字符串。
		if c != '"' && c != '\\' {
			return nil, 0, &PyDecodeError{msg: "Unterminated string starting at", pos: begin, doc: s}
		}
		if next != end {
			b.WriteString(string(s[end:next]))
		}
		next++
		if c == '"' {
			return b.String(), next, nil
		}
		if next == length {
			return nil, 0, &PyDecodeError{msg: "Unterminated string starting at", pos: begin, doc: s}
		}
		c = s[next]
		if c != 'u' {
			var out rune
			ok := true
			switch c {
			case '"':
				out = '"'
			case '\\':
				out = '\\'
			case '/':
				out = '/'
			case 'b':
				out = '\b'
			case 'f':
				out = '\f'
			case 'n':
				out = '\n'
			case 'r':
				out = '\r'
			case 't':
				out = '\t'
			default:
				ok = false
			}
			if !ok {
				return nil, 0, &PyDecodeError{msg: "Invalid \\escape", pos: next - 1, doc: s}
			}
			b.WriteRune(out)
			end = next + 1
			continue
		}
		uni, newEnd, err := sc.decodeEscapedRune(next)
		if err != nil {
			return nil, 0, err
		}
		b.WriteRune(uni)
		end = newEnd
	}
}

// decodeEscapedRune 解析起始于 'u'（下标 uIdx）的 \uXXXX 及可能的代理对，
// 返回合并后的码点与其后的下标。报错位置与 C 实现一一对应。
func (sc *pyScanner) decodeEscapedRune(uIdx int) (rune, int, error) {
	s := sc.s
	first := uIdx + 1
	end := first + 4
	if end >= len(s) {
		return 0, 0, &PyDecodeError{msg: "Invalid \\uXXXX escape", pos: uIdx, doc: s}
	}
	uni, ok := decodeHex4(s, first)
	if !ok {
		return 0, 0, &PyDecodeError{msg: "Invalid \\uXXXX escape", pos: end - 5, doc: s}
	}
	if uni >= 0xd800 && uni <= 0xdbff && end+6 < len(s) && s[end] == '\\' && s[end+1] == 'u' {
		second := end + 2
		end2 := second + 4
		if end2 >= len(s) {
			return 0, 0, &PyDecodeError{msg: "Invalid \\uXXXX escape", pos: end2 - 5, doc: s}
		}
		uni2, ok2 := decodeHex4(s, second)
		if !ok2 {
			return 0, 0, &PyDecodeError{msg: "Invalid \\uXXXX escape", pos: end2 - 5, doc: s}
		}
		if uni2 >= 0xdc00 && uni2 <= 0xdfff {
			return 0x10000 + ((uni-0xd800)<<10 | (uni2 - 0xdc00)), end2, nil
		}
		return uni, end, nil
	}
	return uni, end, nil
}

// decodeHex4 读取从 i 起的 4 个十六进制数字。
func decodeHex4(s []rune, i int) (rune, bool) {
	var v rune
	for j := 0; j < 4; j++ {
		d := s[i+j]
		switch {
		case d >= '0' && d <= '9':
			v = v<<4 | (d - '0')
		case d >= 'a' && d <= 'f':
			v = v<<4 | (d - 'a' + 10)
		case d >= 'A' && d <= 'F':
			v = v<<4 | (d - 'A' + 10)
		default:
			return 0, false
		}
	}
	return v, true
}

// matchNumber 对应 _match_number_unicode；无合法数字前缀时返回 pyStop(start)，
// 由顶层转成 "Expecting value"。
func (sc *pyScanner) matchNumber(start int) (any, int, error) {
	s := sc.s
	endIdx := len(s) - 1
	idx := start
	if s[idx] == '-' {
		idx++
		if idx > endIdx {
			return nil, 0, pyStop{start}
		}
	}
	isFloat := false
	switch {
	case s[idx] >= '1' && s[idx] <= '9':
		idx++
		for idx <= endIdx && s[idx] >= '0' && s[idx] <= '9' {
			idx++
		}
	case s[idx] == '0':
		idx++
	default:
		return nil, 0, pyStop{start}
	}
	if idx < endIdx && s[idx] == '.' && s[idx+1] >= '0' && s[idx+1] <= '9' {
		isFloat = true
		idx += 2
		for idx <= endIdx && s[idx] >= '0' && s[idx] <= '9' {
			idx++
		}
	}
	if idx < endIdx && (s[idx] == 'e' || s[idx] == 'E') {
		eStart := idx
		idx++
		if idx < endIdx && (s[idx] == '-' || s[idx] == '+') {
			idx++
		}
		for idx <= endIdx && s[idx] >= '0' && s[idx] <= '9' {
			idx++
		}
		if s[idx-1] >= '0' && s[idx-1] <= '9' {
			isFloat = true
		} else {
			idx = eStart
		}
	}
	lit := string(s[start:idx])
	if isFloat {
		// 输入已被扫描器限定为合法实数；溢出时 Go 返回 ±Inf（与 Python float() 一致），
		// 故忽略 error。
		f, _ := strconv.ParseFloat(lit, 64)
		return f, idx, nil
	}
	return json.Number(lit), idx, nil
}

// PyAttrError 复刻 Python 对"值不是对象却调用 .get"时的 AttributeError 文案。
func PyAttrError(v any) string {
	return "'" + pyTypeName(v) + "' object has no attribute 'get'"
}

// pyTypeName 给出 Python 的 type(v).__name__，用于 AttributeError 文案。
func pyTypeName(v any) string {
	switch t := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case map[string]any:
		return "dict"
	case json.Number:
		if strings.ContainsAny(string(t), ".eE") {
			return "float"
		}
		return "int"
	case float64:
		return "float"
	default:
		return fmt.Sprintf("%T", t)
	}
}
