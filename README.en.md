# OpenWrt Nintendo Guard

[中文说明](README.md) · [Releases](https://github.com/puppywang/openwrt-nintendo-guard/releases)

A LuCI interface and standalone Go service for per-MAC Nintendo Switch / Switch 2 update filtering on OpenWrt/PonWrt with firewall4.

Modes: **off** (installation default), **known system-update endpoints**, and **full routed internet isolation**. Full isolation also blocks third-party games and other routed destinations. This is not a permanent firmware lock and does not bypass minimum firmware requirements.

The service redirects the selected device's DNS and TCP 80/443 traffic, filters DNS names and TLS SNI / HTTP Host, and forwards allowed connections without TLS decryption. DNS upstream defaults to `127.0.0.1:53` and is configurable. It does not require AI Proxy or an external blocking-DNS provider.

## Install

Requires modern LuCI, firewall4/nftables, procd/rpcd, `ip`, `conntrack`, and `timeout`. The router must see the device's actual MAC, typically through a bridged AP. Custom guest zones and proxy configurations need separate validation.

Use the native packages in Releases. APK systems report their exact package architecture with `apk --print-arch`; opkg systems use `opkg print-architecture`. Download the matching backend package and the architecture-independent LuCI package, verify SHA256SUMS, then install both.

For PonWrt on `aarch64_cortex-a53`:

```sh
apk add --allow-untrusted ./nintendo-guard-0.1.1-r1.aarch64_cortex-a53.apk ./luci-app-nintendo-guard-0.1.1-r1.noarch.apk
```

For opkg-based firewall4 systems:

```sh
opkg install ./nintendo-guard_0.1.1-1_aarch64_cortex-a53.ipk ./luci-app-nintendo-guard_0.1.1-1_all.ipk
```

These are third-party packages, not signed with OpenWrt's official key; `--allow-untrusted` applies to this local-file installation. Package managers track files/dependencies and support upgrades/removal. Existing JSON settings are retained; fresh installations default to OFF. Re-login and open **Network → Nintendo Control**. The UI is currently Chinese.

The `.tar.gz` bundles remain an optional manual installation method: extract the matching CPU bundle and run `sh install.sh`. They are not APK/IPK files and cannot be uploaded directly to a package installation page. Do not use their uninstall script for a package-managed installation.

ARM64/PonWrt is the original hardware test platform. Other architectures still need hardware testing.

## Limits

- Known domains may change. The selective mode cannot inspect every request inside encrypted/shared connections.
- Blocking `aqua` can affect game/DLC downloads and store functions. Allowing `atum` does not guarantee every download works.
- Active filtering blocks forwarded IPv6, UDP 443, TCP/UDP 853, unreadable HTTP/TLS hostnames, ECH and TLS without SNI.
- Existing locally stored firmware, updates from offline media if present, other networks and downstream tunnels are outside router control.
- Full mode isolates routed traffic, not same-L2 traffic; bridge-netfilter and vendor-specific offload need validation.
- One target MAC is supported. Switching modes reconnects affected flows. If the service crashes, rules remain to preserve blocking.
- Boot persistence does not guarantee survival across a router firmware flash.

Disable from LuCI. Native installations should be removed with `apk del luci-app-nintendo-guard nintendo-guard` or `opkg remove luci-app-nintendo-guard nintendo-guard`. Only manual tar installations use `sh uninstall.sh` from the extracted bundle. Uninstallation removes only this feature's files/table/include and retains a configuration backup. It does not restore an old complete firewall configuration.

## Develop

Go 1.24.4+ and Python 3:

```sh
go test ./...
go vet ./...
python3 scripts/build.py --version v0.1.1
```

See the Chinese README for namespace integration tests, endpoint references and details. Configuration is `/etc/nintendo-control.json`; management uses a root-only Unix socket and LuCI/rpcd ACL. No telemetry or cloud account is used.

MIT License. Not affiliated with or endorsed by Nintendo.
