package httpx

// statusReasons 复刻 Python `http.client.responses`（`http.HTTPStatus` 的 standard
// reason phrase），用于把上游 4xx/5xx 复刻成旧实现的 502 文案里的 <reason>。
//
// 旧实现（urllib）实际取的是上游状态行里的原因短语；本版改用标准表，是为了不依赖
// 上游是否发送短语、并与旧运行时同源。表以本机 Python 3.13.5 的 `http.client.responses`
// 逐项比对生成。两处按需求书 §2.6 列举值收口（413=Payload Too Large、422=Unprocessable
// Entity）：这两个码在 Python 3.13 的 dict 里已按 RFC 9110 改名为 Content Too Large /
// Unprocessable Content，取舍与并排对照见 docs/COMPAT.md §2.3。
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
func ReasonPhrase(code int) string {
	if reason, ok := statusReasons[code]; ok {
		return reason
	}
	return "Unknown"
}
