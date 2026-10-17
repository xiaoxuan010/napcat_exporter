# 兼容性

## 运行平台

发布物面向 Linux amd64，使用纯 Go 构建。二进制可独立运行，部署时需要提供 OneBot HTTP API 地址，以及启用鉴权时使用的 Token。

Debian / Ubuntu 用户可以使用 deb 包安装。包依赖 `adduser` 和 `init-system-helpers`，服务管理使用 systemd。其他 Linux 发行版可以使用 tar.gz 中的二进制，结合各自的服务管理器部署。

## OneBot 接口契约

Exporter 通过 `POST /get_status` 查询状态，兼容性取决于以下响应字段：

- HTTP 状态为 2xx。
- `status` 为字符串 `"ok"`。
- `retcode` 为整数 `0`。
- `data` 为对象，`data.online` 为布尔值。

额外字段会被忽略。必要字段缺失、null、类型错误、HTTP 错误或 OneBot 业务错误均视为采集失败。HTTP 200 中的业务错误同样适用。

## 状态语义

`data.online=true` 表示 NapCat 报告 QQ 在线，`false` 表示离线。采集失败时在线状态为未知，指标响应省略 `napcat_online`。NapCat 的核心状态回调会影响在线值更新的时刻；两次采样之间发生并恢复的短暂离线可能被漏过。

OneBot HTTP 服务在登录后的适配器初始化阶段创建。等待登录或 Worker 重启期间，API 可能不可访问。QQ 下线后自动重启 Worker 时，指标可从离线转为未知；告警示例将这两种异常连续计时，直到重新确认在线。

验证方法见[验证指南](validation.md)。
