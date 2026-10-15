package httpx

import "fmt"

// pyDecodeUTF8 复刻 Python json.loads 对 bytes 的解码：等价于
// body.decode("utf-8-sig" | "utf-8", "surrogatepass")。
//
// 之所以自己解 UTF-8 而不用 utf8.DecodeRune：Python 允许 surrogatepass，
// 即接受把 U+D800..U+DFFF 编码成 UTF-8 的非法序列，而 Go 把它判为非法。
// 另外旧实现报出的 char 位置按 code point 计，故这里统一产出 rune 序列。
//
// 本实现只覆盖 UTF-8 家族；带 UTF-16/UTF-32 BOM 的输入属于未映射形态。
func pyDecodeUTF8(b []byte) ([]rune, error) {
	// detect_encoding 对 UTF-8 BOM 返回 utf-8-sig，会先吃掉 BOM。
	if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		b = b[3:]
	}
	out := make([]rune, 0, len(b))
	i := 0
	for i < len(b) {
		c := b[i]
		if c < 0x80 {
			out = append(out, rune(c))
			i++
			continue
		}
		var n int
		switch {
		case c >= 0xc2 && c <= 0xdf:
			n = 2
		case c >= 0xe0 && c <= 0xef:
			n = 3
		case c >= 0xf0 && c <= 0xf4:
			n = 4
		default:
			return nil, pyCodecErr(c, i, i+1, "invalid start byte")
		}
		for j := 1; j < n; j++ {
			if i+j >= len(b) {
				return nil, pyCodecErr(c, i, i+j, "unexpected end of data")
			}
			d := b[i+j]
			if d < 0x80 || d > 0xbf {
				return nil, pyCodecErr(c, i, i+j, "invalid continuation byte")
			}
		}
		// 过短编码（overlong）与超出 U+10FFFF 的编码同样算非法延续字节。
		// 注意：surrogatepass 允许 0xed 0xa0..0xbf，故对 0xed 不加约束。
		if n == 3 && c == 0xe0 && b[i+1] < 0xa0 {
			return nil, pyCodecErr(c, i, i+1, "invalid continuation byte")
		}
		if n == 4 && ((c == 0xf0 && b[i+1] < 0x90) || (c == 0xf4 && b[i+1] > 0x8f)) {
			return nil, pyCodecErr(c, i, i+1, "invalid continuation byte")
		}
		var r rune
		switch n {
		case 2:
			r = rune(c&0x1f)<<6 | rune(b[i+1]&0x3f)
		case 3:
			r = rune(c&0x0f)<<12 | rune(b[i+1]&0x3f)<<6 | rune(b[i+2]&0x3f)
		case 4:
			r = rune(c&0x07)<<18 | rune(b[i+1]&0x3f)<<12 | rune(b[i+2]&0x3f)<<6 | rune(b[i+3]&0x3f)
		}
		out = append(out, r)
		i += n
	}
	return out, nil
}

// pyCodecErr 构造与 Python UnicodeDecodeError 一致的 UTF-8 解码文案。
// leadByte 是序列首字节，[start,end) 是出错的字节范围（end 不含）。
func pyCodecErr(leadByte byte, start, end int, reason string) error {
	var where string
	if end-start <= 1 {
		where = fmt.Sprintf("byte 0x%02x in position %d", leadByte, start)
	} else {
		where = fmt.Sprintf("bytes in position %d-%d", start, end-1)
	}
	return &pyCodecError{msg: fmt.Sprintf("'utf-8' codec can't decode %s: %s", where, reason)}
}
