# muse-proxy-go 与旧实现的兼容性说明

旧实现 `/usr/local/bin/muse-proxy.py`（87 行 Python，`http.server` 单文件）是**功能权威**，
本版逐条对齐其可观测行为。本文列出「逐条对齐」的证据与「无法逐字节一致」的边界，
以及唯一有意差异。

## 1. 逐条对齐（需求书 §2 的 12 条）

| # | 行为 | 本版实现 |
|---|---|---|
| 1 | 监听 `127.0.0.1:8787` → 新版 `127.0.0.1:19287` | `MUSE_LISTEN` 默认 `127.0.0.1:19287`，强制回环；旧版不动。 |
| 2 | 去掉前导 `/v1` 再拼上游 base | `stripV1`：`strings.HasPrefix(path, "/v1")` 则裁剪，与旧实现同源；query 保留。 |
| 3 | 白名单三分支 | 见下节「白名单」。`400` 内层文案复刻 Python `json` 模块的 `JSONDecodeError`（行列 / `char` 偏移逐字节一致，见 §2.2）。 |
| 4 | 请求头过滤 + 覆盖 Authorization | 丢弃 `host` / `content-length` / `connection` / `authorization`，其余透传，`Authorization` 覆盖为 `Bearer <key>`；空 body 时向上游补发 `Content-Length: 0`（对齐旧实现 `data=b""`，见 §2.2）。 |
| 5 | 响应头过滤 + 自设 Content-Length | 丢弃 `transfer-encoding` / `connection` / `content-length`，**状态码透传**，缓冲模式自设 `Content-Length`（旧实现实际会把 4xx/5xx 变 502，见 §2.1）。 |
| 6 | 上游失败/超时 → 502 | 转发链路失败（不可达、超时）回 `502`，message 前缀 `upstream failed: `；上游正常返回的 4xx/5xx 不算失败、原样透传。 |
| 7 | 五方法同处理器，其余 501 | `routeTable` 登记 GET/POST/PUT/DELETE/HEAD；未登记方法回 `501` 且不转发。 |
| 8 | 上游超时 120 秒 | `MUSE_TIMEOUT` 默认 `120s`。 |
| 9 | key 读取顺序，`!` 占位无效 | `env → PI_MODELS_JSON → OPENCODE_AUTH_JSON`，`!` 前缀跳过；仅启动读一次。 |
| 10 | 上游/出口默认值，不得直连 | `MUSE_UPSTREAM` 默认见 `internal/config` 的 `DefaultUpstream`；`MIXED_PROXY` 默认 `http://127.0.0.1:7890`；`Transport.Proxy` 恒非空，**不直连**。 |
| 11 | 每请求一行 stdout | `[<ISO8601 秒>] <METHOD> <去 /v1 后的路径> model=<model 或 None>`；禁用框架访问日志。 |
| 12 | HTTP/1.1 + 并发 | `ForceAttemptHTTP2=false`；`net/http` 每请求一 goroutine。 |

### 白名单（需求书 §2.3）

- body 为空（无 `Content-Length` 或为 0）→ **直接放行**，不校验（如 `GET /models`）。
- body 为合法 JSON 对象且 `model` 在白名单 → 放行。
- `model` 不在白名单（含缺 `model`、`model` 非字符串）→ **403**，体为
  `{"type": "error", "error": {"message": "model <m> not allowed through proxy"}}`，
  其中 `<m>` 复刻 Python 的字符串化（缺省为 `None`）。
- body 不是合法 JSON（或 JSON 不是对象、尾部有多余内容）→ **400**，内层文案与旧实现
  （Python `json.loads`）逐字节一致：解析失败复刻 `JSONDecodeError` 的
  `msg: line L column C (char N)`（按 code point 计位）；解析成功但值不是对象时复刻
  `AttributeError`，如 `'list' object has no attribute 'get'`。详见 §2.2。

### 日志（需求书 §2.11）

- 只在白名单校验通过后打印，每请求一行；`403` / `400` / `501` **不打印**（与旧实现一致）。
- 无 `model` 时打印 **`model=None`**：旧实现 f-string 对 `None` 输出字面量 `None`，本版逐字符对齐。
- 时间戳为本地时间、秒精度、无时区（对齐 `datetime.isoformat(timespec="seconds")`）。

## 2. 无法逐字节一致的地方（本质差异，非有意设计）

这些差异来自 Go 与 Python 运行时，**不影响功能与状态码语义**；验收的并排对照只比
状态码 / 响应体 / 关键响应头。

