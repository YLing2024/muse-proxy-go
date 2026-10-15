// Package logx 提供符合旧实现访问日志格式的 slog.Handler。
//
// 需求要求「日志用标准库 log/slog」，同时又要求每请求一行、格式为
// [<ISO8601 秒精度>] <METHOD> <去 /v1 后的路径> model=<model>。标准 slog 的
// Text/JSON handler 都会带上 attrs 与带时区的时间戳，无法满足该格式，因此这里
// 用自定义 Handler 复刻旧输出，并保持并发写不串行。
package logx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// timeLayout 对齐 Python datetime.isoformat(timespec="seconds")：本地时间、无时区、秒精度。
const timeLayout = "2006-01-02T15:04:05"

// NewLogger 返回把每条记录渲染成「[时间] 消息」的 stdout 日志器。
func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(&lineHandler{w: w, mu: &sync.Mutex{}})
}

// lineHandler 忽略 attrs 与 level，只按旧格式输出时间与消息。
type lineHandler struct {
	w  io.Writer
	mu *sync.Mutex
}

// Enabled 对所有级别都放行：本服务只用 Info，级别无需过滤。
func (h *lineHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle 写出单行日志；用互斥锁保证并发请求下不出现半行交错。
func (h *lineHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := fmt.Fprintf(h.w, "[%s] %s\n", r.Time.Format(timeLayout), r.Message)
	return err
}

// WithAttrs 不保留 attrs：本服务的调用方把信息写进消息正文。
func (h *lineHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup 不分组：格式固定，无嵌套需求。
func (h *lineHandler) WithGroup(string) slog.Handler { return h }
