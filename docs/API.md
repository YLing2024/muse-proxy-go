# muse-proxy-go 转发代理 API（权威契约）

本文件是 muse-proxy-go 对外 HTTP 接口的权威契约。**改了路由不改本文件 = 未完成**：
路由在 `internal/proxy/routes.go` 的 `routeTable` 里以 `{method: "GET", path: "/*"}` 字面量登记，
`scripts/check-api-docs.mjs` 会机检「源码登记 ↔ 本文 `### METHOD /path` 标题」一致，并随 `make check` 一起跑。

本服务是 muse-spark 专属转发代理：只放行白名单模型，把下游请求的凭证换成真实上游 key，
**强制经 mihomo（默认 `127.0.0.1:7890` mixed）出口**转发到上游，原样回传响应。
行为逐条对齐旧实现 `/usr/local/bin/muse-proxy.py`（Python 标准库单文件）；差异见 `docs/COMPAT.md`。

- 默认监听 `127.0.0.1:19287`（旧版 8787 并存不动）。只允许回环地址，非回环在启动时被拒绝。
- 请求/响应体原样透传，不做 charset 改写；错误信封为 UTF-8 JSON。
- 示例统一用 `127.0.0.1` / `example.com`；下文的 `$P` 表示代理基址（默认 `http://127.0.0.1:19287`）。

## 1. 认证模式

本服务**没有客户端鉴权**（与旧实现一致）：任何能访问该端口的调用方都会被转发，因此只监听回环地址。
`AUTH_MODE` 约定**不适用**（本服务无管理面、无数据库、无状态）。

- 客户端自带的 `Authorization` 一律被**丢弃**，转发时替换为真实上游 key（`Bearer <key>`）；
  客户端凭证不会外泄到上游。
- 真实 key 的读取优先级（进程启动时读一次）：
  1. 环境变量 `MUSE_UPSTREAM_KEY`；
  2. `PI_MODELS_JSON`（默认 `/root/.pi/agent/models.json`）的
     `providers["opencode-go"]["apiKey"]`，**以 `!` 开头的占位值视为无效**并继续回退；
  3. `OPENCODE_AUTH_JSON`（默认 `/root/.local/share/opencode/auth.json`）的 `["opencode-go"]["key"]`。
- **key 绝不出现在任何日志、错误信息、HTTP 响应或仓库文件中**。启动日志只打印监听地址、上游 base
  与出口代理地址，不含 key。

## 2. 数据模型约定

- **无持久化、无状态**：本服务不写库、不落盘，进程重启不影响语义；因此 ID 类型、时间格式、
  枚举取值等数据模型约定不适用。
- 唯一的响应体结构是错误信封：

  | 字段 | 类型 | 说明 |
  |---|---|---|
  | `type` | string | 固定 `"error"` |
  | `error.message` | string | 错误说明 |

  序列化与旧实现 `json.dumps` 逐字节一致（`{"type": "error", "error": {"message": "..."}}`，
  逗号/冒号后各一个空格）。

- 流式（`MUSE_STREAM=1`）时上游响应的 `Content-Type`（如 `text/event-stream`）原样透传。

## 3. 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `MUSE_LISTEN` | `127.0.0.1:19287` | 监听地址。必须是回环地址，否则启动失败。 |
| `MUSE_UPSTREAM` | 见 `internal/config/config.go` 的 `DefaultUpstream`（自带 `/v1`） | 上游 base。 |
| `MIXED_PROXY` | `http://127.0.0.1:7890` | 强制出口代理；必须可达，本服务不直连上游。 |
| `MUSE_TIMEOUT` | `120s` | 连接 / TLS 握手 / 上游响应头阶段超时（Go duration）。 |
| `MUSE_STREAM` | 关闭 | `1` 打开真流式透传，其它值/留空为整段缓冲（旧实现语义）。 |
| `MUSE_UPSTREAM_KEY` | 空 | 上游真实 key，优先级最高。 |
| `PI_MODELS_JSON` | `/root/.pi/agent/models.json` | key 的第一回退来源。 |
| `OPENCODE_AUTH_JSON` | `/root/.local/share/opencode/auth.json` | key 的第二回退来源。 |

## 4. 端点

路径处理对所有端点一致：**去掉请求路径的前导 `/v1`** 后拼到上游 base（上游 base 自带 `/v1`，不重复）。
`/v1/chat/completions` → `<UPSTREAM>/chat/completions`；不带 `/v1` 的路径原样拼；query 原样保留。

白名单校验只对**带 body** 的请求生效（带 `Content-Length` 且大于 0）：解析 JSON 取 `model`。
无 body 的请求（如 `GET /models`）直接放行，不校验。

### GET /*

- 请求参数：无固定参数；path/query 原样转发。
- 鉴权：无（见 §1）。
- 行为：无 body → 直接转发。
- 示例：`curl -sS "$P/v1/models"`
- 成功响应：上游 `2xx` 状态码与响应体原样回传；带 `Location` 的 `3xx` 由客户端跟随、取最终响应；
  上游 `4xx/5xx` 以及**未带 `Location` 的 `3xx`** → `502`（见 §5）。
- 副作用：一次上游请求。

### POST /*

- 请求参数：body 为 JSON，需含 `model`（仅当 body 非空时校验）。
- 鉴权：无。
- 行为：body 非空时校验 `model` 是否在白名单
  （`muse-spark-1.2-contributor`、`muse-spark-1.3-contributor`）；
  不在白名单 → `403`；body 不是合法 JSON → `400`。
