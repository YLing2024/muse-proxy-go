package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

	"muse-proxy-go/internal/httpx"
)

// droppedResponseHeaders 是旧实现明确丢弃的上游响应头。
// transfer-encoding 由本进程按模式决定，content-length 自算，connection 不跨跳。
var droppedResponseHeaders = map[string]bool{
	"transfer-encoding": true,
	"connection":        true,
	"content-length":    true,
}

// forward 把请求转发到上游并把响应回传。任何转发期错误都归为 502，message 前缀 upstream failed。
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, target string, body []byte) {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, reader)
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "upstream failed: "+err.Error())
		return
	}
	for name, values := range r.Header {
		if droppedRequestHeaders[strings.ToLower(name)] || len(values) == 0 {
			continue
		}
		// 旧实现把请求头收进 dict，同名头只保留最后一个值；这里保持同样语义。
		req.Header.Set(name, values[len(values)-1])
	}
	// 客户端自带的 Authorization 已在上一步丢弃，这里一律覆盖为真实 key。
	req.Header.Set("Authorization", "Bearer "+p.key)

	// 旧实现对空 body 也传 data=b""，urllib 因此对 GET/HEAD/DELETE 同样发出
	// Content-Length: 0；Go 的传输层不会为这些方法补该头（见 net/http
	// transferWriter.shouldSendContentLength），故显式补上。
	//
	// 这里用小写键写入 Header map：Request.write 的排除表只匹配规范化的
	// "Content-Length"，小写键会像普通头一样被原样写出，等价于补发该头；
	// 对会由传输层自行写 0 的 POST/PUT/PATCH 则不补，避免重复。
	if len(body) == 0 && !transportWritesEmptyContentLength(r.Method) {
		req.Header["content-length"] = []string{"0"}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "upstream failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if p.stream {
		p.streamResponse(w, resp)
		return
	}
	p.bufferedResponse(w, resp)
}

// bufferedResponse 是默认模式：整段读完后自设 Content-Length 回传（与旧实现可观测行为一致）。
func (p *Proxy) bufferedResponse(w http.ResponseWriter, resp *http.Response) {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "upstream failed: "+err.Error())
		return
	}
	copyResponseHeaders(w, resp)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(data)
}

// streamResponse 是 MUSE_STREAM=1 的增强：不设 Content-Length，边收边发（chunked）。
func (p *Proxy) streamResponse(w http.ResponseWriter, resp *http.Response) {
	copyResponseHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	// 先 flush 一次，强制以 chunked 提交响应头，避免小响应被 net/http 自动补上 Content-Length。
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// transportWritesEmptyContentLength 报告 net/http 传输层是否已会为无 body 的
// 该方法写出 Content-Length: 0。POST/PUT/PATCH 属于这类，其余方法不会。
func transportWritesEmptyContentLength(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

// copyResponseHeaders 透传上游响应头，只丢弃 droppedResponseHeaders 列出的三个。
func copyResponseHeaders(w http.ResponseWriter, resp *http.Response) {
	for name, values := range resp.Header {
		if droppedResponseHeaders[strings.ToLower(name)] {
			continue
		}
		for _, v := range values {
			w.Header().Add(name, v)
		}
	}
}
