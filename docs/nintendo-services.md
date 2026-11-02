# 任天堂服务过滤模式

`nintendo` 模式拦截已知任天堂服务，保留其他域名的 DNS、HTTP/HTTPS 和普通 IPv4 TCP/UDP。`all` 始终表示整台设备的外网隔离，0.3.0 起界面明确显示为“禁止该设备全部外网”。升级不自动切换已有设备等级。

## 域名依据

2026-11-02 核对了 90DNS 上游提交 [`4d477d7181188690203aae5f4e56e287edd21734`](https://gitlab.com/a/90dns/-/commit/4d477d7181188690203aae5f4e56e287edd21734) 的以下配置：

- [dnsmasq 域名规则](https://gitlab.com/a/90dns/-/blob/4d477d7181188690203aae5f4e56e287edd21734/dnsmasq/dnsmasq.conf)
- [BIND9 Nintendo 域与联网检测记录](https://gitlab.com/a/90dns/-/blob/4d477d7181188690203aae5f4e56e287edd21734/BIND9/zones/nintendo.net)
- [联网检测 HTTP 响应](https://gitlab.com/a/90dns/-/blob/4d477d7181188690203aae5f4e56e287edd21734/nginx/nintendowifi)

域名事实整理为 [`internal/nintendoguard/nintendo-domains.txt`](../internal/nintendoguard/nintendo-domains.txt)，随二进制内置，不在运行时下载。共 36 个主域或独立端点，覆盖 Nintendo 地区域名、`nintendowifi.net`、`nintendoswitch.*` 和两个专用 AWS 上传主机。按“域名本身或点分子域名”匹配，避免把 `notnintendo.net` 或其他 AWS 租户一起拦截。

插件独立实现过滤逻辑，不安装 90DNS 的服务器配置，不使用其公共 DNS 或联网检测服务器。90DNS BIND9 配置中的 Sony 和通用分析域名不属于本模式范围。

## 处理方式

- DNS：命中的 A 查询返回 `0.0.0.0`，AAAA 返回 `::`，其他记录类型返回空答案；普通查询转发到已配置上游。上游 CNAME/DNAME 指向被拦端点时同样拦截。
- HTTP/HTTPS：按 Host/SNI 检查同一清单，覆盖使用缓存 IP 的连接，不解密 TLS，不按共享 CDN IP 进行整段封禁。
- 联网检测：仅 `conntest.nintendowifi.net` 和 `ctest.cdn.nintendo.net` 由 DNS 返回路由器地址，HTTP 在路由器本地返回 200 和 `X-Organization: Nintendo`。它们不连接任天堂或第三方检测服务器。HTTPS 不作证书仿冒。其他 Switch 2 检测 API 未仿真，可能显示联网检测失败。
- 端口：5356 提供此模式的 DNS，5444 提供透明 HTTP/HTTPS；仅此模式登记的 MAC 可以访问。与 updates（5354/5443）和 all（5355）分开。

可在启用了该模式的主机上验证：任天堂域名得到阻断地址，其他自有域名正常解析和访问。路由器本机可查询 `127.0.0.1:5356` 做策略自测；没有设备使用该模式时此端口返回 REFUSED。自测日志不代表主机实际请求。

## 范围

任天堂商店、下载、联机和遥测会受影响。为限制常见绕过路径，本模式仍阻止目标的 IPv6 转发、UDP 443（QUIC）、TCP/UDP 853，以及无法识别域名的 HTTP/TLS。

新端点、直接 IP 的其他协议、外部隧道或其他网络不属于完整覆盖范围。本模式提供已知服务过滤；需要整台设备完全隔离时选择 `all`。已下载的系统固件和离线更新均不由路由器控制。
