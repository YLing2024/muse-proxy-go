# muse-proxy-go 验收记录（需求书 §6 十条）

所有命令在 `/root/proj/muse-proxy-go` 下执行，涉及的真实上游冒烟只跑一次最小请求、响应不落盘、不打印 key。
验收用临时脚本与日志放在 `.acceptance/`（已 gitignore）。

## 准备：构建与启停

```sh
make build && make tools
# 假上游 19391 / 假出口代理 19392
tools/fakeupstream/fakeupstream -addr 127.0.0.1:19391 &
tools/fakeproxy/fakeproxy      -addr 127.0.0.1:19392 &
# 新版（缓冲档）
MUSE_STREAM=0 MUSE_UPSTREAM=http://127.0.0.1:19391/v1 MIXED_PROXY=http://127.0.0.1:19392 \
  MUSE_UPSTREAM_KEY=parity-key MUSE_TIMEOUT=2s ./muse-proxy-go &
# 旧实现的「同源副本」：只把 UPSTREAM/MIXED_PROXY/PORT/AUTH_HEADER 四个常量换成测试值后运行，
# 脚本本身不改、逻辑不动（原始 /usr/local/bin/muse-proxy.py 未触碰）。
cp /usr/local/bin/muse-proxy.py .acceptance/muse-proxy-parity.py
sed -i -e 's#^UPSTREAM = .*#UPSTREAM = "http://127.0.0.1:19391/v1"#' \
       -e 's#^MIXED_PROXY = .*#MIXED_PROXY = "http://127.0.0.1:19392"#' \
       -e 's/^PORT = 8787/PORT = 19393/' \
       -e 's#^AUTH_HEADER = .*#AUTH_HEADER = "Bearer parity-key"#' .acceptance/muse-proxy-parity.py
python3 .acceptance/muse-proxy-parity.py &
```

启动日志：

```
muse-spark proxy listening on 127.0.0.1:19287 -> http://127.0.0.1:19391/v1 via http://127.0.0.1:19392 (buffered)
muse-spark proxy listening on 127.0.0.1:19393 -> http://127.0.0.1:19391/v1 via http://127.0.0.1:19392   （旧副本）
```

## 1. 路径拼接

```sh
curl -sS -X POST http://127.0.0.1:19287/v1/chat/completions -H 'Content-Type: application/json' -d '{"model":"muse-spark-1.3-contributor"}'
curl -sS -X POST http://127.0.0.1:19287/chat/completions      -H 'Content-Type: application/json' -d '{"model":"muse-spark-1.3-contributor"}'
```

假上游记录（两次都落在 `/v1/...`，未重复）：

```
fake-upstream POST /v1/chat/completions auth=Bearer parity-key
fake-upstream POST /v1/chat/completions auth=Bearer parity-key
```

## 2. 白名单三分支

```sh
curl -sS -w '\nstatus=%{http_code}\n' -X POST http://127.0.0.1:19287/v1/chat/completions -H 'Content-Type: application/json' -d '{"model":"muse-spark-1.2-contributor"}'
curl -sS -w '\nstatus=%{http_code}\n' -X POST http://127.0.0.1:19287/v1/chat/completions -H 'Content-Type: application/json' -d '{"model":"gpt-4"}'
curl -sS -w '\nstatus=%{http_code}\n' -X POST http://127.0.0.1:19287/v1/chat/completions -H 'Content-Type: application/json' -d 'not json'
curl -sS -o /dev/null -w 'status=%{http_code}\n' http://127.0.0.1:19287/v1/models
```

输出：

```
status=200
{"type": "error", "error": {"message": "model gpt-4 not allowed through proxy"}}
status=403
{"type": "error", "error": {"message": "bad request: Expecting value: line 1 column 1 (char 0)"}}
status=400
status=200
fake-upstream GET /v1/models auth=Bearer parity-key
```

## 3. 凭证替换 + 头过滤

