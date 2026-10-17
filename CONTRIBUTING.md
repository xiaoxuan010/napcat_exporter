# 贡献指南

使用 `go.mod` 指定的 Go 工具链。开发工具版本集中维护在 `.github/tool-versions.env`，格式检查规则位于 `.golangci.yml`。

提交 PR 前执行 `gofmt`、`go mod tidy`、`go test ./...`、`go vet ./...`；Linux 环境还需执行 `go test -race ./...`。使用本地 HTTP 服务模拟 OneBot，不使用真实 QQ 账号或 Token。GoReleaser 在打包时从构建依赖生成 `THIRD_PARTY_NOTICES`，发布物会携带这些声明。

测试必须验证行为。名称、默认值及协议常量引用已有定义，不在测试里硬编码第二份，也不使用同一常量赋值再断言相等的空验证。禁止复制生产分支或保存当前输出作为正确性基准。完整约束见 `docs/requirements.md` §8.1。

## 独立进程验收

在 Linux 中构建 exporter，下载校验过的 Prometheus 工具，然后运行：

```sh
mkdir -p .tmp
CGO_ENABLED=0 go build -trimpath -o "$PWD/.tmp/napcat_exporter" ./cmd/napcat_exporter
bash scripts/install-prometheus.sh "$PWD/.tmp/prometheus-tools"
"$PWD/.tmp/prometheus-tools/promtool" test rules examples/alerts.test.yml
NAPCAT_E2E_BINARY="$PWD/.tmp/napcat_exporter" \
PROMETHEUS_BINARY="$PWD/.tmp/prometheus-tools/prometheus" \
PROMTOOL_BINARY="$PWD/.tmp/prometheus-tools/promtool" \
go test -count=1 -v ./internal/acceptance -run TestPrometheus
```

测试使用临时上游、动态端口和临时 TSDB，验证真实 Prometheus 查询中的在线、离线、样本消失和恢复，不依赖外部 NapCat。必须使用 `-count=1`，避免外部二进制已变化而 Go 复用测试结果缓存。

## 打包验收

在有 Git 提交的工作树执行 `goreleaser check`、`goreleaser release --snapshot --clean`。只在可丢弃的 Linux/systemd 环境中以 root 执行包测试，测试会安装、升级、停止和卸载生成的包，发现已有同名包时拒绝运行。

```sh
go test -c -o "$PWD/.tmp/package-acceptance" ./internal/acceptance
package=$(find "$PWD/dist" -maxdepth 1 -name '*.deb' -print -quit)
sudo env NAPCAT_DEB="$package" "$PWD/.tmp/package-acceptance" -test.v -test.run '^TestPackageLifecycle$'
```

包生命周期测试通过调整测试包版本号构造升级路径。历史版本兼容测试应使用对应的已发布包。

## 发布

使用短分支和 PR，合并前通过 CI。`v*` tag 触发发布，同一 tag 的代码先运行检查，再生成和验收最终产物，生成来源证明后上传 GitHub Release。不要重写已发布 tag。

Actions 使用完整 commit SHA 并附版本注释，Dependabot 每周为 Actions 和 Go Modules 提交更新 PR。开发工具版本文件由维护者显式更新。正式版本构建来源可用 `gh attestation verify` 验证，SHA-256 只能校验文件完整性，不能单独证明发布者身份。

验证报告应记录相关版本、执行命令和实际结果。与改动相关但未执行的检查必须说明原因或执行限制；省略无关检查。
