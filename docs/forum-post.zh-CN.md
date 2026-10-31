# 做了个 OpenWrt / PonWrt 小工具：按 MAC 控制 Switch 2 系统更新访问

最近想让 Switch 2 保持当前固件，同时尽量保留正常游戏下载，就做了一个 LuCI 控制页面，整理后开源了。

项目：https://github.com/puppywang/openwrt-nintendo-guard

下载：https://github.com/puppywang/openwrt-nintendo-guard/releases

提供三种状态：

- 关闭拦截。
- 只拦截已知系统更新检查和固件下载域名。
- 拦截全部任天堂服务：这一档直接断开该设备的路由外网访问，第三方游戏联网也会停止。

按设备 MAC 匹配，不依赖固定 DHCP 地址。除了 DNS 过滤，还检查 HTTPS SNI / HTTP Host，处理主机继续用缓存 IP 的情况；不解密 HTTPS，也不用安装证书。界面能看到生效状态、计数和最近拦截记录。

我在 ZN504 / PonWrt 上测试了 Switch 2：普通联网、普通游戏下载正常，系统更新检查请求被拦截。这个结果只代表这套环境，欢迎有其他硬件的朋友反馈。

使用需要 OpenWrt/PonWrt 的 firewall4/nftables 和现代 LuCI。下载对应包管理器架构的后端 APK/IPK 和 LuCI 包，核对 SHA256，再用 apk/opkg 安装，然后在“网络 → Nintendo 控制”里填写自己的 MAC。首次安装默认关闭，不会自动屏蔽设备。

几个边界先说明：它不是永久锁固件工具，不能绕过新游戏的最低系统版本要求。已下载到主机的固件、离线来源或其他网络不受它控制；第一档拦截最低版本查询时，也可能影响部分游戏/DLC 下载。开启保护还会限制该设备的 IPv6、QUIC 等流量，具体见 README。不要为了测试而主动安装系统更新。

目前是早期版本，MIT 许可，有源码、预编译包和卸载脚本。反馈时请附路由器型号、系统版本和错误表现，记得隐藏个人网络信息。
