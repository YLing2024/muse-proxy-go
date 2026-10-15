package httpx

import "testing"

// TestStatusLineReasonTakenVerbatim 锁定 `<reason>` 的取值与旧实现 urllib 一致：
// 原样取上游状态行的短语（含非标准短语），**上游未给短语时留空串**——旧实现的
// HTTPError.msg 就是空串，绝不回落到任何标准短语表。
func TestStatusLineReasonTakenVerbatim(t *testing.T) {
	cases := []struct {
		name       string
		statusLine string
		want       string
	}{
		{"标准短语原样取用", "404 Not Found", "Not Found"},
		{"非标准短语原样取用", "413 Nope", "Nope"},
		{"非标准短语原样取用 599", "599 Weird", "Weird"},
		{"Go 假上游对 599 的兜底短语", "599 status code 599", "status code 599"},
		{"上游只写码", "500", ""},
		{"码后有空白无短语", "500 ", ""},
		{"码后多空格", "413  Nope ", "Nope"},
		{"状态行缺失", "", ""},
	}
	for _, c := range cases {
		if got := StatusLineReason(c.statusLine); got != c.want {
			t.Errorf("%s：StatusLineReason(%q) = %q，want %q", c.name, c.statusLine, got, c.want)
		}
	}
}
