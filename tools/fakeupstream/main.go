// Command fakeupstream 是验收用的本地假上游：记录每个请求的路径、请求头与 body，
// 并按约定回包，用于在不起真实上游的前提下验证 muse-proxy-go。
//
// 行为约定（只用于测试，不参与生产部署）：
//   - 默认回 200 + JSON，body 里回显 method/uri/authorization/headers/body；
//   - 带 X-Fake-Status 时按其值回状态码；
//   - 带 X-Fake-Sleep 时先睡指定秒数（用于验证超时分支）；
//   - 带 X-Fake-Stream 时以 SSE 分块、逐块 flush（用于验证流式）。
//
// 注意：它会把收到的 Authorization 原样回显，只能接假 key，绝不接真实凭证。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "监听地址")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("监听失败：%v", err)
	}
	fmt.Printf("fake-upstream listening on %s\n", ln.Addr())

	srv := &http.Server{Handler: http.HandlerFunc(handle)}
	if err := srv.Serve(ln); err != nil {
		log.Fatalf("服务退出：%v", err)
	}
}

func handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	if s := r.Header.Get("X-Fake-Sleep"); s != "" {
		if secs, err := strconv.ParseFloat(s, 64); err == nil {
			time.Sleep(time.Duration(secs * float64(time.Second)))
		}
	}

	if r.Header.Get("X-Fake-Stream") != "" {
		serveStream(w)
		return
	}

	status := http.StatusOK
	if s := r.Header.Get("X-Fake-Status"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			status = n
		}
	}

	rec := map[string]any{
		"method":        r.Method,
		"uri":           r.RequestURI,
		"authorization": r.Header.Get("Authorization"),
		"headers":       headerSubset(r),
		"bodyLen":       len(body),
		"body":          string(body),
	}
	fmt.Printf("fake-upstream %s %s auth=%s\n", r.Method, r.RequestURI, r.Header.Get("Authorization"))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Fake-Upstream", "yes")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rec)
}

// serveStream 以 SSE 形式分块发送，块间停顿，便于观测首字节与逐块到达。
func serveStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	for i := 0; i < 3; i++ {
		fmt.Fprintf(w, "data: chunk-%d\n\n", i)
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(300 * time.Millisecond)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// headerSubset 返回去掉 X-Fake-* 控制头后的请求头。
func headerSubset(r *http.Request) map[string]string {
	out := map[string]string{}
	for name, values := range r.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-fake-") {
			continue
		}
		out[name] = strings.Join(values, ", ")
	}
	return out
}
