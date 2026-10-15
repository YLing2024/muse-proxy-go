package key

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile 写临时文件并返回路径。
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写临时文件失败：%v", err)
	}
	return path
}

func TestEnvKeyHasHighestPriority(t *testing.T) {
	dir := t.TempDir()
	pi := writeFile(t, dir, "models.json", `{"providers":{"opencode-go":{"apiKey":"from-pi"}}}`)
	auth := writeFile(t, dir, "auth.json", `{"opencode-go":{"key":"from-auth"}}`)

	got, err := Load("from-env", pi, auth)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if got != "from-env" {
		t.Fatalf("env 应优先，得到 %q", got)
	}
}

func TestPIModelsUsedWhenEnvEmpty(t *testing.T) {
	dir := t.TempDir()
	pi := writeFile(t, dir, "models.json", `{"providers":{"opencode-go":{"apiKey":"from-pi"}}}`)

	got, err := Load("", pi, filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if got != "from-pi" {
		t.Fatalf("应取 pi 的 apiKey，得到 %q", got)
	}
}

func TestPlaceholderPIFallsThroughToAuth(t *testing.T) {
	dir := t.TempDir()
	pi := writeFile(t, dir, "models.json", `{"providers":{"opencode-go":{"apiKey":"!placeholder"}}}`)
	auth := writeFile(t, dir, "auth.json", `{"opencode-go":{"key":"from-auth"}}`)

	got, err := Load("", pi, auth)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if got != "from-auth" {
		t.Fatalf("以 ! 开头的 pi 值视为无效，应回退 auth，得到 %q", got)
	}
}

func TestMissingAllReturnsErrorWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	pi := writeFile(t, dir, "models.json", `{"providers":{}}`)
	auth := writeFile(t, dir, "auth.json", `{"opencode-go":{}}`)

	_, err := Load("", pi, auth)
	if err == nil {
		t.Fatal("没有任何来源时应报错")
	}
	if strings.Contains(err.Error(), "from-") {
		t.Fatalf("错误信息不应包含任何凭证片段：%v", err)
	}
}

func TestBrokenJSONDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	pi := writeFile(t, dir, "models.json", `{not json`)
	auth := writeFile(t, dir, "auth.json", `{"opencode-go":{"key":"from-auth"}}`)

	got, err := Load("", pi, auth)
	if err != nil {
		t.Fatalf("坏 JSON 应被跳过而非报错：%v", err)
	}
	if got != "from-auth" {
		t.Fatalf("应回退到 auth，得到 %q", got)
	}
}
