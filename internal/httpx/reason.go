package httpx

import "strings"

// StatusLineReason 复刻旧实现 502（及重定向环）文案里 `<reason>` 的取值。
//
// 旧实现用 urllib：其 HTTPError.msg 直接取自 http.client 从上游状态行解出的 reason
// （`line.split(None, 2)` 后 `reason.strip()`）。上游状态行没给短语时，该值是**空串**，
// 于是 HTTPError.__str__ 输出 `HTTP Error <code>: `（冒号后为空）。旧实现**从不查任何
// 标准短语表**，所以这里也必须原样返回、含空串，不能改用标准表兜底——否则会在
// 「上游只写状态码」这一形态上与旧实现逐字节不一致（实测：旧会留空、新版曾回
// `Internal Server Error` / `Unknown`）。
//
// statusLine 取自 `http.Response.Status`（Go 保留状态行里码之后的原文）。
func StatusLineReason(statusLine string) string {
	if i := strings.IndexByte(statusLine, ' '); i >= 0 {
		// 与 Python 的 reason.strip() 对齐：去掉首尾空白后原样返回，空即空。
		return strings.TrimSpace(statusLine[i+1:])
	}
	return ""
}
