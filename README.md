# napcat_exporter

独立、只读的 NapCat Prometheus exporter。每次 Prometheus 抓取时，通过 OneBot HTTP API 查询一次 `get_status`，将 `data.online` 转换为 QQ 在线状态指标。

社区维护者：xiaoxuan010。项目使用 MIT 许可证，与 NapCat 官方项目无隶属关系。

## 工作方式

```text
Prometheus → GET /metrics → exporter → POST /get_status → NapCat
```

一个 exporter 对应一个 NapCat 实例。exporter 没有后台轮询、自动重试、登录或重启 NapCat 的功能。未抓取时不调用上游。`/healthz` 不请求上游，NapCat 不可用期间仍可响应。

## 构建和运行

工具链版本以 `go.mod` 为准，支持 Go 的自动工具链下载。

```sh
go build -trimpath -o napcat_exporter ./cmd/napcat_exporter
export NAPCAT_API_URL='http://127.0.0.1:3000'
export NAPCAT_TOKEN='<ONEBOT_TOKEN>'
./napcat_exporter
```

上游地址是示例，必须按 NapCat OneBot HTTP 配置修改。未开启鉴权时省略 `NAPCAT_TOKEN`。Token 直接通过环境变量或 `--napcat.token` 传入，程序不读取 Token 文件。

```powershell
$env:NAPCAT_API_URL = 'http://127.0.0.1:3000'
$env:NAPCAT_TOKEN = '<ONEBOT_TOKEN>'
go run ./cmd/napcat_exporter
```

使用 `--help` 查看所有配置及默认值，使用 `--version` 查看 exporter 构建信息。帮助输出不会展示环境中的 Token。日志以 JSON 写入 stderr；帮助和版本写入 stdout。

配置优先级为显式 flag > 环境变量 > 内置默认值。API URL 必填，其他配置的定义以 `internal/config/config.go` 为准。支持路径前缀；URL 不允许包含账号密码、query 或 fragment。HTTPS 正常验证证书，HTTP 客户端直连上游，不读取代理环境变量、不跟随重定向。修改配置后重启生效。

默认 web listener 绑定回环地址，具体默认值见 `--help`。跨主机抓取需显式设置 `--web.listen-address`，由部署者通过防火墙或代理限制访问来源。

## Debian / Ubuntu 安装

发布工作流生成 Linux amd64 `.tar.gz`、`.deb` 和 `checksums.txt`。下载适用的发布物后校验 SHA-256，再安装：

```sh
sudo apt install ./napcat-exporter_<VERSION>_amd64.deb
sudoedit /etc/napcat-exporter/environment
sudo systemctl enable --now napcat-exporter
```

包安装专用非登录用户和 service。首次安装后，先配置 API URL 和 Token，再执行上面的启用命令。环境配置由 systemd `EnvironmentFile` 读取，按该格式填写变量和值。

升级保留本地配置，原本运行的服务按系统策略尝试重启，不重新启用已停用服务。`apt remove` 停止服务并保留配置，`apt purge` 删除包管理的配置。为避免 UID 被复用，卸载后保留专用系统用户。脚本兼容没有运行 systemd 的安装环境，但只有运行 systemd 的系统能管理该服务。

手动二进制安装可参考 `packaging/systemd/napcat-exporter.service`；需要自行创建对应用户、配置路径，并按实际二进制位置调整 unit。

## Prometheus 和告警

参考 `examples/prometheus.yml` 和 `examples/alerts.yml`。建议 `scrape_interval: 15s`、`scrape_timeout: 5s`，上游请求超时必须小于抓取超时。

| 指标 | 含义 |
|---|---|
| `napcat_api_up{action="get_status"}` | 本次 HTTP、OneBot 业务结果和 `online` 类型校验全部成功为 1，否则为 0 |
| `napcat_online` | 本次有效响应的 QQ 在线状态：在线为 1、离线为 0；失败时省略 |
| `napcat_last_successful_collection_timestamp_seconds` | 进程内最近一次成功采集时间；首次成功前省略，重启后清空 |
| `napcat_scrape_duration_seconds` | 本次请求和校验耗时，失败时也输出 |
| `napcat_exporter_build_info` | exporter 自身的版本和 revision；不查询 NapCat 版本 |

另外输出标准 Go 和 process 指标。标签不包含 Token、QQ 身份、消息正文或上游错误正文。

Prometheus 自带 `up` 表示 exporter 抓取是否成功，与 `napcat_api_up` 区分。NapCat API 不可用时 `/metrics` 仍返回 HTTP 200，因此可能同时出现 `up=1`、`napcat_api_up=0`。每次抓取的指标独立构建，不把历史在线值当作当前状态。

最多允许有限数量的并发 metrics 请求，超出时返回 HTTP 503；`/healthz` 独立响应。请求超时、网络、鉴权、HTTP、JSON、结构及业务错误在日志中使用不同类别。

示例告警以“持续无法确认 QQ 在线”为条件，覆盖明确离线、API 采集失败和 exporter 无法抓取。异常持续一分钟后触发，状态从离线转为未知时继续计时；重新采集到有效的在线响应后恢复。最近成功时间用于辅助排查，首次成功前没有该样本。

`get_status.online` 表达 NapCat 维护的核心在线值。OneBot HTTP 服务在登录后初始化，等待登录和 Worker 重启期间可能无法访问，exporter 此时报告采集失败。下线到重启之间的短暂离线窗口可能被采样跳过，因此告警采用“无法确认在线”的含义。QQ 的消息收发能力需要额外的端到端检查。

## 验证和支持范围

运行平台和接口要求见[兼容性](docs/compatibility.md)，测试和部署检查方法见[验证指南](docs/validation.md)。

```sh
go test ./...
go vet ./...
# Linux 且有 C 编译器时运行 race detector：
go test -race ./...
```

独立进程及系统包验收为显式启用的测试，执行方式和测试设计约束见[贡献指南](CONTRIBUTING.md)。
