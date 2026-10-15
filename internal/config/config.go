// Package config 从环境变量装载 muse-proxy-go 的运行配置。
//
// 默认值保持与旧实现（/usr/local/bin/muse-proxy.py）一致，仅监听端口按需求书
// 从 8787 改为 19287（新旧并存）。所有取值都可在不改代码的前提下用环境变量覆盖。
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultListenAddr 是新版默认监听地址；旧版 8787 保持不变，两者并存。
	DefaultListenAddr = "127.0.0.1:19287"
	// DefaultUpstream 是上游 base，自带 /v1 前缀。
	DefaultUpstream = "https://opencode.ai/zen/go/v1"
	// DefaultProxyURL 是 mihomo mixed 出口；本服务强制经代理，不允许直连。
	DefaultProxyURL = "http://127.0.0.1:7890"
	// DefaultPIModels 是 pi 的模型配置路径（key 的第一回退来源）。
	DefaultPIModels = "/root/.pi/agent/models.json"
	// DefaultAuthJSON 是 opencode 的凭据路径（key 的第二回退来源）。
	DefaultAuthJSON = "/root/.local/share/opencode/auth.json"
	// DefaultTimeout 是上游连接/握手/响应头阶段的超时，对齐旧实现的 120 秒。
	DefaultTimeout = 120 * time.Second
)

// Config 是进程启动时读一次的不可变运行配置。
type Config struct {
	ListenAddr   string
	Upstream     string
	ProxyURL     string
	Timeout      time.Duration
	Stream       bool
	EnvKey       string
	PIModelsPath string
	AuthJSONPath string
}

// Load 读取并校验配置。get 便于测试注入环境变量，生产传 os.Getenv。
func Load(get func(string) string) (*Config, error) {
	timeout, err := parseTimeout(get("MUSE_TIMEOUT"))
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		ListenAddr:   valueOr(get("MUSE_LISTEN"), DefaultListenAddr),
		Upstream:     valueOr(get("MUSE_UPSTREAM"), DefaultUpstream),
		ProxyURL:     valueOr(get("MIXED_PROXY"), DefaultProxyURL),
		Timeout:      timeout,
		Stream:       get("MUSE_STREAM") == "1",
		EnvKey:       strings.TrimSpace(get("MUSE_UPSTREAM_KEY")),
		PIModelsPath: valueOr(get("PI_MODELS_JSON"), DefaultPIModels),
		AuthJSONPath: valueOr(get("OPENCODE_AUTH_JSON"), DefaultAuthJSON),
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate 保证「只监听回环」与「上游/出口都是合法绝对 URL」两条硬约束在启动时就挡住。
func (c *Config) validate() error {
	if !isLoopbackListen(c.ListenAddr) {
		return fmt.Errorf("监听地址 %q 不是回环地址：本服务只允许监听 127.0.0.1", c.ListenAddr)
	}
	if !isHTTPURL(c.Upstream) {
		return fmt.Errorf("上游地址 %q 不是合法的 http(s) URL", c.Upstream)
	}
	if !isHTTPURL(c.ProxyURL) {
		return fmt.Errorf("出口代理地址 %q 不是合法的 http(s) URL", c.ProxyURL)
	}
	return nil
}

// parseTimeout 解析 MUSE_TIMEOUT；留空用默认值，非法或非正数报错。
func parseTimeout(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("MUSE_TIMEOUT %q 不是合法的 Go duration（如 120s、2s）", raw)
	}
	if d <= 0 {
		return 0, errors.New("MUSE_TIMEOUT 必须为正数")
	}
	return d, nil
}

// isLoopbackListen 判定地址是否为回环监听，避免部署时误绑到对外网卡。
func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isHTTPURL 要求绝对 URL 且带 host，scheme 仅限 http/https。
func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// valueOr 取环境变量，空则回落默认值。
func valueOr(raw, fallback string) string {
	if v := strings.TrimSpace(raw); v != "" {
		return v
	}
	return fallback
}