```sh
curl -sS -X POST http://127.0.0.1:19287/v1/chat/completions -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer CLIENT-SECRET-MUST-NOT-LEAK' -H 'Connection: close' -H 'X-Custom: kept' \
  -d '{"model":"muse-spark-1.3-contributor"}'
grep -c 'parity-key' new-stream0.log     # 关键：日志里不得出现 key
grep -c 'CLIENT-SECRET' new-stream0.log
```

假上游回显（`authorization` 已换成真实 key；`Connection` 未透传；`X-Custom` 透传）：

```
{"authorization":"Bearer parity-key","body":"{\"model\":\"muse-spark-1.3-contributor\"}",...,"headers":{"Accept":"*/*","Accept-Encoding":"gzip","Authorization":"Bearer parity-key","Content-Type":"application/json","User-Agent":"curl/8.14.1","X-Custom":"kept"},"method":"POST","uri":"/v1/chat/completions"}
0
0
```

## 4. 响应回传（状态码 / 响应头 / Content-Length）

```sh
# 2xx 透传（假上游默认 200）
curl -sS -D - -o /tmp/body4 -X POST http://127.0.0.1:19287/v1/chat/completions \
  -H 'Content-Type: application/json' -H 'X-Fake-Status: 200' -d '{"model":"muse-spark-1.3-contributor"}'
# 上游 4xx/5xx → 502（复刻旧实现，见 §12）
curl -sS -D - -o /tmp/body4 -X POST http://127.0.0.1:19287/v1/chat/completions \
  -H 'Content-Type: application/json' -H 'X-Fake-Status: 503' -d '{"model":"muse-spark-1.3-contributor"}'
```

```
HTTP/1.1 200 OK
Content-Length: 297
Content-Type: application/json
X-Fake-Upstream: yes
body bytes: 297

HTTP/1.1 502 Bad Gateway
Content-Length: 95
Content-Type: application/json
body: {"type": "error", "error": {"message": "upstream failed: HTTP Error 503: Service Unavailable"}}
```

`2xx` 状态码透传、上游自定义头 `X-Fake-Upstream` 透传、自设 `Content-Length` 与 body 字节数一致；
上游 `4xx/5xx` 一律回 `502` + `upstream failed: HTTP Error <code>: <reason>`（需求书 §6.4）。

## 5. 错误分支 502（不可达 / 超时）

```sh
# 超时：假上游 sleep 5s，MUSE_TIMEOUT=2s
time curl -sS -w '\nstatus=%{http_code}\n' -X POST http://127.0.0.1:19287/v1/chat/completions \
  -H 'Content-Type: application/json' -H 'X-Fake-Sleep: 5' -d '{"model":"muse-spark-1.3-contributor"}'
```

```
{"type": "error", "error": {"message": "upstream failed: Post \".../v1/chat/completions\": net/http: timeout awaiting response headers"}}
status=502

real    0m2.029s
```

不可达分支见第 6 条（停掉出口代理后 `proxyconnect ... connection refused` → 502）。

## 6. 出口代理强制（证明经代理出去、且不直连）

```sh
sort -u .acceptance/fake-proxy.log          # 假代理确实收到流量
kill <fakeproxy_pid>                         # 停掉假代理
curl -sS -m 5 -w '\nstatus=%{http_code}\n' -X POST http://127.0.0.1:19287/v1/chat/completions \
  -H 'Content-Type: application/json' -d '{"model":"muse-spark-1.3-contributor"}'
```

```
fake-proxy GET  http://127.0.0.1:19391/v1/models
fake-proxy POST http://127.0.0.1:19391/v1/chat/completions

{"type": "error", "error": {"message": "upstream failed: Post \"...\": proxyconnect tcp: dial tcp 127.0.0.1:19392: connect: connection refused"}}
status=502
```

假代理停掉后请求 502（上游其实健在），说明请求只能经代理出去、不直连。

## 7. 流式两档

