# AGENTS.md — muse-proxy-go 维护约定

本仓库是 muse-spark 专属转发代理（muse-proxy）的 Go 重写。旧实现
`/usr/local/bin/muse-proxy.py` 是**功能权威**（只读，不得修改、不得重启其 systemd 单元）。
权威工程规范见 `/root/proj/ENGINEERING_BASELINE.md`；本文件只做本仓库补充，不降低其要求。

## 技术栈（固定）

- HTTP：仅标准库 `net/http`；不引 web 框架、路由库与第三方依赖（**零第三方依赖**）。
- 出口代理：`http.Transport.Proxy` 强制指定，**绝不直连上游**。
- 日志：标准库 `log/slog`，配 `internal/logx` 的自定义 Handler 复刻旧版访问日志行。
- 无数据库、无状态：双库约定不适用。

## 目录

- `cmd/muse-proxy-go/`：进程入口（装配 + 信号退出）。
- `internal/config/`：环境变量装载与「只监听回环」等启动校验。
- `internal/key/`：凭证按 `env → PI_MODELS_JSON → OPENCODE_AUTH_JSON` 顺序装载。
- `internal/proxy/`：转发核心（路径拼接、白名单、头过滤、缓冲/流式回传）+ `routes.go` 路由登记。
- `internal/httpx/`：与旧实现逐字节兼容的错误信封与 Python 式 JSON 转义。
- `internal/logx/`：旧格式访问日志的 `slog.Handler`。
- `tools/fakeupstream/`、`tools/fakeproxy/`：验收用假上游 / 假代理（不参与生产部署）。
- `deploy/`：systemd / nginx 样例（**只提交，不安装到 `/etc`**）。

## 硬约束（改动前必读）

1. **行为逐条对齐旧实现**（需求书 §2 共 12 条）：路径去 `/v1`、白名单三分支、请求/响应头过滤、
   `Authorization` 覆盖、响应头透传、`2xx` 状态码透传（`3xx` 带 `Location` 跟随、无 `Location` 回 `502`）、上游 `4xx/5xx` 与转发失败一律 `502`、五方法同处理器、`120s` 超时、
   key 读取顺序、强制出口代理、每请求一行日志。改了就要同步 `docs/COMPAT.md`。
2. **唯一允许的增强是 `MUSE_STREAM`**，且默认关闭（保持整段缓冲语义）。新增任何其它行为差异前先改需求书。
3. **key 永不外泄**：不进源码、日志、错误信息、响应、仓库文件；仓库内不得有任何密钥文件。
4. 改了路由必须同步 `docs/API.md`（`make check` 会机检）。

## 红线

- 源码 / 配置 / 文档 / 提交信息不得出现私有域名、公私网 IP、邮箱、密钥、真实口令；
  示例统一 `example.com` / `127.0.0.1`。（上游 base 的公共 API 域名是需求书指定的默认值，属例外。）
- 只监听 `127.0.0.1`；生产端口 `19287`；不占用 8787 / 7890 及其它现网端口。
- 不写调试残留、不留注释掉的死代码、新增 TODO/FIXME 必须为 0；导出符号必须有文档注释。
- 不安装 systemd 单元到 `/etc`、不动 nginx、不重启旧服务与 mihomo。

## 命令

- `make build` / `make test` / `make race` / `make vet` / `make check`（格式 + 路由文档机检 + vet + 单测）。

## 提交

小步提交、中文信息、正文不带署名与 TASK 号；**禁止** `git push`、`checkout -b`、`rebase`、
`reset --hard`、`merge`；不要 `git add` 构建产物（二进制、`tools/*/fake*`、`.acceptance/`）。
