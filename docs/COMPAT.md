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
| 5 | 响应头过滤 + 自设 Content-Length | 丢弃 `transfer-encoding` / `connection` / `content-length`，缓冲模式自设 `Content-Length`；**2xx 透传；3xx 带 `Location` 由客户端跟随、无 `Location` 的 3xx 回 502；4xx/5xx 一律回 502**（复刻旧实现，见 §2.1）。 |
| 6 | 上游失败/超时 → 502 | 转发链路失败（不可达、超时、TLS 失败等非 HTTP 异常）回 `502`，message 前缀 `upstream failed: `；上游返回的 4xx/5xx、以及未被跟随的 3xx（无 `Location`）同样回 `502`（见 §2.1）。 |
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
| `502` 内层文案（非 HTTP 异常：不可达/超时/TLS 失败） | `upstream failed: <urlopen error [Errno 111] ...>` | `upstream failed: <Go net/http 错误>` | 前缀与状态码一致，错误文本不同（唯一无法逐字节对齐的一类）。 |
| `501` 响应体 | `send_error` 生成的 HTML 错误页 | 纯文本 `unsupported method` | 状态码一致，体不同。 |
| 请求头大小写 | `urllib` 会把头名 `capitalize`（如 `Content-type`） | `net/http` 规范化为 `Content-Type` | HTTP 头名大小写不敏感，语义一致。 |
| 同名请求头 | 收进 dict，只保留最后一个值 | 同样只保留最后一个值 | 语义一致（已显式对齐）。 |
| 响应 `Date` / `Server` 头 | 旧实现会同时发自身的与上游的两份 `Date`/`Server` | 只保留上游的 `Date`，不发自身 `Server` | 非关键头，客户端忽略。 |
| 自动 gzip | 不透明解压 | `DisableCompression=true`，同样不透明解压、不擅自加 `Accept-Encoding` | 行为一致（已显式对齐）。 |
| 响应头重复值 | `email.message` 可能合并重复头 | 逐个 `Add` 保留 | 语义一致。 |
| 上游请求头附加项 | `urllib` 额外加 `Accept-Encoding: identity`、`Connection: close`，无 body 时还会加 `Content-Type: application/x-www-form-urlencoded` | 纯透传（只转发客户端实际带的头），空 body 时补 `Content-Length: 0` | 需求书 §2.4 要求「其余原样透传」，故不复制 urllib 的运行时默认头；上游可见的额外头不同。 |
| 重定向 | `urllib.HTTPRedirectHandler` 对 GET/HEAD/POST 跟随 301/302/303/307/308，其余方法（PUT/DELETE）或 POST+307/308 会抛 `HTTPError` → 502 | `net/http` 对 301/302/303/307/308 一律跟随（301/302/303 改 GET；307/308 保留方法与 body） | 带 `Location` 的常见形态（GET/POST + 302 等）两者都跟随、结果一致；仅「PUT/DELETE + 任意 3xx」与「POST + 307/308」两者处理不同（旧版 502、新版跟随），属已知边界。 |

## 2.1 状态码口径：复刻旧实现的 502（2026-10-16 定案）

上一轮曾按当时的需求书实现「4xx/5xx 透传」，并据此列出旧实现与之矛盾。**需求书 §2.6 / §6.4
已按旧实现实测改定**：以旧实现可观测行为为准，上游 4xx/5xx **一律回 502**；**未被跟随的 3xx
（不带 `Location`）同样回 502**（本轮补齐）。本版现已对齐。

- 旧实现用 `urllib.request.OpenerDirector.open()`，其 `HTTPErrorProcessor` 对**所有非 2xx**
  状态抛 `HTTPError`（带 `Location` 的 3xx 先由 `HTTPRedirectHandler` 跟随、不落到这里），
  被 `except` 吞掉后回 **502** `upstream failed: HTTP Error <code>: <reason>`。
