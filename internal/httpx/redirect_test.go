package httpx

import "testing"

// TestRedirectLoopErrorMatchesPython 锁定重定向环文案逐字节等于 Python 3.13
// urllib 的 HTTPError.__str__（真机上以自环上游实测取得）：
// `HTTP Error 302: The HTTP server returned a redirect error that would lead to an
// infinite loop.\nThe last 30x error message was:\nFound`。
func TestRedirectLoopErrorMatchesPython(t *testing.T) {
	cases := []struct {
		name       string
		code       int
		statusLine string
		want       string
	}{
		{
			"302 Found",
			302,
			"302 Found",
			"HTTP Error 302: The HTTP server returned a redirect error that would lead to an infinite loop.\n" +
				"The last 30x error message was:\nFound",
		},
		{
			"303 非标准短语原样取用",
			303,
			"303 Weird",
			"HTTP Error 303: The HTTP server returned a redirect error that would lead to an infinite loop.\n" +
				"The last 30x error message was:\nWeird",
		},
		{
			"状态行未给短语则兜底",
			302,
			"302",
			"HTTP Error 302: The HTTP server returned a redirect error that would lead to an infinite loop.\n" +
				"The last 30x error message was:\nFound",
		},
	}
	for _, c := range cases {
		got := NewRedirectLoopError(c.code, c.statusLine).Error()
		if got != c.want {
			t.Errorf("%s：环路文案不一致：\n got=%q\nwant=%q", c.name, got, c.want)
		}
	}
}
