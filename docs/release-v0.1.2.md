本版本将测试、编译、打包和 Release 发布全部交给 GitHub Actions。推送 `vX.Y.Z` 版本标签后，工作流自动：

1. 执行 Go 单元/并发检查、网络隔离测试及 APK 安装/升级/卸载测试。
2. 构建五种 CPU 的静态二进制、原生 APK/IPK 和压缩包。
3. 校验全部 20 个发布文件及 SHA256SUMS。
4. 创建预览版草稿，上传后比对 GitHub 侧文件摘要，通过后发布。

普通 main/PR 提交及手动运行只生成 Actions Artifact。发布使用仓库自带的 GITHUB_TOKEN，不需要配置个人 Token。失败的草稿可以重跑补传；已发布资产保留。

过滤行为与 v0.1.1 相同。PonWrt 的 `aarch64_cortex-a53` 架构选择下面两个文件，核对 SHA256 后在路由器执行：

```sh
apk add --allow-untrusted ./nintendo-guard-0.1.2-r1.aarch64_cortex-a53.apk ./luci-app-nintendo-guard-0.1.2-r1.noarch.apk
```

旧版 opkg 系统选择对应架构后端 IPK 和 `luci-app-nintendo-guard_0.1.2-1_all.ipk`。首次安装默认关闭，已有 JSON 配置保留。第三方 APK 未使用 OpenWrt 官方签名；请先核对校验文件。
