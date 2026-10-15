package httpx

import "testing"

// TestReasonPhraseMatchesPython 锁定常见状态码的原因短语，取值与 Python 3.13
// `http.client.responses` 一致；未收录的码回 "Unknown"。
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
