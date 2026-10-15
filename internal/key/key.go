// Package key 按固定优先级装载上游真实凭证，保证凭证不进日志、错误信息与响应。
//
// 优先级（与需求书 §2.9 一致）：
//  1. 环境变量 MUSE_UPSTREAM_KEY（便于脱离本机部署）；
//  2. PI_MODELS_JSON 的 providers["opencode-go"]["apiKey"]，以 ! 开头的占位值视为无效；
//  3. OPENCODE_AUTH_JSON 的 ["opencode-go"]["key"]。
package key

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// Load 依次尝试三个来源，返回第一个有效凭证。
//
// 三条路径由调用方（config）解析好并传入，本包不读取环境变量，便于测试注入临时文件。
// 返回的错误只描述「找不到」，绝不包含凭证内容或文件正文。
func Load(envKey, piModelsPath, authJSONPath string) (string, error) {
	if k := strings.TrimSpace(envKey); k != "" {
		return k, nil
	}
	if k, ok := fromPIModels(piModelsPath); ok {
		return k, nil
	}
	if k, ok := fromAuthJSON(authJSONPath); ok {
		return k, nil
	}
	return "", errors.New("未找到可用的上游凭证：设置 MUSE_UPSTREAM_KEY，或提供含 opencode-go 的 PI_MODELS_JSON / OPENCODE_AUTH_JSON")
}

// fromPIModels 读取 pi 的 models.json；文件缺失、结构不符或 apiKey 为空/占位时都返回 ok=false 继续回退。
func fromPIModels(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var doc struct {
		Providers map[string]struct {
			APIKey string `json:"apiKey"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", false
	}
	k := doc.Providers["opencode-go"].APIKey
	if k == "" || strings.HasPrefix(k, "!") {
		return "", false
	}
	return k, true
}

// fromAuthJSON 读取 opencode 的 auth.json；缺失/不符/空值时返回 ok=false。
func fromAuthJSON(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var doc map[string]struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", false
	}
	k := doc["opencode-go"].Key
	if k == "" {
		return "", false
	}
	return k, true
}
