package proxy

import (
	"net/http"

	"muse-proxy-go/internal/httpx"
)

// 复刻 CPython urllib.request.HTTPRedirectHandler 的环路上限：同一个 URL 最多被
// 重定向跟随 redirectMaxRepeats 次，累计跟随的不同 URL 最多 redirectMaxVisits 个。
//
// 旧实现并不在「第一次重复」时就判环——它给 cookie 等有状态重定向留了余量：只有同一
// URL 被再次导向超过 redirectMaxRepeats 次、或跟随的不同 URL 超过 redirectMaxVisits 个
// 才抛错。逐值复刻这两条，才能既对齐旧实现、又不把「合法地回到某个 URL 一两次」误判成环。
const (
	redirectMaxRepeats = 4
	redirectMaxVisits  = 10
)

// redirectTracker 记录单次转发已跟随的重定向目标及次数。它只属于一次请求，
// 故不会在并发请求间共享状态。
type redirectTracker struct {
	visits map[string]int
}

// newRedirectTracker 返回一个空的重定向跟踪器。
func newRedirectTracker() *redirectTracker {
	return &redirectTracker{visits: make(map[string]int)}
}

// check 作为 http.Client.CheckRedirect：复刻 urllib 跟随重定向前判定顺序——先看目标
// URL 的已访问次数是否达到 redirectMaxRepeats，再看已记录的不同 URL 数是否达到
// redirectMaxVisits，任一成立即返回与旧实现等价的错误；否则记一次并继续跟随。
//
// req.Response 是触发本次跳转的那个 3xx，其状态码与状态行即旧实现抛错时用的 code/msg。
func (t *redirectTracker) check(req *http.Request, _ []*http.Request) error {
	target := req.URL.String()
	if t.visits[target] >= redirectMaxRepeats || len(t.visits) >= redirectMaxVisits {
		return httpx.NewRedirectLoopError(req.Response.StatusCode, req.Response.Status)
	}
	t.visits[target]++
	return nil
}
