// Package proxy 实现 muse-spark 专属转发代理：白名单校验、凭证替换、强制经出口代理
// 转发到上游、响应原样回传（默认整段缓冲，MUSE_STREAM=1 时真流式）。
package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"muse-proxy-go/internal/config"
	"muse-proxy-go/internal/httpx"
)

// allowedModels 是唯一允许穿过代理的模型集合，与旧实现 ALLOWED_MODELS 一致。
var allowedModels = map[string]bool{
	"muse-spark-1.2-contributor": true,
	"muse-spark-1.3-contributor": true,
}

// droppedRequestHeaders 是旧实现明确丢弃的请求头（小写比较）。
// host/authorization 由本进程重设，content-length 由转发层按 body 重算，connection 不跨跳。
var droppedRequestHeaders = map[string]bool{
	"host":           true,
	"content-length": true,
	"connection":     true,
	"authorization":  true,
}

// Proxy 是无状态转发处理器，可安全并发复用。
type Proxy struct {
	upstream string
	key      string
	stream   bool
	client   *http.Client
	logger   *slog.Logger
}

// New 装配转发器。出口代理用 http.Transport.Proxy 强制指定，绝不会直连上游。
func New(cfg *config.Config, upstreamKey string, logger *slog.Logger) (*Proxy, error) {
	proxyURL, err := url.Parse(cfg.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("解析出口代理地址失败：%w", err)
	}
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: (&net.Dialer{
			Timeout:   cfg.Timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		// 旧实现固定 HTTP/1.1，且不透明解压；两者都要保持，否则会改变上游可见行为。
		ForceAttemptHTTP2:     false,
		DisableCompression:    true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   cfg.Timeout,
		ResponseHeaderTimeout: cfg.Timeout,
		ExpectContinueTimeout: time.Second,
	}
	return &Proxy{
		upstream: cfg.Upstream,
		key:      upstreamKey,
		stream:   cfg.Stream,
		client:   &http.Client{Transport: transport},
		logger:   logger,
	}, nil
}

// ServeHTTP 是唯一的请求入口，五个方法共用它。
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !routes.allows(r.Method) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = io.WriteString(w, "unsupported method\n")
		return
	}

	targetPath := stripV1(requestURI(r))
	body, err := readRequestBody(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}

	model, status, err := checkModel(body)
	if err != nil {
		httpx.WriteError(w, status, err.Error())
		return
	}

	// 旧实现：无 model 时日志打印 model=None。这里逐字符对齐。
	logModel := "None"
	if model != "" {
		logModel = model
	}
	p.logger.Info(fmt.Sprintf("%s %s model=%s", r.Method, targetPath, logModel))

	p.forward(w, r, p.upstream+targetPath, body)
}

// checkModel 仅对「带 body」的请求做白名单校验，返回 (model, 0, nil) 表示放行。
// 无 body 的请求（如 GET /models）直接放行，model 为空串。
//
// 解析走 httpx.PyDecode（移植自 CPython），以保证 400 文案与旧实现逐字节一致。
func checkModel(body []byte) (model string, status int, err error) {
	if len(body) == 0 {
		return "", 0, nil
	}
	payload, perr := httpx.PyDecode(body)
	if perr != nil {
		return "", http.StatusBadRequest, fmt.Errorf("bad request: %s", perr.Error())
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		// 旧实现直接 payload.get(...)，非对象会抛 AttributeError，同样走 400。
		return "", http.StatusBadRequest, fmt.Errorf("bad request: %s", httpx.PyAttrError(payload))
	}
	raw := obj["model"]
	name, ok := raw.(string)
	if !ok || !allowedModels[name] {
		return "", http.StatusForbidden, fmt.Errorf("model %s not allowed through proxy", httpx.PyStr(raw))
	}
	return name, 0, nil
}

// readRequestBody 复刻旧实现：只按 Content-Length 读取；无 Content-Length（如 chunked）
// 时 body 视为空、且不消费请求流。
func readRequestBody(r *http.Request) ([]byte, error) {
	if r.ContentLength <= 0 {
		return nil, nil
	}
	buf := make([]byte, r.ContentLength)
	if _, err := io.ReadFull(r.Body, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// stripV1 去掉路径前导的 /v1，避免与上游 base 自带的 /v1 重复（对齐旧实现）。
func stripV1(path string) string {
	if strings.HasPrefix(path, "/v1") {
		return path[len("/v1"):]
	}
	return path
}

// requestURI 取原始请求目标（含 query）；RequestURI 为空时回退到 URL 重建结果。
func requestURI(r *http.Request) string {
	if r.RequestURI != "" {
		return r.RequestURI
	}
	return r.URL.RequestURI()
}
