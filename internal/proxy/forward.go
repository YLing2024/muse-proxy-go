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

// forward 把请求转发到上游并把响应回传。转发期错误（不可达/超时等）与上游 4xx/5xx
// 都归为 502，message 前缀 upstream failed（复刻旧实现，见 §2.6）。
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

	// 复刻旧实现：urllib 的 HTTPErrorProcessor 对**所有非 2xx/3xx** 抛 HTTPError，
	// 只有 2xx 会被 `opener.open()` 正常返回；3xx 里带 Location 的由
	// HTTPRedirectHandler 跟随（Go 客户端同理由 Transport 跟随），而**没被跟随的
	// 3xx**（无 Location，或 300/304 等不跟随的码）同样落到错误处理器，被外层
	// `except` 吞掉后统一 _reply(502)，文案为
	// `upstream failed: HTTP Error <code>: <reason>`。
	//
	// 因此这里把「到达本层的非 2xx」一律回 502（不是透传），只有 2xx 才照旧回传；
	// 能到达本层的 3xx 一定没被 Go 客户端跟随，与旧实现一致。
	//
	// 该判定必须发生在任何响应头落盘之前，流式模式下同样如此：能到这里说明上游
	// 尚未提交 2xx，故流式档也把非 2xx 变 502，与缓冲档一致。
	if resp.StatusCode >= http.StatusMultipleChoices {
		httpx.WriteError(w, http.StatusBadGateway, upstreamHTTPErrorMessage(resp))
		return
	}

	if p.stream {
		p.streamResponse(w, resp)
		return
	}
	p.bufferedResponse(w, resp)
}

// upstreamHTTPErrorMessage 复刻旧实现把上游非 2xx（4xx/5xx，以及未被跟随的 3xx）
// 变成 502 时的内层文案：`upstream failed: HTTP Error <code>: <reason>`。
// reason 优先取上游状态行里的原样短语，仅在上游未给短语时回落到标准表；
// 取值逻辑见 httpx.StatusLineReason。
func upstreamHTTPErrorMessage(resp *http.Response) string {
	return "upstream failed: HTTP Error " + strconv.Itoa(resp.StatusCode) + ": " +
		httpx.StatusLineReason(resp.StatusCode, resp.Status)
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
