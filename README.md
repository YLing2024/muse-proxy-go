# muse-proxy-go

muse-spark 专属转发代理的 Go 重写版。只放行两个 muse-spark 模型，把下游请求的凭证换成
真实上游 key，**强制经出口代理**转发到上游，并原样回传响应（含流式）。

- 单二进制、零第三方依赖、无数据库、无状态。
- 默认只监听 `127.0.0.1:19287`，与旧 Python 版（`127.0.0.1:8787`）**并存**。
- 行为逐条对齐旧实现；唯一有意差异是 `MUSE_STREAM=1` 的真流式透传（默认关闭）。

## 快速开始

```sh
make build                          # 产出 ./muse-proxy-go
MUSE_UPSTREAM=https://example.invalid/zen/go/v1 \
MIXED_PROXY=http://127.0.0.1:7890 \
MUSE_UPSTREAM_KEY=... \
./muse-proxy-go
```

真实部署的 key 走环境变量 `MUSE_UPSTREAM_KEY`，或本机的 pi / opencode 配置文件（见下）。

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `MUSE_LISTEN` | `127.0.0.1:19287` | 监听地址，必须是回环地址。 |
| `MUSE_UPSTREAM` | 自带 `/v1` 的上游 base（见 `internal/config`） | 上游地址。 |
| `MIXED_PROXY` | `http://127.0.0.1:7890` | 强制出口代理；不可达即 `502`，不直连。 |
| `MUSE_TIMEOUT` | `120s` | 连接 / 握手 / 响应头阶段超时。 |
| `MUSE_STREAM` | 关闭 | `1` 打开真流式透传。 |
| `MUSE_UPSTREAM_KEY` | 空 | 上游真实 key，优先级最高。 |
| `PI_MODELS_JSON` | `/root/.pi/agent/models.json` | key 回退来源（`providers["opencode-go"]["apiKey"]`，`!` 开头无效）。 |
| `OPENCODE_AUTH_JSON` | `/root/.local/share/opencode/auth.json` | key 回退来源（`["opencode-go"]["key"]`）。 |

## 开发

```sh
make check     # 格式 + 路由↔文档机检 + go vet + go test
make race      # 并发转发用例（-race）
make tools     # 构建验收用假上游 / 假代理
```

## 文档

- `docs/API.md`：对外 HTTP 契约（端点、错误码、环境变量）。
- `docs/COMPAT.md`：与旧实现的逐条对齐、不可逐字节一致的边界、唯一有意差异。
- `docs/ACCEPTANCE.md`：需求书 §6 十条验收的可复现命令与真实输出。
- `AGENTS.md`：维护约定。

## 许可

MIT，见 `LICENSE`。旧项目没有 LICENSE，按重写工程统一采用 MIT。
