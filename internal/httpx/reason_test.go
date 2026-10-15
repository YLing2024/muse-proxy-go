package httpx

import "testing"

// TestReasonPhraseMatchesPython 锁定标准表兜底的原因短语，取值与 Python 3.13
// `http.client.responses` 一致；未收录的码回 "Unknown"。标准表只在上游未给短语时使用。
func TestReasonPhraseMatchesPython(t *testing.T) {
	cases := map[int]string{
		400: "Bad Request",
		401: "Unauthorized",
		403: "Forbidden",
		404: "Not Found",
		405: "Method Not Allowed",
		409: "Conflict",
		413: "Payload Too Large",
		422: "Unprocessable Entity",
		429: "Too Many Requests",
		500: "Internal Server Error",
		502: "Bad Gateway",
		503: "Service Unavailable",
		504: "Gateway Timeout",
		// 未收录的码走通用兜底。
		599: "Unknown",
		999: "Unknown",
	}
	for code, want := range cases {
		if got := ReasonPhrase(code); got != want {
			t.Errorf("ReasonPhrase(%d) = %q，want %q", code, got, want)
		}
	}
}

// TestStatusLineReasonPrefersUpstream 验证 `<reason>` 优先取上游状态行原样短语
// （含非标准短语），仅在上游未给短语时回落到标准表。语义对齐 Python
// `line.split(None, 2)` 后 `reason.strip()`。
func TestStatusLineReasonPrefersUpstream(t *testing.T) {
	cases := []struct {
		name       string
		code       int
		statusLine string
		want       string
	}{
		{"标准短语原样取用", 404, "404 Not Found", "Not Found"},
		{"非标准短语原样取用", 413, "413 Nope", "Nope"},
		{"非标准短语原样取用 599", 599, "599 Weird", "Weird"},
		{"Go 假上游对 599 的兜底短语", 599, "599 status code 599", "status code 599"},
		{"上游只写码", 500, "500", "Internal Server Error"},
		{"码后有空白无短语", 500, "500 ", "Internal Server Error"},
		{"码后多空格", 413, "413  Nope ", "Nope"},
		{"状态行缺失", 503, "", "Service Unavailable"},
		{"空短语且未收录码", 599, "599 ", "Unknown"},
	}
	for _, c := range cases {
		if got := StatusLineReason(c.code, c.statusLine); got != c.want {
			t.Errorf("%s：StatusLineReason(%d, %q) = %q，want %q", c.name, c.code, c.statusLine, got, c.want)
		}
	}
}
