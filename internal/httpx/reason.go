package httpx

import "strings"

// statusReasons 复刻 Python `http.client.responses`（`http.HTTPStatus` 的 standard
// reason phrase）。
//
// 它**只在旧实现没有理由短语可复刻时兜底**：旧实现（urllib）的 502 文案 `<reason>`
// 取自上游状态行本身（`http.client` 把状态行按空白拆成 `version, code, reason`，
// 故短语是上游原样，而非任何标准表）。因此本表仅在上游状态行未给短语时使用
// （HTTP/2 无 reason，或状态行只写到码），见 StatusLineReason。
//
// 表以本机 Python 3.13.5 的 `http.client.responses` 逐项比对生成。两处沿用需求书 §2.6
// 列举值（413=Payload Too Large、422=Unprocessable Entity）：这两个码在 Python 3.13 的
// dict 里已按 RFC 9110 改名为 Content Too Large / Unprocessable Content。因为兜底才是
// 本表的用途、上游给了短语就以上游为准，保留经典还是新版短语都不影响一致性。
var statusReasons = map[int]string{
	100: "Continue",
	101: "Switching Protocols",
	102: "Processing",
	103: "Early Hints",

	200: "OK",
	201: "Created",
	202: "Accepted",
	203: "Non-Authoritative Information",
	204: "No Content",
	205: "Reset Content",
	206: "Partial Content",
	207: "Multi-Status",
	208: "Already Reported",
	226: "IM Used",

	300: "Multiple Choices",
	301: "Moved Permanently",
	302: "Found",
	303: "See Other",
	304: "Not Modified",
	305: "Use Proxy",
	307: "Temporary Redirect",
	308: "Permanent Redirect",

	400: "Bad Request",
	401: "Unauthorized",
	402: "Payment Required",
	403: "Forbidden",
	404: "Not Found",
	405: "Method Not Allowed",
	406: "Not Acceptable",
	407: "Proxy Authentication Required",
	408: "Request Timeout",
	409: "Conflict",
	410: "Gone",
	411: "Length Required",
	412: "Precondition Failed",
	413: "Payload Too Large",
	414: "URI Too Long",
	415: "Unsupported Media Type",
	416: "Range Not Satisfiable",
	417: "Expectation Failed",
	418: "I'm a Teapot",
	421: "Misdirected Request",
	422: "Unprocessable Entity",
	423: "Locked",
	424: "Failed Dependency",
	425: "Too Early",
	426: "Upgrade Required",
	428: "Precondition Required",
	429: "Too Many Requests",
	431: "Request Header Fields Too Large",
	451: "Unavailable For Legal Reasons",

	500: "Internal Server Error",
	501: "Not Implemented",
	502: "Bad Gateway",
	503: "Service Unavailable",
	504: "Gateway Timeout",
	505: "HTTP Version Not Supported",
	506: "Variant Also Negotiates",
	507: "Insufficient Storage",
	508: "Loop Detected",
	510: "Not Extended",
	511: "Network Authentication Required",
}

// ReasonPhrase 返回与 Python `http.client.responses` 一致的标准原因短语；
// 未收录的状态码返回 "Unknown"（沿用 Python 的通用兜底文案风格）。
// 它只在 StatusLineReason 判定上游未给短语时被调用。
func ReasonPhrase(code int) string {
	if reason, ok := statusReasons[code]; ok {
		return reason
	}
	return "Unknown"
}

// StatusLineReason 复刻旧实现 502 文案里的 `<reason>` 取值：
// 优先取**上游状态行里的原样短语**（旧实现 `http.client` 对状态行做
// `line.split(None, 2)` 后 `reason.strip()`，本届用同样的语义），
// 仅当上游没有给出短语时（HTTP/2 无 reason，或状态行在码后即结束）才回落到
// 标准表 ReasonPhrase。
//
// statusLine 取自 `http.Response.Status`（Go 会保留状态行里码之后的原文）。
func StatusLineReason(code int, statusLine string) string {
	if i := strings.IndexByte(statusLine, ' '); i >= 0 {
		// 与 Python 的 reason.strip() 对齐：去掉首尾空白后仍非空即用上游原样短语，
		// 空（上游只写了码、或码后只有空白）才回落到标准表。
		if reason := strings.TrimSpace(statusLine[i+1:]); reason != "" {
			return reason
		}
	}
	return ReasonPhrase(code)
}
