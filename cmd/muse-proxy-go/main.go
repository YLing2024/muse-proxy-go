// Command muse-proxy-go 是 muse-spark 专属转发代理的 Go 重写版。
//
// 它只放行白名单模型，把下游请求的凭证换成真实上游 key，强制经出口代理
// （默认 mihomo 的 127.0.0.1:7890）转发到上游，并原样回传响应（含流式）。
// 旧实现 /usr/local/bin/muse-proxy.py 是功能权威，本版逐条对齐；唯一有意差异是
// MUSE_STREAM=1 时的真流式透传（默认关闭，保持旧版整段缓冲语义）。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"muse-proxy-go/internal/config"
	"muse-proxy-go/internal/key"
	"muse-proxy-go/internal/logx"
	"muse-proxy-go/internal/proxy"
)

func main() {
	if err := run(os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败：%v\n", err)
		os.Exit(1)
	}
}

// run 装配并运行服务；get 便于测试注入环境变量。凭证在任何分支都不会被打印。
func run(get func(string) string) error {
	logger := logx.NewLogger(os.Stdout)

	cfg, err := config.Load(get)
	if err != nil {
		return err
	}
	upstreamKey, err := key.Load(cfg.EnvKey, cfg.PIModelsPath, cfg.AuthJSONPath)
	if err != nil {
		return err
	}
	handler, err := proxy.New(cfg, upstreamKey, logger)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
		// net/http 的错误输出（如响应写入冲突）也走 slog，不引入额外的访问日志。
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	mode := "buffered"
	if cfg.Stream {
		mode = "stream"
	}
	logger.Info(fmt.Sprintf("muse-spark proxy listening on %s -> %s via %s (%s)", cfg.ListenAddr, cfg.Upstream, cfg.ProxyURL, mode))

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	logger.Info("muse-spark proxy stopped")
	return nil
}
