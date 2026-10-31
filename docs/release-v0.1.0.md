本版本提供的是手工安装压缩包，不能直接交给 apk/opkg 或 LuCI 软件包页面。解压后运行 `sh install.sh`；原生 APK/IPK 请使用 [v0.1.1](https://github.com/puppywang/openwrt-nintendo-guard/releases/tag/v0.1.1)。

首个公开预览版。面向使用 firewall4/nftables 的 OpenWrt/PonWrt。

- LuCI 页面按 MAC 配置关闭、系统更新域名拦截、设备外网隔离。
- DNS + HTTPS SNI / HTTP Host 过滤，不解密 TLS。
- 可配置上游 DNS，默认使用本机 127.0.0.1:53。
- 持久化配置、procd 自启、规则恢复、最近拦截记录。
- ARM64、AMD64、ARMv7、MIPS/MIPSLE 静态二进制安装包及 SHA256SUMS。

首次安装默认关闭，请在 LuCI 中填写自己的设备 MAC。安装前核对架构和 SHA256，按 README 操作；卸载脚本随包提供。

原始实现已在 ZN504/PonWrt 与 Switch 2 上实测；其他硬件包仍需实机验证。全部模式会阻止第三方游戏联网。第一档可能影响部分商店/游戏/DLC 功能，不保证永久阻止所有固件升级；已下载的固件、离线来源和其他网络不受路由器控制。
