package config

import (
	"testing"
	"time"
)

// envMap 返回一个把 map 当作环境变量的 get 函数。
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(envMap(nil))
	if err != nil {
		t.Fatalf("默认配置应加载成功，得到错误：%v", err)
	}
	if cfg.ListenAddr != DefaultListenAddr {
		t.Errorf("ListenAddr = %q，期望 %q", cfg.ListenAddr, DefaultListenAddr)
	}
	if cfg.Upstream != DefaultUpstream {
		t.Errorf("Upstream = %q，期望 %q", cfg.Upstream, DefaultUpstream)
	}
	if cfg.ProxyURL != DefaultProxyURL {
		t.Errorf("ProxyURL = %q，期望 %q", cfg.ProxyURL, DefaultProxyURL)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v，期望 %v", cfg.Timeout, DefaultTimeout)
	}
	if cfg.Stream {
		t.Error("默认应为整段缓冲（Stream=false）")
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(envMap(map[string]string{
		"MUSE_LISTEN":        "127.0.0.1:19999",
		"MUSE_UPSTREAM":      "http://example.com/v1",
		"MIXED_PROXY":        "http://127.0.0.1:1080",
		"MUSE_TIMEOUT":       "2s",
		"MUSE_STREAM":        "1",
		"PI_MODELS_JSON":     "/tmp/pi.json",
		"OPENCODE_AUTH_JSON": "/tmp/auth.json",
	}))
	if err != nil {
		t.Fatalf("覆盖配置应加载成功，得到错误：%v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:19999" || cfg.Upstream != "http://example.com/v1" ||
		cfg.ProxyURL != "http://127.0.0.1:1080" || !cfg.Stream || cfg.Timeout != 2*time.Second {
		t.Fatalf("覆盖值未生效：%+v", cfg)
	}
	if cfg.PIModelsPath != "/tmp/pi.json" || cfg.AuthJSONPath != "/tmp/auth.json" {
		t.Fatalf("凭据路径未生效：%+v", cfg)
	}
}

func TestValidateRejectsNonLoopbackListen(t *testing.T) {
	_, err := Load(envMap(map[string]string{"MUSE_LISTEN": "0.0.0.0:19287"}))
	if err == nil {
		t.Fatal("绑定非回环地址必须被拒绝")
	}
}

func TestValidateRejectsBadURLs(t *testing.T) {
	cases := []map[string]string{
		{"MUSE_UPSTREAM": "not-a-url"},
		{"MIXED_PROXY": "ftp://example.com"},
		{"MIXED_PROXY": "http://"},
	}
	for _, m := range cases {
		if _, err := Load(envMap(m)); err == nil {
			t.Errorf("非法地址 %v 应被拒绝", m)
		}
	}
}

func TestParseTimeoutRejectsBad(t *testing.T) {
	if _, err := Load(envMap(map[string]string{"MUSE_TIMEOUT": "abc"})); err == nil {
		t.Error("非法 duration 应被拒绝")
	}
	if _, err := Load(envMap(map[string]string{"MUSE_TIMEOUT": "0s"})); err == nil {
		t.Error("非正 duration 应被拒绝")
	}
}
