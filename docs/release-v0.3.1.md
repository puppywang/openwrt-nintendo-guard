# v0.3.1 — Switch 1 / Switch 2 联网检测精确例外

发布日期：2026-11-04

## 兼容性修复

- 修复 Nintendo 服务拦截模式遗漏 Switch 2 联网检测 `ctest.p01.ctest.srv.nintendo.net` 导致系统认为离线的问题。
- 对照 [NintendoClients Connection Test](https://github.com/kinnay/NintendoClients/wiki/Connection-Test) 核对全部 9 个 Switch 1 / Switch 2 检测域名：
  - `ctest.cdn.nintendo.net`、`ctest.p01.ctest.srv.nintendo.net` 通过路由器本地 HTTP 返回 `200 OK`、`Content-Length: 2`、`X-Organization: Nintendo`、`ok`，保留旧的 `conntest.nintendowifi.net` 兼容端点。
  - Switch 1 的 HTTP 下载 / 上传测速，以及 Switch 2 的 HTTPS 下载 / 上传测速，使用真实目标、完整正文和源站 TLS，不伪造速度。
  - Switch 1/2 的 HTTPS `/v1/ip`、`/v1/time` 使用精确主机名透传，不伪造证书或 API 数据。
  - Switch 2 的 `ctest-ipv6` 仅精确放行并保留真实 DNS / HTTP 语义，不伪造 IPv6；路由器未提供 IPv6 时可能仍失败。
- 显式暴露本地响应和精确透传的主机名，LuCI 显示模式的网络影响。任天堂登录、商店、系统更新与其他服务依旧按原模式拦截，不会因近似域名或子域名绕过。
- 本地示例响应的 Date/Expires 使用当前时间，Cache-Control / Pragma 禁缓存，Connection: close 与单次请求套接字一致。不会硬编码网页上 2021/2025 年的历史时间戳，也不谎称 keep-alive。

## 验证范围

- 单元测试覆盖全部已知端点、大小写及域名边界、CNAME/DNAME、HTTP GET/HEAD 错误路径。
- Linux 网络命名空间集成测试覆盖 9 个端点的 HTTP / TLS DNS 及 1 MiB 上传转发，并验证已知更新域名仍阻断和四种设备模式隔离。
- 实机部署需核对版本、firewall_ok、设备 MAC / mode / DNS upstream、包管理状态、LuCI RPC 和网络可达性；实际 Switch 主机联网测试 / 第三方游戏须另行在主机验收。

## 适用及限制

此版只在 nintendo 模式对精确检测域名例外；updates/off/all 模式语义不变。精确 HTTPS 透传确实连接 Nintendo 服务器，不能保证所有游戏联网可用。插件不会更改路由器其他 DNS 代理、加速服务或下载站点的设置。
