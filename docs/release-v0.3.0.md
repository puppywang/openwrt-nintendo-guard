# v0.3.0 任天堂服务过滤预览版

新增“只拦截任天堂服务（保留其他网络）”，适合希望禁用任天堂联网、同时访问自有服务和其他网站的设备。

- 参考 90DNS 上游固定版本，内置 36 个 Nintendo 主域、地区域名和专用上传端点；保留现有上游 DNS。
- DNS 和 HTTP Host / HTTPS SNI 使用相同清单，普通域名正常放行，覆盖常见缓存 IP 访问。
- 两个 Switch HTTP 联网检测域名由路由器本地响应，无需连接外部检测服务器。
- 原 `all` 模式更名为“禁止该设备全部外网”，保持原先隔离行为；现有设备设置保留，请自行选择新模式。
- 支持 updates / nintendo / all / off 四种等级混用；增加域名边界、CNAME、联网检测、普通 TCP/UDP 放行和安装包配置保留测试。

请同时升级后端和 LuCI 包并刷新页面。详见 [过滤依据与范围](https://github.com/puppywang/openwrt-nintendo-guard/blob/v0.3.0/docs/nintendo-services.md)。新模式仍限制 IPv6、QUIC 和加密 DNS 专用端口；它按已知端点过滤，不提供整台设备完全隔离保证。