- 本版在响应头落盘前判定：**非 2xx（`resp.StatusCode >= 300`）→ 502**。能到达本层的 3xx
  一定是 Go 客户端**没有跟随**的（无 `Location`，或 300/304 等不跟随的码）：旧实现
  `urllib` 对这些 3xx 同样抛 `HTTPError` → 502；带 `Location` 的 3xx 由客户端跟随、取最终响应。
  **只有 2xx 照旧透传**。体为
  `{"type": "error", "error": {"message": "upstream failed: HTTP Error <code>: <reason>"}}`，
  `Content-Type: application/json`、`Content-Length` 自算。
  缓冲档与 `MUSE_STREAM=1` 流式档走同一判定，语义一致（见 §3）。

现网 8787（旧实现）实测座实：

```
POST /v1/chat/completions（上游回 400 Bad Request） → 旧实现回 502
  {"type": "error", "error": {"message": "upstream failed: HTTP Error 400: Bad Request"}}
GET  /v1/does-not-exist（上游回 404 Not Found）      → 旧实现回 502
  {"type": "error", "error": {"message": "upstream failed: HTTP Error 404: Not Found"}}
```

并排对照（同一假上游 `tools/fakeupstream`、同一假出口代理 `tools/fakeproxy`、同一请求矩阵；
旧 = `/usr/local/bin/muse-proxy.py` 同源副本 `19393`，新缓冲 `19287`，新流式 `19288`；
复现命令 `bash .acceptance/parity.sh`，原始输出 `.acceptance/parity.out`）：

| 上游 `X-Fake-Status` | 旧（19393） | 新缓冲（19287） | 新流式（19288） | 结论 |
|---|---|---|---|---|
| 200 / 201 | 200 / 201 | 200 / 201 | 200 / 201 | 一致（2xx 不受影响） |
| 400 | 502 `HTTP Error 400: Bad Request` | 同左 | 同左 | 逐字节一致 |
| 401 | 502 `HTTP Error 401: Unauthorized` | 同左 | 同左 | 逐字节一致 |
| 403 | 502 `HTTP Error 403: Forbidden` | 同左 | 同左 | 逐字节一致 |
| 404 | 502 `HTTP Error 404: Not Found` | 同左 | 同左 | 逐字节一致 |
| 405 | 502 `HTTP Error 405: Method Not Allowed` | 同左 | 同左 | 逐字节一致 |
| 409 | 502 `HTTP Error 409: Conflict` | 同左 | 同左 | 逐字节一致 |
| 422 | 502 `HTTP Error 422: Unprocessable Entity` | 同左 | 同左 | 逐字节一致 |
| 429 | 502 `HTTP Error 429: Too Many Requests` | 同左 | 同左 | 逐字节一致 |
| 500 | 502 `HTTP Error 500: Internal Server Error` | 同左 | 同左 | 逐字节一致 |
| 502 | 502 `HTTP Error 502: Bad Gateway` | 同左 | 同左 | 逐字节一致 |
| 503 | 502 `HTTP Error 503: Service Unavailable` | 同左 | 同左 | 逐字节一致 |
| 504 | 502 `HTTP Error 504: Gateway Timeout` | 同左 | 同左 | 逐字节一致 |
| 413 | 502 `... 413: Request Entity Too Large` | 同左 | 同左 | 逐字节一致 |
| 414 | 502 `... 414: Request URI Too Long` | 同左 | 同左 | 逐字节一致 |
| 416 | 502 `... 416: Requested Range Not Satisfiable` | 同左 | 同左 | 逐字节一致 |
| 418 | 502 `... 418: I'm a teapot` | 同左 | 同左 | 逐字节一致 |
| 599（未收录） | 502 `... 599: status code 599` | 同左 | 同左 | 逐字节一致 |
| 302（无 `Location`） | 502 `... 302: Found` | 同左 | 同左 | 逐字节一致 |
| 302（带 `Location`） | 跟随重定向后取最终状态码 | 同左 | 同左 | 逐字节一致 |

常见码 `400/401/403/404/405/409/422/429/500/502/503/504` 与上述非标准码均逐字节一致
（假上游是 Go，其状态行短语正是旧实现读到的同一批）；**唯一仍无法逐字节对齐的一类**是
非 HTTP 异常（不可达 / 超时）的文案，回 502、前缀 `upstream failed: `，文案为 Go 措辞（§2 表首行）。

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

## 2.3 502 文案里的 `<reason>` 来源：已对齐（2026-10-16 拍板）

