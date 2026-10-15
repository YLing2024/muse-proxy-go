package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"muse-proxy-go/internal/config"
	"muse-proxy-go/internal/logx"
)

// recordedRequest 是假代理收到的一次请求（即本代理实际转发出去的内容）。
type recordedRequest struct {
	method string
	host   string
	url    string
	header http.Header
	body   []byte
}

// fakeProxy 是一个「只记录不回源」的 HTTP 代理：记录绝对形式的请求，再按 respond 回包。
// 用它既能验证转发出去的路径/头/body，又能精确构造上游响应。
type fakeProxy struct {
	srv     *httptest.Server
	respond func(w http.ResponseWriter, r *http.Request)
	mu      sync.Mutex
	all     []recordedRequest
}

func newFakeProxy(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *fakeProxy {
	t.Helper()
	fp := &fakeProxy{respond: respond}
	fp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fp.mu.Lock()
		fp.all = append(fp.all, recordedRequest{
			method: r.Method,
			host:   r.Host,
			url:    r.URL.String(),
			header: r.Header.Clone(),
			body:   body,
		})
		fp.mu.Unlock()
		fp.respond(w, r)
	}))
	t.Cleanup(fp.srv.Close)
	return fp
}

// last 返回最近一次记录的请求；无记录则测试失败。
func (fp *fakeProxy) last(t *testing.T) recordedRequest {
	t.Helper()
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.all) == 0 {
		t.Fatal("假代理没有收到任何请求")
	}
	return fp.all[len(fp.all)-1]
}

// count 返回收到的请求数。
func (fp *fakeProxy) count() int {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return len(fp.all)
}

const testUpstreamKey = "UPSTREAM-KEY-FOR-TESTS"

