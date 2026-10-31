补齐原生 OpenWrt/PonWrt 安装包：APK v3 和 IPK。二进制及过滤行为与 0.1.0 相同，更新了版本标识和分发方式。

## 选择安装包

- 新版 PonWrt / OpenWrt（`apk --print-arch` 有输出）：下载后端对应架构 `.apk` 和前端 `luci-app-nintendo-guard-0.1.1-r1.noarch.apk`。
- 使用 opkg 的 OpenWrt：下载后端对应架构 `.ipk` 和前端 `luci-app-nintendo-guard_0.1.1-1_all.ipk`。需要 firewall4。
- 包管理器会登记安装文件、依赖和版本，支持正常升级/卸载。首次安装默认关闭，已有设备配置保留。

ZN504 / PonWrt 的 `aarch64_cortex-a53` 示例，在路由器上的两个文件所在目录执行：

```sh
apk add --allow-untrusted ./nintendo-guard-0.1.1-r1.aarch64_cortex-a53.apk ./luci-app-nintendo-guard-0.1.1-r1.noarch.apk
```

这些是项目自行构建的第三方包，未使用 OpenWrt 官方签名；`--allow-untrusted` 用于本次本地文件安装。请先核对 SHA256SUMS。

旧版 OpenWrt 的对应命令：

```sh
opkg install ./nintendo-guard_0.1.1-1_aarch64_cortex-a53.ipk ./luci-app-nintendo-guard_0.1.1-1_all.ipk
```

重登 LuCI 后进入“网络 → Nintendo 控制”。仍提供 `.tar.gz` 供手工安装；原生安装后请使用包管理器升级/卸载，不要再运行压缩包里的卸载脚本。

脚本安装迁移到原生包时，建议先自行备份 `/etc/nintendo-control.json`。包的安装过程会保留现有 JSON 内容。