| 项 | 旧实现（Python） | 本版（Go） | 影响 |
|---|---|---|---|
| `502` 内层文案 | `upstream failed: <urlopen error [Errno 111] ...>` | `upstream failed: <Go net/http 错误>` | 前缀与状态码一致，错误文本不同。 |
| `501` 响应体 | `send_error` 生成的 HTML 错误页 | 纯文本 `unsupported method` | 状态码一致，体不同。 |
| 请求头大小写 | `urllib` 会把头名 `capitalize`（如 `Content-type`） | `net/http` 规范化为 `Content-Type` | HTTP 头名大小写不敏感，语义一致。 |
| 同名请求头 | 收进 dict，只保留最后一个值 | 同样只保留最后一个值 | 语义一致（已显式对齐）。 |
| 响应 `Date` / `Server` 头 | 旧实现会同时发自身的与上游的两份 `Date`/`Server` | 只保留上游的 `Date`，不发自身 `Server` | 非关键头，客户端忽略。 |
| 自动 gzip | 不透明解压 | `DisableCompression=true`，同样不透明解压、不擅自加 `Accept-Encoding` | 行为一致（已显式对齐）。 |
| 响应头重复值 | `email.message` 可能合并重复头 | 逐个 `Add` 保留 | 语义一致。 |
| 上游请求头附加项 | `urllib` 额外加 `Accept-Encoding: identity`、`Connection: close`，无 body 时还会加 `Content-Type: application/x-www-form-urlencoded` | 纯透传（只转发客户端实际带的头），空 body 时补 `Content-Length: 0` | 需求书 §2.4 要求「其余原样透传」，故不复制 urllib 的运行时默认头；上游可见的额外头不同。 |
| 重定向 | `urllib` 默认跟随 301/302/303/307 | `net/http` 默认跟随 301/302/303/307/308 | 两者都跟随重定向，边界（跨主机是否携带 Authorization）略有差异。 |

## 2.1 旧实现自身与需求书不一致处（本版按需求书实现）

并行对照时发现一处**旧实现的可观测行为与需求书矛盾**，本版以需求书为准：

- 需求书 §2.5 要求「状态码取上游状态码」、§6.4 要求「上游 5xx 原样回传」。
- 但旧实现用 `urllib.request.OpenerDirector.open()`，其 `HTTPErrorProcessor` 对**所有非 2xx/3xx**
  状态抛 `HTTPError`，被 `except` 捕获后回 **502** `upstream failed: HTTP Error <code>: <reason>`。
  即：旧实现**不会**把上游的 4xx/5xx 透传，而是统一变成 502。
- 本版实现**状态码透传**（`503` 就回 `503`），符合需求书 §2.5 / §6.4。
  并排对照矩阵的 `c7`（上游 `X-Fake-Status: 503`）会显示：
  旧 `502 upstream failed: HTTP Error 503: Service Unavailable`、新 `503` 且响应体为上游内容。

> 这是本次重写发现的旧实现缺陷，不是本版新增的差异；如需与旧版逐字节一致地「把 4xx/5xx 变成 502」，
> 需要产品方确认后另行改需求书。

## 2.2 已对齐的两处旧实现行为（2026-10-16 修复）

并排对照曾把下列两项列为差异，现已按旧实现可观测行为对齐，并各配单测：

| 项 | 旧实现（Python） | 修复前（Go） | 修复后（Go） |
|---|---|---|---|
| 空 body 请求的上游 `Content-Length` | `data=b""` 使 `urllib` 对 GET/HEAD/DELETE 也发 `Content-Length: 0` | 完全不发该头 | 也发 `Content-Length: 0` |
| `400` 内层文案 | `json.loads` 的 `JSONDecodeError` 文案（含真实行列 / `char`） | `encoding/json` 的 Go 文案 | 逐字节复刻 Python 文案 |

`400` 覆盖的形态（`internal/httpx` 移植 CPython 3.13 `Modules/_json.c` 的扫描器，
在 rune 序列上计位）：`Expecting value`、`Expecting property name enclosed in double quotes`、
`Expecting ':' delimiter`、`Expecting ',' delimiter`、`Unterminated string starting at`、
`Extra data`、`Invalid control character at`，以及 `Illegal trailing comma ...`、
`Invalid \escape`、`Invalid \uXXXX escape`；解析成功但非对象时复刻 `AttributeError`
（`'list'` / `'str'` / `'int'` / `'float'` / `'bool'` / `'NoneType'`）。

**未映射形态**：只有带 UTF-16/UTF-32 BOM 的 body（Python `detect_encoding` 会据此选编码）。
UTF-8（含 UTF-8 BOM、以及 `surrogatepass` 允许的代理区编码）已逐字节对齐。

## 3. 唯一有意差异：MUSE_STREAM

- 默认（`MUSE_STREAM` 非 `1`）：**整段缓冲**，读完整段后自设 `Content-Length` 回传，
  与旧实现可观测行为一致。
- `MUSE_STREAM=1`：**真流式透传**——不设 `Content-Length`，改用 `chunked`，边收边发，
  SSE 首字节延迟显著下降。
- 该开关是需求书 §3 允许的唯一增强，默认关闭；两档都有单测与端到端验收。
- 流式模式下若上游在**响应头之后**中途出错，已提交的状态码无法更改，连接会提前结束；
  这是流式的固有取舍（缓冲模式则会把中途读失败归为 `502`）。

## 4. 安全边界

- key 只从 `MUSE_UPSTREAM_KEY` 或两个本地配置文件读取；不打印、不进错误信息、不进响应。
- 仓库内**没有任何密钥文件**；`.env.example` 与 `deploy/` 样例只含占位符。
- 强制经出口代理，**不直连上游**；出口代理不可达时返回 `502` 而非直连。
