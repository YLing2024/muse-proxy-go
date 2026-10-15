package logx

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestLineFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)
	logger.Info("POST /v1/chat/completions model=muse-spark-1.3-contributor")

	re := regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\] POST /v1/chat/completions model=muse-spark-1\.3-contributor\n$`)
	if !re.MatchString(buf.String()) {
		t.Fatalf("日志格式不符：%q", buf.String())
	}
}

func TestConcurrentLinesDoNotInterleave(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("GET /v1/models model=None")
		}()
	}
	wg.Wait()
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if !strings.HasSuffix(line, "GET /v1/models model=None") {
			t.Fatalf("出现被交错的半行：%q", line)
		}
	}
}
