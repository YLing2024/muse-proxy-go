package httpx

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Quote 生成与 Python json.dumps(s, ensure_ascii=True) 一致的 JSON 字符串字面量。
//
// 旧实现用 json.dumps 序列化错误体，要保证 403 响应「逐字节相同」就必须复刻其转义规则：
//   - 短转义 \b \f \n \r \t 与 \" \\；
//   - ASCII 可打印区（0x20..0x7e）原样输出，其余（含 DEL 0x7f 与非 ASCII）转 \uXXXX；
//   - 非 BMP 字符拆成 UTF-16 代理对；十六进制小写。
func Quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			switch {
			case r >= 0x20 && r <= 0x7e:
				b.WriteRune(r)
			case r <= 0xffff:
				fmt.Fprintf(&b, `\u%04x`, r)
			default:
				v := r - 0x10000
				fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(v>>10), 0xdc00+(v&0x3ff))
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// PyStr 复刻 Python 对 f-string 里任意 JSON 值的字符串化，用于错误信息中的 model。
//
// 白名单只含字符串，正常路径永远是字符串；这里覆盖数值/布尔/null 是为了在
// 「model 不是字符串」的边界请求下，错误体也尽量贴近旧实现。
func PyStr(v any) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case bool:
		if t {
			return "True"
		}
		return "False"
	case string:
		return t
	case json.Number:
		return pyNumber(string(t))
	case float64:
		return pyFloat(t)
	default:
		return fmt.Sprint(t)
	}
}

// pyNumber 复刻 Python：无小数点/指数的 JSON 数字是 int（原样），否则是 float（带小数点）。
func pyNumber(lit string) string {
	if !strings.ContainsAny(lit, ".eE") {
		return lit
	}
	f, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		return lit
	}
	return pyFloat(f)
}

// pyFloat 让浮点输出带小数点，贴近 Python str(float) 的观感（如 1.0 而非 1）。
func pyFloat(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
