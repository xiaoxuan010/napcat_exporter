# 技术决策

- 使用 Go 单 module，路径为 `github.com/xiaoxuan010/napcat_exporter`。
- 运行时主要依赖 `prometheus/client_golang`；测试使用 Prometheus 的指标解析与数据模型包。
- 配置入口为 flags 与环境变量，Token 直接传入。systemd 通过 `EnvironmentFile` 提供环境配置。
- Prometheus 抓取触发同步采集，请求绑定抓取上下文。每次抓取执行一次 `get_status`。
- 上游请求具有超时和响应大小限制，必要字段严格校验，允许额外字段。
- 每次抓取构建独立指标快照。最近成功时间在进程内并发安全地维护，重启后从首次成功采集重新记录。
- `napcat_exporter_build_info` 表示 exporter 自身的版本与源码 revision。
- Go 版本以 `go.mod` 为准；开发工具版本集中维护在 `.github/tool-versions.env`；Actions 固定 SHA，Dependabot 提交升级 PR。
- 提供 Linux amd64 tar.gz 与 deb。deb 首次安装后由用户配置并启用服务，升级保留配置与启用状态。
- 使用语义化版本。二进制名为 `napcat_exporter`；Debian 包、service 和服务用户采用 `napcat-exporter`。
- 默认监听地址及端口由配置定义维护，可通过 flags 或环境变量覆盖；配置接口变更需提供升级说明。
- 第三方许可证从构建所用模块和 Go 工具链自动生成，随发布物分发。
