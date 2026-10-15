package proxy

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"muse-proxy-go/internal/config"
)

// redirectLoopWant 拼出旧实现（urllib）重定向环 502 的完整响应体，便于逐字节比对。
func redirectLoopWant(code int, reason string) string {
	return `{"type": "error", "error": {"message": "upstream failed: HTTP Error ` +
		strconv.Itoa(code) + `: The HTTP server returned a redirect error that would lead to an infinite loop.\n` +
		`The last 30x error message was:\n` + reason + `"}}`
}

// TestRedirectSelfLoopMatchesOld 验证 302 自环（Location 指回自身）复刻旧实现的 502 文案，
// 且跟随次数与旧实现一致（urllib max_repeats=4 → 上游共收到 5 次请求）。
func TestRedirectSelfLoopMatchesOld(t *testing.T) {
	fp := newFakeProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://upstream.example"+r.URL.RequestURI())
		w.WriteHeader(http.StatusFound)
	})
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	resp, body := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("302 自环应回 502，得到 %d", resp.StatusCode)
	}
	if want := redirectLoopWant(http.StatusFound, "Found"); body != want {
		t.Fatalf("302 自环文案不一致：\n got=%s\nwant=%s", body, want)
	}
	if got := fp.count(); got != 5 {
		t.Fatalf("自环应跟随 4 次后判环（上游共 5 次请求），实际 %d", got)
	}
}

// TestRedirectJitterLoopMatchesOld 验证 302→303→302 抖动（两个 URL 交替）同样复刻旧实现：
// 触发判环的那次是 302，文案 reason 为 Found，上游共收到 9 次请求。
func TestRedirectJitterLoopMatchesOld(t *testing.T) {
	fp := newFakeProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/jit-a") {
			w.Header().Set("Location", "http://upstream.example/v1/jit-b")
			w.WriteHeader(http.StatusSeeOther) // 303
			return
		}
		w.Header().Set("Location", "http://upstream.example/v1/jit-a")
		w.WriteHeader(http.StatusFound) // 302
	})
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	resp, body := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("抖动环应回 502，得到 %d", resp.StatusCode)
	}
	if want := redirectLoopWant(http.StatusFound, "Found"); body != want {
		t.Fatalf("抖动环文案不一致：\n got=%s\nwant=%s", body, want)
	}
	if got := fp.count(); got != 9 {
		t.Fatalf("抖动环上游应收到 9 次请求，实际 %d", got)
	}
}

// TestRedirectDistinctChainCapMatchesOld 验证「不重复但超过跟随上限」也按旧实现判环：
// urllib max_redirections=10，跟满 10 个不同 URL 后第 11 跳抛错（上游共 11 次请求）。
func TestRedirectDistinctChainCapMatchesOld(t *testing.T) {
	fp := newFakeProxy(t, func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/v1/r"))
		w.Header().Set("Location", "http://upstream.example/v1/r"+strconv.Itoa(n+1))
		w.WriteHeader(http.StatusFound)
	})
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	resp, body := doReq(t, srv.URL, "POST", "/v1/r0", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("超上限链应回 502，得到 %d", resp.StatusCode)
	}
	if want := redirectLoopWant(http.StatusFound, "Found"); body != want {
		t.Fatalf("超上限链文案不一致：\n got=%s\nwant=%s", body, want)
	}
	if got := fp.count(); got != 11 {
		t.Fatalf("超上限链上游应收到 11 次请求，实际 %d", got)
	}
}

// rawLoopUpstream 用裸 TCP 冒充出口代理/上游：读请求行取出绝对目标 URL，再按给定状态行
// 回 302，Location 指回同一 URL 形成自环。Go 的 httptest 无法自定义 reason 短语，
// 故用它验证「环里的 reason 原样取用」。
func rawLoopUpstream(t *testing.T, statusLine string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("裸上游监听失败：%v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				requestLine, err := br.ReadString('\n')
				if err != nil {
					return
				}
				for { // 读完请求头，避免客户端还在写请求时就收到 RST。
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if line == "\r\n" {
						break
					}
				}
				fields := strings.Fields(requestLine)
				target := ""
				if len(fields) >= 2 {
					target = fields[1]
				}
				resp := "HTTP/1.1 " + statusLine + "\r\n" +
					"Location: " + target + "\r\n" +
					"Content-Length: 0\r\nConnection: close\r\n\r\n"
				_, _ = io.WriteString(c, resp)
			}(conn)
		}
	}()
	return "http://" + ln.Addr().String()
}

// TestRedirectLoopReasonTakenVerbatim 验证环文案里的 reason 取触发那次 3xx 状态行的
// 原样短语（非标准短语也照抄），与旧实现 urllib 的可观测文案逐字节一致。
func TestRedirectLoopReasonTakenVerbatim(t *testing.T) {
	raw := rawLoopUpstream(t, "302 Weird")
	srv, _ := newTestProxy(t, &config.Config{ProxyURL: raw}, newFakeProxy(t, okResponder))

	resp, body := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("自环应回 502，得到 %d", resp.StatusCode)
	}
	if want := redirectLoopWant(http.StatusFound, "Weird"); body != want {
		t.Fatalf("非标准 reason 文案不一致：\n got=%s\nwant=%s", body, want)
	}
}

// TestRedirectChainStillFollowed 验证非环的双跳普通重定向仍照旧跟随（301→302→200），
// 不会被环判定影响。
func TestRedirectChainStillFollowed(t *testing.T) {
	fp := newFakeProxy(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/final":
			w.Header().Set("X-Final", "yes")
			_, _ = io.WriteString(w, "landed-double")
		case "/v1/step2":
			w.Header().Set("Location", "http://upstream.example/v1/final")
			w.WriteHeader(http.StatusFound)
		default:
			w.Header().Set("Location", "http://upstream.example/v1/step2")
			w.WriteHeader(http.StatusMovedPermanently)
		}
	})
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	resp, body := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("双跳重定向应跟随到 200，得到 %d", resp.StatusCode)
	}
	if body != "landed-double" || resp.Header.Get("X-Final") != "yes" {
		t.Fatalf("跟随双跳后应回传最终响应，得到 body=%q header=%q", body, resp.Header.Get("X-Final"))
	}
	if fp.count() != 3 {
		t.Fatalf("双跳应发生两次重定向（上游共 3 次请求），实际 %d", fp.count())
	}
}
