# 安全报告

安全修复面向最新发布版本。

安全问题请通过维护者邮箱 `me@xiaoxuan010.top` 私下报告。普通功能问题使用公开 Issue。报告请对 Token、QQ 身份及响应正文进行脱敏。

exporter 只发起 `get_status` 请求，不提供任意 target 参数或 QQ 操作接口。指标 listener 默认绑定回环地址，跨主机访问由部署者的防火墙或代理控制。OneBot Token 通过环境变量或 flag 传入，程序不会将它写入日志、指标或诊断输出。
