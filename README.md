# OpenWrt Nintendo Guard

[English](README.en.md) · [下载](https://github.com/puppywang/openwrt-nintendo-guard/releases) · [反馈问题](https://github.com/puppywang/openwrt-nintendo-guard/issues)

给 OpenWrt / PonWrt 增加一个 LuCI 页面，按设备 MAC 控制 Nintendo Switch / Switch 2 的系统更新访问。

它用于减少意外获取系统更新的机会，**不是永久锁定固件，也不是破解或绕过游戏最低系统版本要求的工具**。

## 能做什么

| 模式 | 实际行为 |
| --- | --- |
| 关闭拦截 | 不重定向目标设备流量，首次安装默认此模式 |
| 只拦截系统更新 | 拦截已知更新检查、固件下载和最低版本查询域名；保留游戏 CDN |
| 拦截全部任天堂服务 | 通过断开该设备经路由器的外网/跨网段访问实现；第三方游戏联网也停止 |

- 按 MAC 匹配，DHCP 地址变化不需要重配；当前只支持一个目标设备。
- DNS 过滤 + HTTPS SNI / HTTP Host 检查，覆盖仍使用缓存 IP 的常见连接。
- 不解密 HTTPS，不安装证书；允许的连接按原始字节转发。
- 页面显示已保存配置、规则状态、设备 IP、计数和最近拦截记录。
- 配置持久保存，支持 procd 开机启动和防火墙重载恢复。
- 不依赖公共拦截 DNS、AI Proxy 或付费代理服务。

## 适用环境

需要现代 LuCI、firewall4 / nftables、procd、rpcd、`ip`、`conntrack`、`timeout`。典型 OpenWrt LAN 区域应允许访问路由器；自定义访客网、强制代理和非标准防火墙需要单独适配。

已在 ZN504 ARM64 / PonWrt / Linux 6.18 上验证原始实现：Switch 2 普通联网、系统更新检查拦截、普通游戏下载，以及两台路由器的部署。公开版新增通用 DNS 配置和安装工具；自动化测试在 Linux 网络命名空间中验证。其他硬件的预编译包属于待实机验证版本。

**路由器必须能看到主机的真实 MAC**：桥接 AP 通常可以；经过另一个 NAT 路由器后，匹配到的可能是下级路由器 MAC。Wi-Fi 和有线网卡的 MAC 也可能不同。

## 安装

从 Releases 下载原生安装包，先用 **包管理器报告的架构** 选包，而不是只看 `uname -m`。

| 系统 | 检查架构 | 下载文件 |
| --- | --- | --- |
| 新版 PonWrt / OpenWrt，使用 apk | `apk --print-arch` | 对应架构后端 `.apk` + `luci-app-nintendo-guard-0.1.2-r1.noarch.apk` |
| 使用 opkg 的 OpenWrt | `opkg print-architecture` | 对应架构后端 `.ipk` + `luci-app-nintendo-guard_0.1.2-1_all.ipk` |

例如 ZN504 / PonWrt 的架构是 `aarch64_cortex-a53`，应下载：

- `nintendo-guard-0.1.2-r1.aarch64_cortex-a53.apk`
- `luci-app-nintendo-guard-0.1.2-r1.noarch.apk`

将这两个文件上传到路由器 `/tmp`，与 Release 的 `SHA256SUMS` 核对后安装：

```sh
cd /tmp
apk add --allow-untrusted ./nintendo-guard-0.1.2-r1.aarch64_cortex-a53.apk ./luci-app-nintendo-guard-0.1.2-r1.noarch.apk
```

这些是本项目自行构建的第三方包，未使用 OpenWrt 官方签名；`--allow-untrusted` 只用于此次本地文件安装。若 LuCI 上传安装时因签名被拒绝，请用上面的 SSH 命令。

旧版 OpenWrt 的对应安装方式（同样需要 firewall4）：

```sh
opkg install ./nintendo-guard_0.1.2-1_aarch64_cortex-a53.ipk ./luci-app-nintendo-guard_0.1.2-1_all.ipk
```

依赖会由包管理器检查。后端依赖 firewall4、nftables、conntrack、ip-full、procd 和 busybox；界面依赖 luci-base 和 rpcd。

安装完成后重新登录 LuCI，进入 **网络 → Nintendo 控制**，填写自己的主机 MAC，再选择模式并保存。首次安装默认为关闭；已有 `/etc/nintendo-control.json` 会原样保留。文件由包管理器登记，不需要另运行 `install.sh`。

上游 DNS 默认 `127.0.0.1:53`，通常是路由器的 dnsmasq。使用其他 DNS 服务时填写实际 **IP:端口**。不要指向本程序的 `5354`，也不要让上游 DNS 转回本程序。脚本安装迁移到原生包前，可先备份该 JSON。

### 压缩包手工安装（含旧版 v0.1.0）

`.tar.gz` 是另一种分发方式，不能直接交给 apk/opkg 或 LuCI 软件包上传页面。若要使用，下载匹配 CPU 的包，解压后执行：

```sh
tar -xzf nintendo-guard-v0.1.2-linux-arm64.tar.gz
cd nintendo-guard-v0.1.2-linux-arm64
sh install.sh
```

v0.1.0 下载包把上述版本改成 v0.1.0 即可。`aarch64` 用 arm64，`x86_64` 用 amd64，`armv7l` 用 armv7；MIPS 需要确认端序。手工安装会检查依赖并备份原文件到 `/root/nintendo-control-backup-*`，不会进入包管理器数据库。

## 怎样测试

先做普通联网或游戏下载测试，在页面看目标 MAC 的连接和拦截记录。**无需为了测试主动发起本体更新**。

可在路由器上安全查询 DNS 策略：

```sh
nslookup sun.p01.lp1.d4c.srv.nintendo.net 127.0.0.1:5354
nslookup atum.p01.lp1.d4c.srv.nintendo.net 127.0.0.1:5354
```

在 updates 模式中，第一个应返回 NXDOMAIN，第二个应由上游正常解析。这些自测记录的来源是路由器本机，不代表主机实际请求。off 模式下直接查询保护端口会收到 REFUSED，正常设备 DNS 不经过该端口。

查看状态：`nintendo-guard status`。规则检查显示的是配置/专属表状态；仍需结合目标设备的实际流量验证整个网络路径。

## 边界与副作用

- **不是“永不升级”保证**。已知域名之外的新端点、特殊协议、共享 TLS 连接中的其他请求等不一定被第一档覆盖。
- `aqua` 最低系统版本查询也被拦截，部分游戏/DLC 下载、商店功能可能受影响。保留 `atum` 不代表所有游戏永远可下载。
- 启用时会阻止目标的 IPv6 转发、QUIC（UDP 443）、TCP/UDP 853；无法识别域名的 HTTP/HTTPS、ECH 和无 SNI TLS 会被拒绝。
- 切换等级会重建目标相关连接。全部模式还会清理目标现有 conntrack，包括流卸载状态；部分厂商专有加速可能需要重新连接主机网络。
- 无法控制已保存在主机的固件、离线介质内若存在的固件安装，或改用其他 Wi-Fi/热点/下级代理的通信。
- 全部模式禁止路由转发；同一二层网络的直连通信不经过这条转发规则。开启 bridge-netfilter 的环境需要单独验证。
- 不修改全局 IPv6 设置或其他设备的网络规则。重刷路由器固件不等于普通重启，刷机后需要重新检查安装是否保留。
- 服务异常退出时保留规则，目标可能无法联网；这是保留阻断的设计。恢复联网请在 UI 关闭或按下方卸载。

## 关闭与卸载

临时关闭：在页面选择“关闭拦截”并保存。

原生包安装请使用对应的包管理器：

```sh
apk del luci-app-nintendo-guard nintendo-guard
# 使用 opkg 的系统：
opkg remove luci-app-nintendo-guard nintendo-guard
```

移除时会停止服务、清除本功能规则及相关连接，并保留设备配置 JSON 供重装使用。只移除界面包会保留后端服务，请同时移除两个包以完整卸载。

压缩包手工安装才使用 `sh uninstall.sh`。脚本和原生包的卸载方式不要混用。

配置位于 `/etc/nintendo-control.json`，管理接口为权限 0600 的 Unix socket。网页沿用 LuCI 登录和专用 rpcd ACL。没有遥测、云端账号或额外 HTTP 管理端口。

## 构建与开发

需要 Go 1.24.4+、Python 3；测试构建使用 Go 1.25。

```sh
go test ./...
go vet ./...
python3 scripts/build.py --version v0.1.2
```

原生包构建还需 Linux 和带 mkpkg 的完整 apk-tools 3。可先执行 `sh scripts/build_apk_host.sh`，再执行 `fakeroot python3 scripts/build_native.py --version v0.1.2 --apk build/apk-tools/out/src/apk`。

构建结果在 `dist/v0.1.2/`，包含安装脚本、对应架构的静态二进制、许可证和 SHA256SUMS。`--arch arm64` 可只构建一个架构。

Linux 网络测试需要 root、nftables、conntrack、iproute2、openssl，并只在隔离网络命名空间内运行：

```sh
go build -o build/nintendo-guard ./cmd/nintendo-guard
sudo timeout 90 unshare --net python3 tests/network_integration.py build/nintendo-guard
```

GitHub Actions 运行单元测试、race detector、网络测试及多架构构建。欢迎提交具体型号、固件版本和去掉私人信息的日志；不要上传路由器备份、代理密钥或抓包中的个人数据。

## GitHub Actions 自动打包与发布

整个构建和发布流程位于 [`.github/workflows/ci.yml`](.github/workflows/ci.yml)：

- main 提交、PR、手动 Run workflow：执行测试并生成 `release-bundles` Artifact，可从对应 Actions 运行页面下载。
- 推送 `vX.Y.Z` 标签：全部测试通过后，自动把本次运行的 20 个文件发布为 GitHub 预览版 Release。
- 发布前检查完整文件清单及 SHA256，上传后比对 GitHub 返回的摘要，通过后公开草稿。
- 失败的草稿可重跑补传；已公开版本只核验，不覆盖现有资产。更改代码请使用新版本标签。

发布使用仓库自动提供的 `GITHUB_TOKEN`，仅发布任务申请 `contents: write`，无需设置个人 Token 或手动上传二进制。

维护者准备好修改后，更新 `VERSION`，可选添加 `docs/release-vX.Y.Z.md`，提交并推送，然后推送标签。例如下一版：

```sh
git tag v0.1.3
git push origin v0.1.3
```

不必先在 GitHub 网页创建 Release。有对应说明文件时采用该文件，否则自动生成发布说明。当前版本仍以预览版形式发布。

## 域名依据

- [NintendoClients 服务列表](https://github.com/kinnay/NintendoClients/wiki/Server-List#switch-2)
- [Aqua 最低固件查询](https://nintendo-wiki.pretendo.network/docs/switch/aqua)
- [Nintendo Switch 2 系统更新说明](https://www.nintendo.com/au/support/articles/system-update-information-for-nintendo-switch-2/)

MIT License。与 Nintendo 无隶属或认可关系；商标属于其权利人。