旧实现的 502 文案 `upstream failed: HTTP Error <code>: <reason>` 里，`<reason>` 取自
**上游响应状态行本身**（`http.client` 对状态行做 `split(None, 2)` 后 `reason.strip()`），
不是任何标准表。本版现已按同一口径取值，逻辑在 `internal/httpx.StatusLineReason`：

- **优先**用上游状态行给的原样短语：上游写 `HTTP/1.1 413 Nope` → 文案即
  `upstream failed: HTTP Error 413: Nope`；上游写 `HTTP/1.1 599 Weird` → `... 599: Weird`。
  非标准码（413/414/416/418/599…）因此也与旧实现逐字节一致，不必再纠结 RFC 改名。
- **仅当**上游没给短语时，才回落到标准表 `internal/httpx.ReasonPhrase`。触发条件是
  HTTP/2 无 reason，或状态行在码后即结束 / 码后只有空白；表以本机 Python 3.13.5 的
  `http.client.responses` 逐项生成，未收录的码回 `Unknown`。**标准表只在这时兜底**，
  故表里保留经典还是新版短语都不影响一致性（413/422 仍沿用需求书 §2.6 列举值）。

并排对照（假上游 = Go `net/http`，其状态行短语即 Go `http.StatusText`，旧实现读到的正是同一批）
现在全部逐字节一致：

| 码 | 旧实现（取上游状态行） | 本版（同取上游状态行） | 结论 |
|---|---|---|---|
| `413` | 502 `... 413: Request Entity Too Large` | 同左 | 逐字节一致 |
| `414` | 502 `... 414: Request URI Too Long` | 同左 | 逐字节一致 |
| `416` | 502 `... 416: Requested Range Not Satisfiable` | 同左 | 逐字节一致 |
| `418` | 502 `... 418: I'm a teapot` | 同左 | 逐字节一致 |
| `599`（未收录） | 502 `... 599: status code 599` | 同左 | 逐字节一致 |

单测 `internal/httpx.TestStatusLineReasonPrefersUpstream` 覆盖「非标准短语原样取用」
「码后只有空白回落标准表」「状态行缺失回落标准表」；`internal/proxy` 的
`TestUpstreamReasonPhraseTakenVerbatim` 用裸 TCP 上游注入自定义状态行做端到端验证。

至此，**上游按 HTTP 应答时的 502 文案已全部对齐**。唯一仍无法逐字节对齐的一类只剩
**非 HTTP 异常**的文案（不可达 / 超时 / TLS 失败等：Go 运行时措辞 vs Python `URLError`），
见 §2 表首行；它由两种运行时的错误类型决定，非本代理可复刻。

3xx 无 `Location` 的 502 也已复刻（见 §2.1），单测 `internal/proxy.TestRedirectWithoutLocationBecomes502`
与 `TestRedirectWithLocationIsFollowed` 分别覆盖「无 Location → 502」与「带 Location → 跟随」。

## 3. 唯一有意差异：MUSE_STREAM

- 默认（`MUSE_STREAM` 非 `1`）：**整段缓冲**，读完整段后自设 `Content-Length` 回传，
  与旧实现可观测行为一致。
- `MUSE_STREAM=1`：**真流式透传**——不设 `Content-Length`，改用 `chunked`，边收边发，
  SSE 首字节延迟显著下降。
- 该开关是需求书 §3 允许的唯一增强，默认关闭；两档都有单测与端到端验收。
- **状态码语义与档位无关**：`MUSE_STREAM=1` 也在响应头落盘前判定上游非 2xx（含未被跟随的 3xx、
  4xx/5xx）→ `502` 错误体（此时上游尚未提交 2xx）；能进入流式回传即表示上游已是 2xx。
- 流式模式下若上游在**响应头之后**中途出错，已提交的状态码无法更改，连接会提前结束；
  这是流式的固有取舍（缓冲模式则会把中途读失败归为 `502`）。

## 4. 安全边界

- key 只从 `MUSE_UPSTREAM_KEY` 或两个本地配置文件读取；不打印、不进错误信息、不进响应。
- 仓库内**没有任何密钥文件**；`.env.example` 与 `deploy/` 样例只含占位符。
- 强制经出口代理，**不直连上游**；出口代理不可达时返回 `502` 而非直连。