- 示例：`curl -sS "$P/v1/chat/completions" -H 'Content-Type: application/json' -d '{"model":"muse-spark-1.3-contributor","messages":[{"role":"user","content":"hi"}]}'`
- 成功响应：上游 `2xx` 状态码与响应体原样回传（常见 `200`）；带 `Location` 的 `3xx` 由客户端跟随、
  取最终响应；上游 `4xx/5xx` 以及**未带 `Location` 的 `3xx`** → `502`（见 §5）。
- 错误响应：见 §5。
- 副作用：一次上游请求。

### PUT /*

- 行为与 `POST /*` 相同（同处理器、同样的 body 白名单校验）。
- 示例：`curl -sS -X PUT "$P/v1/chat/completions" -d '{"model":"muse-spark-1.3-contributor"}'`

### DELETE /*

- 行为：与其它方法同处理器；若带 body 同样做白名单校验，无 body 直接转发。
- 示例：`curl -sS -X DELETE "$P/v1/models/x"`

### HEAD /*

- 行为：与 GET 相同，但上游响应体为空；缓冲模式下回传 `Content-Length: 0`（对齐旧实现）。
- 示例：`curl -sS -I "$P/v1/models"`

> 未登记的方法（如 `PATCH`、`OPTIONS`、`CONNECT`）一律 `501`，且不转发到上游。

## 5. 错误码表

| 状态码 | 触发条件 | 响应体 |
|---|---|---|
| `400` | 请求带 body 但不是合法 JSON，或 JSON 不是对象 | `{"type": "error", "error": {"message": "bad request: <解析信息>"}}` |
| `403` | body 的 `model` 不在白名单（含缺 `model`、`model` 非字符串） | `{"type": "error", "error": {"message": "model <m> not allowed through proxy"}}`，与旧实现逐字节一致 |
| `501` | 方法不在路由表内 | 纯文本 `unsupported method`（对齐旧实现的状态码语义） |
| `502` | 上游/出口代理不可达、超时等转发期出错；**或上游返回 4xx/5xx；或上游返回未带 `Location` 的 3xx；或上游重定向成环（含超过跟随上限）** | `{"type": "error", "error": {"message": "upstream failed: <错误信息>"}}`；中间两种 `<错误信息>` 为 `HTTP Error <code>: <reason>`，重定向环见下（复刻旧实现，见下） |
| `2xx` | 上游最终返回的成功状态码 | 原样透传上游响应体与状态码 |
| `3xx`（带 `Location`） | 上游返回 `Location`，由客户端跟随重定向（非成环时） | 跟随后的最终响应（状态码/体/头）原样回传 |

- **上游 4xx/5xx、未带 `Location` 的 3xx 的文案**：`upstream failed: HTTP Error <code>: <reason>`。
  `<reason>` **优先取上游状态行里的原样短语**（旧实现 `urllib` 的 `HTTPError` 文案即取此处，
  如上游写 `HTTP/1.1 413 Nope` 则文案为 `... 413: Nope`）；**仅当**上游未给短语时（HTTP/2 无
  reason，或状态行只写到码）才回落到标准表 Python `http.client.responses`（如 `400 Bad Request`、
  `404 Not Found`、`429 Too Many Requests`、`503 Service Unavailable`，未收录的码用 `Unknown`）。
  这是复刻旧实现（`urllib` 对非 2xx 抛 `HTTPError` 后被 `except` 吞成 502）；缓冲与
  `MUSE_STREAM=1` 两档行为一致。完整边界见 `docs/COMPAT.md` §2.1 / §2.3。
- **重定向环的文案**：上游把请求导进环（同一 URL 跟随超过 4 次，或累计跟随超过 10 个
  不同 URL）时，回 `502`，`<错误信息>` 逐字节复刻旧实现 `urllib`，形如
  `HTTP Error 302: The HTTP server returned a redirect error that would lead to an infinite loop.`
  换行 `The last 30x error message was:` 换行 `<reason>`（`<reason>` 取值同上；`<code>` 取
  触发判环那次 3xx 的状态码）。非环的普通 `3xx`（带 `Location`）仍照旧跟随，不受影响；
  详见 `docs/COMPAT.md` §2.4。
- 上游响应的响应头（除 `Transfer-Encoding` / `Connection` / `Content-Length`）原样透传；
  缓冲模式自算 `Content-Length`，流式模式改为 `chunked`。
- `<解析信息>` 与 `<错误信息>` 中非 HTTP 类的具体文本由 Go 运行时产生，与 Python 版本的措辞不同
  （详见 `docs/COMPAT.md`）；外层信封与各状态码语义保持一致。

## 6. 副作用 / 幂等 / 限流 / 超时

- **副作用**：每个请求产生一次对上游的 HTTP 调用；本服务自身无写库、无落盘、无消息。
- **幂等性**：由上游接口语义决定，本服务不做去重。
- **限流**：无（与旧实现一致）。
- **超时**：`MUSE_TIMEOUT`（默认 120 秒）作用于连接 / TLS 握手 / 上游响应头阶段；超时归为 `502`。
- **并发**：`net/http` 每请求一个 goroutine，可并发处理（旧实现是 `ThreadingHTTPServer`）。

## 7. 有意差异

与旧实现的唯一有意差异是 `MUSE_STREAM=1` 的真流式透传（默认关闭）。完整清单见 `docs/COMPAT.md`。
