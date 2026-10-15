package httpx

import "strconv"

// redirectLoopDetail 复刻 urllib.request.HTTPRedirectHandler.inf_msg：环路文案里
// 状态码之后、原因短语之前的固定两句。两处换行都是真实换行符，最终由 Quote 转义成
// JSON 里的字面 `\n`（旧实现 json.dumps 同样如此）。
const redirectLoopDetail = "The HTTP server returned a redirect error that would lead to an infinite loop.\n" +
	"The last 30x error message was:\n"

// RedirectLoopError 表示上游把请求导进重定向环（或超过跟随上限），复刻旧实现
// urllib 在 HTTPRedirectHandler 里抛出的 HTTPError。Error() 逐字节对齐该异常的
// 字符串形式 `HTTP Error <code>: <inf_msg><reason>`；外层再由转发层加上
// `upstream failed: ` 前缀。
type RedirectLoopError struct {
	// Code 是触发环判定那一次 3xx 的状态码。
	Code int
	// Reason 是该 3xx 状态行的原因短语，取值规则同 502 文案，见 StatusLineReason。
	Reason string
}

// Error 实现 error，输出即旧实现 HTTPError.__str__ 的结果。
func (e *RedirectLoopError) Error() string {
	return "HTTP Error " + strconv.Itoa(e.Code) + ": " + redirectLoopDetail + e.Reason
}

// NewRedirectLoopError 依据触发环判定那一次 3xx 的状态码与状态行构造错误；
// reason 的取值与上游 502 文案同一规则（原样短语，未给短语时为空串）。
func NewRedirectLoopError(code int, statusLine string) *RedirectLoopError {
	return &RedirectLoopError{Code: code, Reason: StatusLineReason(statusLine)}
}