// newTestProxy 装配被测代理（上游 base 带 /v1，出口指向假代理），返回 httptest 服务与日志缓冲。
func newTestProxy(t *testing.T, cfg *config.Config, fp *fakeProxy) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	if cfg.Upstream == "" {
		cfg.Upstream = "http://upstream.example/v1"
	}
	if cfg.ProxyURL == "" {
		cfg.ProxyURL = fp.srv.URL
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	var buf bytes.Buffer
	handler, err := New(cfg, testUpstreamKey, logx.NewLogger(&buf))
	if err != nil {
		t.Fatalf("装配代理失败：%v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, &buf
}

// doReq 直接向被测代理发请求（不经任何出口代理）。
func doReq(t *testing.T, base, method, path, body string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		t.Fatalf("构造请求失败：%v", err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求代理失败：%v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

func okResponder(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"ok":true}`)
}

func TestPathJoinStripsLeadingV1(t *testing.T) {
	fp := newFakeProxy(t, okResponder)
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	body := `{"model":"muse-spark-1.3-contributor"}`
	doReq(t, srv.URL, "POST", "/v1/chat/completions?x=1", body, nil)
	if got := fp.last(t).url; got != "http://upstream.example/v1/chat/completions?x=1" {
		t.Fatalf("带 /v1 的路径拼接错误：%s", got)
	}

	doReq(t, srv.URL, "POST", "/chat/completions", body, nil)
	if got := fp.last(t).url; got != "http://upstream.example/v1/chat/completions" {
		t.Fatalf("裸路径拼接错误：%s", got)
	}
}

func TestWhitelistBranches(t *testing.T) {
	fp := newFakeProxy(t, okResponder)
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	// 允许的模型放行。
	resp, _ := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.2-contributor","messages":[]}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("白名单内模型应 200，得到 %d", resp.StatusCode)
	}

	// 非白名单模型：403，且错误体逐字节等于旧格式。
	resp, body := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"gpt-4"}`, nil)
	if resp.StatusCode != 403 {
		t.Fatalf("非白名单应 403，得到 %d", resp.StatusCode)
	}
	want := `{"type": "error", "error": {"message": "model gpt-4 not allowed through proxy"}}`
	if body != want {
		t.Fatalf("403 错误体不一致：\n got=%s\nwant=%s", body, want)
	}

	// 缺 model 时按 None 处理。
	resp, body = doReq(t, srv.URL, "POST", "/v1/chat/completions", `{}`, nil)
	if resp.StatusCode != 403 || !strings.Contains(body, "model None not allowed") {
		t.Fatalf("缺 model 应 403 且报 None，得到 %d %s", resp.StatusCode, body)
	}

	// body 非 JSON：400。
	resp, body = doReq(t, srv.URL, "POST", "/v1/chat/completions", `not json`, nil)
	if resp.StatusCode != 400 || !strings.HasPrefix(body, `{"type": "error", "error": {"message": "bad request: `) {
		t.Fatalf("非 JSON body 应 400，得到 %d %s", resp.StatusCode, body)
	}

	// 无 body 的 GET /models 直接放行。
	resp, _ = doReq(t, srv.URL, "GET", "/v1/models", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("无 body 请求应放行，得到 %d", resp.StatusCode)
	}
	if got := fp.last(t).url; got != "http://upstream.example/v1/models" {
		t.Fatalf("GET /models 转发路径错误：%s", got)
	}
}

func TestCredentialReplacementAndHeaderFilter(t *testing.T) {
	fp := newFakeProxy(t, okResponder)
	srv, buf := newTestProxy(t, &config.Config{}, fp)

	const clientSecret = "CLIENT-SECRET-MUST-NOT-REACH-UPSTREAM"
	resp, _ := doReq(t, srv.URL, "POST", "/v1/chat/completions",
		`{"model":"muse-spark-1.3-contributor"}`,
		map[string]string{
			"Authorization": "Bearer " + clientSecret,
			"X-Custom":      "kept",
			"Connection":    "close",
		})
	if resp.StatusCode != 200 {
		t.Fatalf("应 200，得到 %d", resp.StatusCode)
	}

	got := fp.last(t)
	if auth := got.header.Get("Authorization"); auth != "Bearer "+testUpstreamKey {
		t.Fatalf("Authorization 未替换为真实 key：%q", auth)
	}
	if strings.Contains(string(got.body), clientSecret) {
		t.Fatal("客户端凭证泄漏到了上游 body")
	}
	if got.header.Get("X-Custom") != "kept" {
		t.Fatalf("普通头应透传，得到 %q", got.header.Get("X-Custom"))
	}
	if got.header.Get("Connection") != "" {
		t.Fatalf("connection 不应透传，得到 %q", got.header.Get("Connection"))
	}
	if got.host != "upstream.example" {
		t.Fatalf("Host 应由上游决定，得到 %q", got.host)
	}
	if strings.Contains(buf.String(), testUpstreamKey) || strings.Contains(buf.String(), clientSecret) {
		t.Fatal("日志里出现了凭证")
	}
}

func TestResponsePassthrough(t *testing.T) {
	fp := newFakeProxy(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Upstream-Marker", "abc")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "upstream-error-body")
	})
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	resp, body := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("状态码应透传 503，得到 %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Upstream-Marker") != "abc" {
		t.Fatalf("上游自定义头应透传，得到 %q", resp.Header.Get("X-Upstream-Marker"))
	}
	if body != "upstream-error-body" {
		t.Fatalf("响应体应原样回传，得到 %q", body)
	}
	if resp.Header.Get("Content-Length") != "19" {
		t.Fatalf("Content-Length 应为 19，得到 %q", resp.Header.Get("Content-Length"))
	}
}

func TestUpstreamUnreachableReturns502(t *testing.T) {
	fp := newFakeProxy(t, okResponder)
	// 出口代理指向一个不可能连上的端口：走不了代理即视为上游失败。
	cfg := &config.Config{ProxyURL: "http://127.0.0.1:1", Timeout: time.Second}
	srv, _ := newTestProxy(t, cfg, fp)

	resp, body := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("应 502，得到 %d", resp.StatusCode)
	}
	if !strings.Contains(body, "upstream failed: ") {
		t.Fatalf("502 信息应以 upstream failed: 开头，得到 %s", body)
	}
}

func TestUpstreamTimeoutReturns502(t *testing.T) {
	fp := newFakeProxy(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		_, _ = io.WriteString(w, "late")
	})
	cfg := &config.Config{Timeout: 100 * time.Millisecond}
	srv, _ := newTestProxy(t, cfg, fp)

	resp, body := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(body, "upstream failed: ") {
		t.Fatalf("超时应 502，得到 %d %s", resp.StatusCode, body)
	}
}

func TestStreamingUsesChunked(t *testing.T) {
	fp := newFakeProxy(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, s := range []string{"data: a\n\n", "data: b\n\n", "data: [DONE]\n\n"} {
			_, _ = io.WriteString(w, s)
			if fl != nil {
				fl.Flush()
			}
		}
	})
	srv, _ := newTestProxy(t, &config.Config{Stream: true}, fp)

	resp, body := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("流式应 200，得到 %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Length") != "" {
		t.Fatalf("流式不应带 Content-Length，得到 %q", resp.Header.Get("Content-Length"))
	}
	if !strings.Contains(strings.Join(resp.TransferEncoding, ","), "chunked") {
		t.Fatalf("流式应以 chunked 传输，得到 %v", resp.TransferEncoding)
	}
	if body != "data: a\n\ndata: b\n\ndata: [DONE]\n\n" {
		t.Fatalf("流式内容不完整：%q", body)
	}
}

