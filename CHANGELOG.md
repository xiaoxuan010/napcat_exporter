# 更新记录

## 功能与接口

- 实现单实例、同步抓取的 NapCat OneBot `get_status` exporter。
- 支持 flags 与环境变量配置，直接传入 Token，配置诊断和日志脱敏。
- 区分在线、离线和未知状态，输出最近成功时间与采集耗时。
- 增加 Prometheus、进程及 deb/systemd 生命周期验收。
- 增加 GitHub Actions 检查、漏洞扫描、GoReleaser 发布与来源证明配置。

指标与配置为公开接口；不兼容变更需明确标注并提供升级说明。