```sh
# MUSE_STREAM=0（缓冲）
curl -sS -D h0 -o b0 -w 'time_starttransfer=%{time_starttransfer}s time_total=%{time_total}s\n' \
  -X POST http://127.0.0.1:19287/v1/chat/completions -H 'Content-Type: application/json' -H 'X-Fake-Stream: 1' \
  -d '{"model":"muse-spark-1.3-contributor"}'
# MUSE_STREAM=1（真流式，重启实例后）
curl -sS -D h1 -o b1 -w 'time_starttransfer=%{time_starttransfer}s time_total=%{time_total}s\n' \
  -X POST http://127.0.0.1:19287/v1/chat/completions -H 'Content-Type: application/json' -H 'X-Fake-Stream: 1' \
  -d '{"model":"muse-spark-1.3-contributor"}'
```

| 档位 | 关键响应头 | time_starttransfer | time_total |
|---|---|---|---|
| `MUSE_STREAM=0` | `Content-Length: 59` | `0.912s` | `0.913s` |
| `MUSE_STREAM=1` | `Transfer-Encoding: chunked`（无 Content-Length） | `0.0037s` | `0.906s` |

`MUSE_STREAM=1` 逐块到达（每块一行加时间戳）：

```
00:44:10.410 data: chunk-0
00:44:10.709 data: chunk-1
00:44:11.017 data: chunk-2
00:44:11.311 data: [DONE]
```

## 8. 与旧版并排对照

对照矩阵见 `.acceptance/matrix.sh`（`bash .acceptance/matrix.sh http://127.0.0.1:19393 http://127.0.0.1:19287`）。
旧版另用现网 `8787` 复核「不触上游」的分支（`403` / `400` 完全相同的行为）。

| 用例 | 旧（19393 副本 / 8787 现网） | 新（19287） | 结论 |
|---|---|---|---|
| c1 白名单模型 | 200 | 200 | 一致（响应体差异仅在假上游回显的请求头：旧多 `Accept-Encoding: identity`/`Connection: close`） |
| c2 非白名单 `gpt-4` | 403 + 逐字节相同 | 403 + 逐字节相同 | 一致 |
| c3 非 JSON body | 400 `bad request: Expecting value: line 1 column 1 (char 0)` | 400 同上（逐字节相同） | 一致（内层文案复刻 Python `json` 模块，见 §11.2） |
| c4 缺 model | 403 `model None not allowed` | 403 `model None not allowed` | 一致 |
| c5 `GET /v1/models` | 200 | 200 | 一致（同上请求头回显差异：旧还多 `Content-Type: application/x-www-form-urlencoded`） |
| c6 `PATCH` | 501（HTML 错误页） | 501（纯文本） | 状态码一致；体不同 |
| c7 上游 503 | **502** `upstream failed: HTTP Error 503: Service Unavailable` | **502** 同文案 | 一致（复刻旧实现把 4xx/5xx 变 502，见 §12） |

现网 8787 复核（不触上游的分支）：

```
OLD8787 {"type": "error", "error": {"message": "model gpt-4 not allowed through proxy"}} <<status=403>>
NEW19287 {"type": "error", "error": {"message": "model gpt-4 not allowed through proxy"}} <<status=403>>
OLD8787 {"type": "error", "error": {"message": "bad request: Expecting value: line 1 column 1 (char 0)"}} <<status=400>>
NEW19287 {"type": "error", "error": {"message": "bad request: Expecting value: line 1 column 1 (char 0)"}} <<status=400>>
OLD8787 {"type": "error", "error": {"message": "model None not allowed through proxy"}} <<status=403>>
NEW19287 {"type": "error", "error": {"message": "model None not allowed through proxy"}} <<status=403>>
```

## 9. 真实上游冒烟（真 key + 真 mihomo，一次最小请求）

新版以默认配置启动（上游默认值、出口 `127.0.0.1:7890`、key 来自 `/root/.pi/agent/models.json`），
向 `/v1/responses` 发一次最小请求；响应只解析元数据、不落盘、不打印 key。

```sh
curl -sS -X POST http://127.0.0.1:19287/v1/responses -H 'Content-Type: application/json' \
  -H 'x-opencode-session: muse-proxy-smoke-test' \
  -d '{"model":"muse-spark-1.3-contributor","input":"ping","max_output_tokens":16}'
```

