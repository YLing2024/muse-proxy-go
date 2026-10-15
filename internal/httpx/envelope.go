// Package httpx 提供与旧实现（/usr/local/bin/muse-proxy.py）逐字节兼容的 HTTP 响应工具。
package httpx

import (
	"net/http"
	"strconv"
)

// ErrorEnvelope 生成旧实现 _reply 的错误体：{"type": "error", "error": {"message": <message>}}。
//
// 分隔符与空白逐个复刻 Python json.dumps 的默认输出（逗号/冒号后各一个空格），
// 这样 403 等分支的响应体与旧实现逐字节一致。
func ErrorEnvelope(message string) string {
	return `{"type": "error", "error": {"message": ` + Quote(message) + `}}`
}

// WriteError 以给定状态码写出错误信封，并设置与旧实现一致的 Content-Type 与 Content-Length。
func WriteError(w http.ResponseWriter, status int, message string) {
	body := ErrorEnvelope(message)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
