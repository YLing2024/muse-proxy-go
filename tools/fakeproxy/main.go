// Command fakeproxy 是验收用的本地假出口代理：证明 muse-proxy-go 的请求确实经代理出去。
//
// 它支持两种形态：
//   - 绝对 URI 的普通请求（目标为 http 上游）：记录后转发到目标并回传响应；
//   - CONNECT 隧道（目标为 https 上游）：记录后建立双向隧道。
//
// 每次收到请求都会打印一行 "fake-proxy ..."，验收时 grep 该行即可证明流量经过代理。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "监听地址")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("监听失败：%v", err)
	}
	fmt.Printf("fake-proxy listening on %s\n", ln.Addr())

	transport := &http.Transport{Proxy: nil}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			fmt.Printf("fake-proxy CONNECT %s\n", r.Host)
			tunnel(w, r)
			return
		}
		fmt.Printf("fake-proxy %s %s\n", r.Method, r.URL.String())
		proxyAbsolute(w, r, transport)
	})

	srv := &http.Server{Handler: handler}
	if err := srv.Serve(ln); err != nil {
		log.Fatalf("服务退出：%v", err)
	}
}

// proxyAbsolute 处理绝对 URI 的普通转发请求。
func proxyAbsolute(w http.ResponseWriter, r *http.Request, transport *http.Transport) {
	outReq, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	for name, values := range r.Header {
		for _, v := range values {
			outReq.Header.Add(name, v)
		}
	}
	resp, err := transport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for name, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(name, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
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

// tunnel 处理 CONNECT，建立到目标的双向字节流。
func tunnel(w http.ResponseWriter, r *http.Request) {
	target, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer target.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	go func() { _, _ = io.Copy(target, client) }()
	_, _ = io.Copy(client, target)
}