```
真实上游冒烟 HTTP 200
object = response | model = muse-spark-1.3-contributor | status = incomplete
id = resp_6ad10337b3dc0c8c71884136
usage = {'input_tokens': 8, 'output_tokens': 16, 'total_tokens': 24, ...}
--- 新代理日志（证明无 key） ---
0
[2026-10-16T00:45:42] POST /responses model=muse-spark-1.3-contributor
```

> 说明：该上游的 `muse-spark` 模型走 OpenAI **Responses** 协议（`/v1/responses`）；
> `POST /v1/chat/completions` 会被上游回 `400 ModelProtocolUnsupported`（与代理无关）。

## 10. 资源占用

```sh
ls -l muse-proxy-go
ps -o pid,rss,comm -p <new_pid>
```

```
二进制：6,406,328 bytes ≈ 6.11 MiB（-s -w -trimpath）
空载 RSS：≈ 7.4–13.5 MiB（旧 Python 实现现网进程 15.5 MiB，仅作对照）
冷启动：12 ms（启动到端口可连接）
```

## 11. 兼容性修复记录（2026-10-16）

并排对照暴露两处旧实现可观测行为未对齐：空 body 请求未向上游补发 `Content-Length: 0`、
`400` 内层文案用的是 `encoding/json` 的 Go 文案而非 Python `json` 模块文案。两处均已修复并各配单测。

下方为同机、同参数、真实运行取证的「旧实现 / 修复前 / 修复后」三者对照：旧实现为
`/usr/local/bin/muse-proxy.py` 的同源副本（仅换 `UPSTREAM` / `MIXED_PROXY` / `PORT` / `AUTH_HEADER` 四个常量，
逻辑未动），修复前取修复前提交构建的二进制，修复后取当前工作树构建的二进制；三者的出口代理串到同一
抓包记录器，由其打印转发到上游的原始请求行与 `Content-Length`。

### 11.1 空 body 请求补发 `Content-Length: 0`

```
RECV GET    http://oldrun.example/v1/models       HTTP/1.1 content-length=0          ← 旧实现
RECV GET    http://beforerun.example/v1/models    HTTP/1.1 content-length=<MISSING>  ← 修复前
RECV GET    http://afterrun.example/v1/models     HTTP/1.1 content-length=0          ← 修复后
RECV DELETE http://oldrun.example/v1/files/abc    HTTP/1.1 content-length=0
RECV DELETE http://beforerun.example/v1/files/abc HTTP/1.1 content-length=<MISSING>
RECV DELETE http://afterrun.example/v1/files/abc  HTTP/1.1 content-length=0
RECV HEAD   http://oldrun.example/v1/models       HTTP/1.1 content-length=0
RECV HEAD   http://beforerun.example/v1/models    HTTP/1.1 content-length=<MISSING>
RECV HEAD   http://afterrun.example/v1/models     HTTP/1.1 content-length=0
```

修复后 GET / HEAD / DELETE 与旧实现一致，均为 `content-length=0`；带 body 的 POST 仍发真实长度
（`content-length=52`），语义与旧实现 `data=b""` 对齐。单测
`internal/proxy.TestBodylessRequestsSendContentLengthZero`。

### 11.2 `400` 内层文案复刻 Python `json` 模块

同一串非法 / 非对象 body（`POST /v1/chat/completions`，`Content-Type: application/json`）的状态码与响应体：

