# 验证指南

## 单元测试与静态检查

使用 `go.mod` 指定的工具链和 `.github/tool-versions.env` 中的开发工具版本，执行 `go test ./...`、`go vet ./...` 和 `golangci-lint run`。

Linux 环境安装 C 编译器后，执行 `go test -race -coverprofile=coverage.out ./...` 检查并发行为。

测试通过本地 HTTP 服务覆盖在线、离线、业务错误、鉴权错误、网络错误、超时、无效 JSON、字段缺失和字段类型错误。状态切换测试检查在线样本的出现与消失、最近成功时间的更新与保留，以及每次抓取的上游请求次数。

## Prometheus 集成验证

按[贡献指南](../CONTRIBUTING.md#独立进程验收)构建 exporter、准备 Prometheus 和 promtool，运行独立进程验收。

验收会启动独立 exporter、模拟 OneBot 服务和 Prometheus，检查查询中的在线、离线、未知、恢复及 exporter 退出后的抓取状态。promtool 用于检查实际指标输出和示例告警规则。

执行 `promtool test rules examples/alerts.test.yml` 验证离线、API 失败和 exporter 无法抓取之间的告警计时连续性及恢复后的解除。

## deb 与 systemd 验证

按[贡献指南](../CONTRIBUTING.md#打包验收)生成 deb，并在隔离的 Linux/systemd 环境执行包生命周期测试。测试需要 root 安装软件包，exporter 服务以专用非 root 用户运行。

验收覆盖首次安装、显式启用、带 Token 采集、升级保留配置、停用后重新安装、remove 和 purge。升级生命周期测试通过调整测试包版本号构造升级路径；历史版本兼容测试应使用对应的已发布包。

## NapCat 部署验证

部署时核对 OneBot HTTP API 地址和鉴权设置，将 exporter 加入 Prometheus 抓取配置。检查在线状态、API 采集结果和 exporter 抓取状态，并在测试账号与隔离环境中验证离线、API 停止和恢复。

提交验证结果时记录相关版本、执行命令和实际结果。与改动相关但未执行的检查必须说明原因或执行限制；报告只包含与本次改动有关的检查。
