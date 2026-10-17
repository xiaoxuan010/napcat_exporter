# NapCat Exporter 需求文档

- 项目名：`napcat_exporter`
- 许可证：MIT
- 运行方式：独立进程
- v1 状态来源：OneBot HTTP API `get_status` 的 `data.online`
- 目标读者：使用 NapCat、Prometheus 的部署者及项目维护者

## 1. 背景与目标

Prometheus 通过 exporter 将上游系统的状态转换成 `/metrics` 指标，再按配置主动抓取。本项目使用 NapCat OneBot HTTP API 的 `get_status` 查询在线状态。[NapCat API 兼容情况](https://napneko.github.io/develop/) [Prometheus exporters](https://prometheus.io/docs/instrumenting/exporters/)

本项目为 NapCat 提供一个独立、只读、低资源消耗的 Prometheus exporter，核心目标是让 Prometheus 能发现 NapCat 报告的 QQ 离线状态。Exporter 通过 OneBot HTTP API `get_status` 读取 `data.online`，并输出 QQ 在线状态、API 采集结果和必要的采集诊断指标。

## 2. 项目边界与决策

### 2.1 独立项目

`napcat_exporter` 作为独立开源仓库、独立版本和独立发布物维护，以独立进程运行。它可以独立发布、升级、回滚和报告问题，也能让其他 NapCat 部署者复用。

独立进程在 NapCat 重启、等待登录或 OneBot API 暂时不可用期间继续提供指标端点，记录 API 采集结果；API 恢复后自动恢复在线状态采集。推荐与 NapCat 部署在同一主机，通过本机 OneBot HTTP API 查询状态。

### 2.2 明确的单一职责

项目读取 NapCat OneBot HTTP API 的状态信息并输出 Prometheus 指标。v1 的采集链路为 `get_status` → `data.online` → `/metrics`，凭据使用 OneBot Token。

### 2.3 v1 范围

- v1 支持一个 NapCat 实例对应一个 exporter 进程；多实例由多个 systemd 服务或部署实例承载，并由 Prometheus 用不同 target 区分。
- 每次 Prometheus 抓取时同步调用 OneBot HTTP API `get_status`；采集周期由 Prometheus 控制，exporter 不设后台轮询。
- v1 的主要目标是发现 NapCat 报告的 QQ 在线状态变为离线。核心指标为 QQ 在线状态和 API 采集结果。
- 短暂离线事件的历史记录、事件流新鲜度和网络适配器内部统计在后续版本单独评估。
- v1 发布平台为 Linux amd64，提供通用二进制及 Debian / Ubuntu 安装包；验收在隔离测试环境中进行。

## 3. 目标用户与使用场景

1. 个人或小型团队需要监控一台 NapCat 实例。
2. Prometheus 运维者需要判断故障来自 Prometheus 到 exporter 的抓取链路、NapCat API，还是账号状态。
3. 维护者需要通过版本化发布、问题报告和自动化验证安全地迭代 exporter。

## 4. 目标架构

```text
NapCat OneBot HTTP API（建议仅本机或受限网络可访问）
                │ 本机只读查询
                ▼
napcat_exporter（独立、非特权服务）
                │ GET /metrics，部署者配置的受限网络
                ▼
Prometheus（部署位置由运营者决定）
                │ 可选 remote_write
                ▼
兼容的远端指标平台
```

Prometheus 主动抓取 exporter。推荐 exporter 与 NapCat 位于同一主机；跨主机抓取时，运营者按自己的网络与防火墙策略开放 exporter 的指标端点。远端存储由 Prometheus 的 `remote_write` 配置承接。

公开仓库中的部署示例使用占位符和通用网络示意。实际地址、主机名、凭据、Prometheus 部署位置与网络策略保存在部署者的本地配置中。

## 5. 功能需求

### F-1：读取 NapCat API

- 接收 OneBot HTTP API 基地址，通过 `POST /get_status` 查询，请求体为 `{}`。启用鉴权时使用 `Authorization: Bearer <OneBot Token>`。
- 校验 HTTP 成功状态、JSON 结构、OneBot `status="ok"`、`retcode=0`，以及 `data.online` 为布尔值；全部满足时，本轮采集成功。
- 在线状态以 `get_status.data.online` 为唯一来源：`true` 映射为 1，`false` 映射为 0。请求失败、鉴权失败或响应校验失败时，在线状态为未知。
- API 地址和超时可配置；HTTP 客户端使用有限超时。
- v1 状态采集固定调用 `get_status`，每轮执行一次请求，不重试、不跟随重定向。上游版本查询留待后续版本。

NapCat 文档中的 `get_status` 成功响应包含 `data.online` 与 `data.good`。上游提交 `810052c605feb5044a080adfe99ba2fa69b7b351` 以 `!!core.selfInfo.online` 提供 `online`，`good` 固定为 `true`、`stat` 固定为空对象。v1 采集其中动态变化的 `online`；被踢回调和账号状态回调 `status=20` 均会将核心在线值更新为 `false`。[OneBot API 列表](https://napneko.github.io/onebot/api) [get_status 接口说明](https://napcat.apifox.cn/226657083e0) [GetStatus 实现](https://github.com/NapNeko/NapCatQQ/blob/810052c605feb5044a080adfe99ba2fa69b7b351/packages/napcat-onebot/action/system/GetStatus.ts) [核心状态更新](https://github.com/NapNeko/NapCatQQ/blob/810052c605feb5044a080adfe99ba2fa69b7b351/packages/napcat-core/index.ts#L256)

正常响应的状态相关字段示例：

```json
{
  "status": "ok",
  "retcode": 0,
  "data": {
    "online": true,
    "good": true,
    "stat": {}
  }
}
```

`data.online` 为 `true` 或 `false`；v1 的在线状态判定使用这个布尔字段。其他响应封装字段包括 `message`、`wording`、`echo`、`stream`。

OneBot 业务错误可以通过 HTTP 200 返回。HTTP action 封装在参数校验失败时返回 `status="failed"`、`retcode=400`、`data=null`，在执行异常时返回 `status="failed"`、`retcode=200`、`data=null`。HTTP 适配器鉴权失败返回 HTTP 403。Exporter 按 HTTP 状态与 JSON 业务结果共同判定采集结果。[响应封装](https://github.com/NapNeko/NapCatQQ/blob/810052c605feb5044a080adfe99ba2fa69b7b351/packages/napcat-onebot/action/OneBotAction.ts) [HTTP 鉴权](https://github.com/NapNeko/NapCatQQ/blob/810052c605feb5044a080adfe99ba2fa69b7b351/packages/napcat-onebot/network/http-server.ts#L117)

### F-2：Prometheus 指标端点

- 暴露标准 `GET /metrics`，使用所选实现语言中维护良好的 Prometheus 客户端库与标准 exposition 格式。
- 提供 `GET /healthz` 检查 exporter 进程状态；OneBot API 的采集结果由 `napcat_api_up` 表示。
- Prometheus 自带的 `up{job="napcat"}` 表示抓取 exporter 成功；它与 exporter 输出的 `napcat_api_up` 是不同故障层。
- 指标名遵循 Prometheus 命名习惯，单位使用 `_seconds`、`_total` 等约定；帮助文本说明含义、来源和局限。

### F-3：v1 指标契约

| 指标 | 类型 | 定义 |
|---|---|---|
| `napcat_api_up{action="get_status"}` | Gauge | 最近一轮 `get_status` 通过 F-1 的全部校验为 1，否则为 0。 |
| `napcat_online` | Gauge | 最近一轮有效响应的 `data.online`：`true` 为 1，`false` 为 0；状态未知时不输出该样本。 |
| `napcat_last_successful_collection_timestamp_seconds` | Gauge | 最近一次 `get_status` 采集成功的 Unix 时间戳；首次成功前不输出该样本。 |
| `napcat_scrape_duration_seconds` | Gauge | 本轮 `get_status` 请求与响应处理耗时。 |
| `napcat_exporter_build_info` | Gauge | exporter 自身的版本与源码 revision 信息，值恒定为 1，不查询 NapCat 版本。 |

#### F-3.1：响应与状态指标映射

下表适用于 exporter 正常运行并完成本轮采集的情况。每轮状态采集执行一次 `get_status`。

| 请求与响应结果 | `napcat_api_up{action="get_status"}` | `napcat_online` | 状态含义 |
|---|---|---|---|
| HTTP 成功，`status="ok"`、`retcode=0`、`data.online=true` | 1 | 1 | 采集成功，NapCat 报告 QQ 在线。 |
| HTTP 成功，`status="ok"`、`retcode=0`、`data.online=false` | 1 | 0 | 采集成功，NapCat 报告 QQ 离线。 |
| HTTP 成功，但 `status` 非 `"ok"` 或 `retcode` 非 0 | 0 | 本轮不输出 | OneBot 业务响应未满足成功条件，在线状态未知。 |
| HTTP 401 / 403 鉴权失败 | 0 | 本轮不输出 | 凭据未通过验证，在线状态未知；当前 NapCat HTTP 适配器返回 403。 |
| 其他 HTTP 非成功状态，例如 404、500、502 | 0 | 本轮不输出 | 接口请求失败，在线状态未知。 |
| DNS 错误、连接拒绝、连接中断、超时等网络错误 | 0 | 本轮不输出 | 未取得有效响应，在线状态未知。 |
| 响应不是有效 JSON，或根对象、`status`、`retcode`、`data` 等必要结构缺失或类型错误 | 0 | 本轮不输出 | 响应校验失败，在线状态未知。 |
| 成功响应中的 `data.online` 缺失，或为 `null`、字符串、数字等非布尔值 | 0 | 本轮不输出 | 在线字段校验失败，在线状态未知。 |

状态组合为 `1/1`（在线）、`1/0`（离线）或 `0/无样本`（未知）。采集失败后，本轮指标响应省略 `napcat_online`，上一轮成功的在线值只保留在历史时序中。

#### F-3.2：诊断指标与抓取结果

- `napcat_last_successful_collection_timestamp_seconds`：在线、离线响应均更新为本轮成功采集时间；失败时保留上次成功时间，首次成功前不输出。
- `napcat_scrape_duration_seconds`：请求成功或失败均输出本轮耗时。
- Prometheus 自带的 `up`：成功抓取 exporter 为 1，抓取失败为 0。NapCat API 采集失败时，exporter 仍能成功返回指标，因此可以同时出现 `up=1` 与 `napcat_api_up=0`。抓取失败时，Prometheus 记录 `up=0`，本轮无法取得 exporter 的新样本。
- `napcat_exporter_build_info`：来自构建时注入的信息；上游 `napcat_build_info` 留待后续版本。

`napcat_api_up` 同时表达当前在线状态是否可读取。v1 使用上述单一状态来源，核心状态指标为 `napcat_api_up` 与 `napcat_online`。

指标标签使用 action 名、版本等有限集合。QQ 身份、消息内容、Token 和 API 错误正文保留在指标之外。Prometheus target 的 `instance` 标签由 Prometheus 配置。

### F-4：配置和凭据

- 支持 flags 或环境变量配置 OneBot API URL、Token、监听地址、API 超时、metrics 路径和日志级别。显式命令行参数优先于环境变量，环境变量优先于内置默认值。
- OneBot Token 通过 `--napcat.token` 或环境变量 `NAPCAT_TOKEN` 直接传入，填入请求的 bearer 鉴权头；不提供 Token 文件读取功能。Token 的有效性由 NapCat 的 OneBot 配置决定；更换 Token 后更新启动参数或环境变量并重启 exporter。未配置 Token 时不发送 Authorization 头，适用于未开启鉴权的 OneBot HTTP API。
- 日志、指标及配置诊断输出不得回显 Token；公开示例仅使用占位符。
- 推荐 NapCat 的 OneBot HTTP API 绑定回环地址，exporter 从本机访问。
- exporter 的 web listener 默认绑定回环地址。跨主机抓取必须由部署者显式配置监听地址并限制来源网络。
- 配置错误应在启动时指出字段名和修复方向，不回显凭据。

### F-5：日志与故障语义

- 结构化日志记录 action 名、状态码类别、耗时和脱敏错误类别；不记录认证头、完整请求/响应体或 QQ 身份资料。
- 网络错误、超时、401/403、HTTP 非成功、JSON 无效、OneBot 非成功 `retcode` 应能区分诊断。
- 上游请求失败时，`/metrics` 继续输出 exporter 自身指标、`napcat_api_up{action="get_status"}=0` 和采集耗时，本轮在线状态为未知。
- 每轮最多执行一次状态请求，失败后等待下一轮采集。

## 6. 安全与部署需求

- 以独立系统用户运行，禁止 root、交互登录和不必要的 Linux capabilities。
- systemd 示例使用 `NoNewPrivileges=true`、`PrivateTmp=true`、只读系统目录及适当的文件系统限制；权限必须经过可运行性验证。
- exporter 指标端点只允许部署者指定的 Prometheus 来源访问；具体地址由部署者在本地防火墙策略中配置，不写入项目默认值或公开示例。
- Prometheus 抓取 exporter 的 `/metrics`；OneBot API 访问权限按 exporter 的来源配置。
- 限制高基数标签；不把账号 ID、群、用户、事件正文作为标签。
- API 错误不得触发重启 NapCat、重新登录或主动 QQ 操作。

## 7. 非功能需求

- **轻量**：提供适用于 Linux amd64 的发布产物，尽量减少额外运行时依赖；持续占用和每轮 API 请求数尽可能低。
- **及时**：支持 15 秒采集周期；默认请求超时建议 3 秒，须可配置并确保小于 Prometheus `scrape_timeout`。
- **健壮**：处理 NapCat 重启、API 不可达、响应字段缺失和非成功业务状态；恢复后下轮采集自动恢复，不需要 exporter 重启。
- **可维护**：指标和配置契约有版本化文档；配置项有默认值及示例；变更有 changelog 和升级说明。
- **可移植**：核心业务不依赖 systemd；systemd unit 是部署样例，便于后续支持其他 init / 容器环境。
- **可观测**：Prometheus `up` 表示抓取结果，`napcat_api_up` 表示 OneBot 状态采集结果，`napcat_online` 表示 NapCat 报告的 QQ 在线状态。

## 8. 开源与维护要求

- 采用 MIT 许可证，便于个人与组织自由使用、修改和再分发；分发时保留版权与许可证声明。
- 公开仓库包含 README、LICENSE、CHANGELOG、CONTRIBUTING、CODE_OF_CONDUCT、SECURITY、示例配置、systemd unit 和版本兼容说明。
- 发布页提供 Linux amd64 发布产物、版本号、SHA-256 校验和及对应源码 tag；发布构建可复现或至少由 CI 自动生成并附带 provenance。
- CI 至少执行格式检查、静态分析、单元测试、适用的并发安全检查和 Linux amd64 构建；测试不得依赖真实 QQ 账号或在线 NapCat 实例。
- 使用本地 HTTP 测试服务器模拟 `get_status` 的在线、离线响应，以及业务错误、鉴权错误、超时、损坏 JSON、字段缺失和字段类型错误；验证在线状态与采集结果的映射。
- 新增指标视为公开接口变更：保持向后兼容；移除或改名指标需遵循 deprecation / major version 策略，并更新升级指南。
- README 标明社区维护者、项目用途和支持的 NapCat 版本。

### 8.1 测试设计约束：单一事实来源

- 禁止在测试中重新硬编码生产代码已有的默认端口、超时、路径、配置键、指标名、包名、service 名、用户名称等项目事实。项目事实由其所属的配置定义、指标定义或打包源文件统一维护；测试读取或引用该来源，不另建一份期望值清单。
- 禁止仅比较某个值与生成它所用的同一常量，或把同一常量同时传入实现与期望值后断言相等；此类断言不构成有效行为验证。
- 禁止复制被测实现的分支、映射或序列化算法来计算期望结果；禁止把当前实现输出直接保存为整份 golden/snapshot 并作为正确性依据。
- 配置测试使用测试独立生成的不同输入，验证优先级、非法输入拒绝及实际生效行为，不重新列举默认值。时间相关测试验证边界、更新和保留关系，不重复默认超时，也不依赖精确的真实耗时相等。
- 指标测试解析真实 HTTP 指标响应，验证成功、失败和恢复时的样本出现或消失、状态映射、最近成功时间及请求次数；固定名称和编码引用唯一契约定义。单纯的常量一致性检查不能替代这些验证。
- 打包测试安装实际生成的包并操作实际服务，验证非特权运行、升级保留用户修改、停用状态保持、卸载和重复安装行为。名称、路径及 unit 内容从实际打包来源取得，不在测试中另写一份安装布局或完整 unit。
- Token 直接通过参数或环境变量传入；不编写 Token 文件权限、读取、轮换或删除测试。脱敏测试使用测试生成的 Token，验证请求鉴权有效且日志、指标和诊断输出不包含该值。
- 每项测试必须指出可观察的行为及其能够发现的错误。无独立行为判断的实现复述测试不纳入测试集，也不以覆盖率为由添加。

## 9. 告警使用建议

Prometheus 侧以“持续无法确认 QQ 在线”为主告警，将明确离线、API 采集失败和 exporter 无法抓取连续计时。以下指标用于区分异常原因：

- `up{job="napcat"} == 0`：Prometheus 无法抓取 exporter。
- `napcat_online == 0`：NapCat 报告 QQ 离线。
- `napcat_api_up{action="get_status"} == 0`：本轮状态采集失败，当前 QQ 在线状态未知。
- `time() - napcat_last_successful_collection_timestamp_seconds > 60`：状态数据超过预期时间未更新。

示例告警持续一分钟后触发，重新取得有效的在线响应后解除。QQ 下线到 Worker 重启之间可能只有短暂窗口，采样可能直接观察到 API 不可用；主告警覆盖这一状态转换。最近成功时间用于排查采集新鲜度。规则行为通过 `examples/alerts.test.yml` 验证。

## 10. 已知限制与后续能力

`get_status.online` 表达 NapCat 当前维护的核心在线值。QQ 内核状态回调的到达时间会影响状态更新时刻，支持矩阵应验证目标 NapCat / QQ 版本的行为。OneBot HTTP 服务在登录后的适配器初始化阶段创建；首次等待登录、Worker 重启等阶段可能无法访问，此时 exporter 继续报告采集失败。[核心状态更新](https://github.com/NapNeko/NapCatQQ/blob/810052c605feb5044a080adfe99ba2fa69b7b351/packages/napcat-core/index.ts#L256) [OneBot 初始化](https://github.com/NapNeko/NapCatQQ/blob/810052c605feb5044a080adfe99ba2fa69b7b351/packages/napcat-onebot/index.ts#L163)

定期查询记录采样时刻的状态。采样间隔内发生又恢复的短暂离线，可能无法被轮询捕获；记录每次离线需要事件采集能力。

可考虑的后续版本：

1. 采集离线事件时间、次数和事件流新鲜度；根据公开事件的覆盖范围，评估是否增加辅助插件读取内部事件。
2. 多实例配置或 target 模式；须先处理 SSRF、凭据隔离和标签身份设计。
3. ARM64 二进制、更多发行版的软件包（如 deb / rpm）、Windows 服务支持。
4. 网络配置数、配置启用数、反向 WebSocket 连接状态等统计；若采集内部适配器状态，单独评估插件配合方式和版本维护成本。

## 11. 验收标准

### v1 发布验收

- [ ] 在受支持的 Linux amd64 测试环境中以非 root 用户启动，能响应 `/healthz` 和 `/metrics`。
- [ ] 对模拟 `get_status` 响应正确输出 `napcat_api_up{action="get_status"}`、`napcat_online`、`napcat_last_successful_collection_timestamp_seconds` 和采集耗时指标；在线、离线和采集失败情况均有覆盖。
- [ ] 覆盖 F-3.1 的全部映射，包括 HTTP 200 的业务失败、HTTP 鉴权错误、网络错误、无效 JSON、必要结构缺失，以及 `online` 为 `null`、字符串或数字等情况；验证 `1/1`、`1/0` 和 `0/无样本` 三种组合。
- [ ] 在线和离线响应均更新最近成功时间；首次成功前省略时间戳，失败后保留上次成功时间；每轮均输出耗时。
- [ ] 对超时、鉴权失败、业务 `retcode` 非零、JSON 损坏和字段缺失能报告失败；不把旧在线值保留为本轮状态。
- [ ] Prometheus 能稳定抓取；target `up`、QQ 在线状态与 OneBot API 采集结果可单独解释。
- [ ] 日志、指标和配置诊断输出不回显 Token；命令行示例及仓库测试数据不包含真实认证信息和 QQ 身份信息。
- [ ] systemd 示例可运行，服务不以 root 身份运行；关闭或重启 exporter 不会操作 NapCat / QQ。
- [ ] CI 通过，Linux amd64 发布产物有版本、源码 tag 和 SHA-256 校验值。
- [ ] 文档说明 `/metrics` 的暴露边界、NapCat 状态指标限制、配置方式和最小告警示例。

### 目标部署验收

- [ ] 部署前已核对 NapCat 服务、版本与 OneBot HTTP 监听、鉴权配置。
- [ ] exporter 使用部署者配置的 OneBot Token 成功调用 `get_status`，响应业务成功且 `data.online` 为布尔值。
- [ ] 运营者配置的 Prometheus 抓取目标为 `UP`。
- [ ] Prometheus 查询中能看到 exporter 抓取状态、QQ 在线状态与 OneBot API 采集结果。
- [ ] 在支持版本的测试环境中验证 `get_status` 在线、离线响应和 API 不可用时的采集结果；NapCat 恢复后下一轮采集自动恢复。
- [ ] NapCat 停止或重启期间，exporter 进程及 `/metrics` 持续可用。
- [ ] 网络策略只允许预期的 Prometheus 来源访问 exporter。
- [ ] 部署文档和诊断材料已清除实际地址、主机名、域名、认证凭据及其他环境专属信息。

## 12. 技术约束

1. 使用 Go，工具链以 `go.mod` 为准；配置定义集中在 `internal/config/config.go`。技术决策见 `decisions.md`。
2. 支持 flags 和环境变量，Token 直接传入；systemd 环境文件由服务管理器读取。deb 首次安装不启动、不启用，升级保留配置与停用状态。
3. 并发抓取分别生成指标快照，最近成功时间在进程内并发安全地维护，重启后清空。上游不可用时 `/metrics` 返回 HTTP 200，并省略在线样本；达到并发上限时返回 HTTP 503。
4. OneBot 接口契约见[兼容性](compatibility.md)，测试及部署检查方法见[验证指南](validation.md)。

---

## 参考资料

- [NapCat API 兼容情况](https://napneko.github.io/develop/)
- [NapCat OneBot API：get_status](https://napcat.apifox.cn/226657083e0)
- [NapCat get_status 实现](https://github.com/NapNeko/NapCatQQ/blob/810052c605feb5044a080adfe99ba2fa69b7b351/packages/napcat-onebot/action/system/GetStatus.ts)
- [NapCat 核心在线状态更新](https://github.com/NapNeko/NapCatQQ/blob/810052c605feb5044a080adfe99ba2fa69b7b351/packages/napcat-core/index.ts#L256)
- [OneBot HTTP 适配器](https://github.com/NapNeko/NapCatQQ/blob/810052c605feb5044a080adfe99ba2fa69b7b351/packages/napcat-onebot/network/http-server.ts)
- [Prometheus exporters and integrations](https://prometheus.io/docs/instrumenting/exporters/)
- [NapCat issue：账号静默离线与状态滞后报告](https://github.com/NapNeko/NapCatQQ/issues/2071)