| body | 旧实现 | 修复前 | 修复后 |
|---|---|---|---|
| `{not json` | `bad request: Expecting property name enclosed in double quotes: line 1 column 2 (char 1)` | `bad request: invalid character 'n' looking for beginning of object key string` | 同旧实现 |
| `{"a"` | `bad request: Expecting ':' delimiter: line 1 column 5 (char 4)` | `bad request: unexpected EOF` | 同旧实现 |
| `{"a":1} extra` | `bad request: Extra data: line 1 column 9 (char 8)` | `bad request: trailing data after JSON value` | 同旧实现 |
| `not json` | `bad request: Expecting value: line 1 column 1 (char 0)` | `bad request: invalid character 'o' in literal null (expecting 'u')` | 同旧实现 |
| `[1,2]` | `bad request: 'list' object has no attribute 'get'` | `bad request: payload is not a JSON object` | 同旧实现 |
| `"str"` | `bad request: 'str' object has no attribute 'get'` | `bad request: payload is not a JSON object` | 同旧实现 |

三实例在 `{"model":"gpt-4"}`（403）与 `{}`（403）上逐字节相同，未受本次改动影响。

修复方式：`internal/httpx` 移植 CPython 3.13 `Modules/_json.c` 的扫描器，在 rune（code point）
序列上计位，使其行列与 `char` 偏移同 Python 一致（含多字节字符：`{"é":}` → `char 5`、非字节 `6`）。
解析成功但值不是对象时复刻 `AttributeError`。122 条真实向量与 Python `json.loads` 逐字节比对
（`cmp expected.tsv got.tsv`）完全一致。未映射形态仅剩带 UTF-16 / UTF-32 BOM 的 body，
已在 `docs/COMPAT.md` §2.2 说明。单测 `internal/proxy.TestBadJSONMessagesMatchPython`、
`internal/httpx.TestPyDecodeErrorsMatchPython`。

## 12. 状态码口径对齐：上游 4xx/5xx → 502（2026-10-16）

需求书 §2.6 / §6.4 按旧实现实测改定为「上游 4xx/5xx 一律回 502」。同机、同假上游
（`tools/fakeupstream`）、同假出口代理（`tools/fakeproxy`）跑同一请求矩阵：

- 旧实现 = `/usr/local/bin/muse-proxy.py` 同源副本（`PORT=19393`，仅换 4 个常量）；
- 新缓冲 = `MUSE_STREAM=0`（`19287`）；新流式 = `MUSE_STREAM=1`（`19288`）。

复现命令：`bash .acceptance/parity.sh`（原始输出见 `.acceptance/parity.out`）。

完整并排矩阵（含不一致项判定）见 `docs/COMPAT.md` §2.1，此处只留原始输出节选。

原始输出（节选）：

```
--- 上游 X-Fake-Status: 400 ---
old   502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 400: Bad Request"}}
new0  502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 400: Bad Request"}}
new1  502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 400: Bad Request"}}
--- 上游 X-Fake-Status: 404 ---
old   502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 404: Not Found"}}
new0  502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 404: Not Found"}}
new1  502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 404: Not Found"}}
--- 上游 X-Fake-Status: 429 ---
old   502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 429: Too Many Requests"}}
new0  502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 429: Too Many Requests"}}
new1  502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 429: Too Many Requests"}}
--- 上游 X-Fake-Status: 503 ---
old   502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 503: Service Unavailable"}}
new0  502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 503: Service Unavailable"}}
new1  502 | {"type": "error", "error": {"message": "upstream failed: HTTP Error 503: Service Unavailable"}}
```

非 HTTP 异常（不可达 / 超时）仍回 502，前缀 `upstream failed: `，文案为 Go 措辞（唯一无法逐字节对齐的一类）：

```
{"type": "error", "error": {"message": "upstream failed: Post \"...\": net/http: timeout awaiting response headers"}}   status=502
{"type": "error", "error": {"message": "upstream failed: Post \"...\": proxyconnect tcp: dial tcp 127.0.0.1:1: connect: connection refused"}}   status=502
```

新版日志中未出现测试 key（`grep -c parity-key .acceptance/new-*.log` → `0`）。

## 端到端测试

```sh
make check   # 格式 + 路由↔文档机检 + go vet + go test
make race    # 并发转发 -race
```

```
源码 API 路由：5 条 / 文档端点：5 条 / 检查通过：源码路由与 docs/API.md 一致
ok  muse-proxy-go/internal/config / httpx / key / logx / proxy
```