func TestBufferedModeHasContentLength(t *testing.T) {
	fp := newFakeProxy(t, okResponder)
	srv, _ := newTestProxy(t, &config.Config{Stream: false}, fp)

	resp, _ := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.Header.Get("Content-Length") == "" {
		t.Fatal("整段缓冲模式必须带 Content-Length（与旧实现一致）")
	}
}

// TestBodylessRequestsSendContentLengthZero 对齐旧实现：urllib 对空 body 也传
// data=b""，于是 GET/HEAD/DELETE 上游同样收到 Content-Length: 0。
func TestBodylessRequestsSendContentLengthZero(t *testing.T) {
	fp := newFakeProxy(t, okResponder)
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	for _, method := range []string{"GET", "HEAD", "DELETE", "POST", "PUT"} {
		resp, _ := doReq(t, srv.URL, method, "/v1/models", "", nil)
		if resp.StatusCode != 200 {
			t.Fatalf("%s 应 200，得到 %d", method, resp.StatusCode)
		}
		got := fp.last(t)
		if got.method != method {
			t.Fatalf("上游收到的方法应为 %s，得到 %s", method, got.method)
		}
		if cl := got.header.Get("Content-Length"); cl != "0" {
			t.Errorf("%s 无 body 时上游应收到 Content-Length: 0，得到 %q（头：%v）", method, cl, got.header)
		}
	}
}

// TestBadJSONMessagesMatchPython 锁定 400 内层文案与 Python json 模块一致。
func TestBadJSONMessagesMatchPython(t *testing.T) {
	fp := newFakeProxy(t, okResponder)
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	cases := []struct {
		body string
		want string
	}{
		{`{not json`, `{"type": "error", "error": {"message": "bad request: Expecting property name enclosed in double quotes: line 1 column 2 (char 1)"}}`},
		{`{"a"`, `{"type": "error", "error": {"message": "bad request: Expecting ':' delimiter: line 1 column 5 (char 4)"}}`},
		{`{"a":1} extra`, `{"type": "error", "error": {"message": "bad request: Extra data: line 1 column 9 (char 8)"}}`},
		{`not json`, `{"type": "error", "error": {"message": "bad request: Expecting value: line 1 column 1 (char 0)"}}`},
		// 解析成功但非对象：旧实现调用 payload.get 抛 AttributeError。
		{`[1,2]`, `{"type": "error", "error": {"message": "bad request: 'list' object has no attribute 'get'"}}`},
		{`"str"`, `{"type": "error", "error": {"message": "bad request: 'str' object has no attribute 'get'"}}`},
	}
	for _, c := range cases {
		resp, body := doReq(t, srv.URL, "POST", "/v1/chat/completions", c.body, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body=%q 应 400，得到 %d", c.body, resp.StatusCode)
			continue
		}
		if body != c.want {
			t.Errorf("body=%q 错误体不一致：\n got=%s\nwant=%s", c.body, body, c.want)
		}
	}
}

func TestUnsupportedMethodReturns501(t *testing.T) {
	fp := newFakeProxy(t, okResponder)
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	resp, _ := doReq(t, srv.URL, "PATCH", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("未支持方法应 501，得到 %d", resp.StatusCode)
	}
	if fp.count() != 0 {
		t.Fatal("501 不应转发到上游")
	}
}

func TestLogLineFormat(t *testing.T) {
	fp := newFakeProxy(t, okResponder)
	srv, buf := newTestProxy(t, &config.Config{}, fp)

	doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
	doReq(t, srv.URL, "GET", "/v1/models", "", nil)

	re := regexp.MustCompile(`(?m)^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\] (POST|GET) \S+ model=\S+$`)
	lines := re.FindAllString(buf.String(), -1)
	if len(lines) != 2 {
		t.Fatalf("日志格式不符，原始内容：\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "model=muse-spark-1.3-contributor") {
		t.Fatalf("日志缺少模型名：\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "model=None") {
		t.Fatalf("无 body 请求应记 model=None（对齐旧实现）：\n%s", buf.String())
	}
}

func TestConcurrentForwarding(t *testing.T) {
	fp := newFakeProxy(t, okResponder)
	srv, _ := newTestProxy(t, &config.Config{}, fp)

	var wg sync.WaitGroup
	errs := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, _ := doReq(t, srv.URL, "POST", "/v1/chat/completions", `{"model":"muse-spark-1.3-contributor"}`, nil)
			if resp.StatusCode != 200 {
				errs <- resp.Status
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("并发请求出现非 200：%s", e)
	}
	if fp.count() != 20 {
		t.Fatalf("应转发 20 次，实际 %d", fp.count())
	}
}
