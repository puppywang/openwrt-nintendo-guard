#!/bin/sh
set -eu
cd "$(dirname "$0")"
[ "$(id -u)" = 0 ] || { echo 'Run as root on the router.' >&2; exit 1; }
for cmd in nft fw4 uci ubus conntrack timeout ip; do
 command -v "$cmd" >/dev/null || { echo "Missing dependency: $cmd" >&2; exit 1; }
done
[ -f /www/luci-static/resources/luci.js ] || { echo 'Modern LuCI is required.' >&2; exit 1; }
for file in nintendo-guard luci/nintendo.js luci/menu.json luci/acl.json luci/nintendo-control.init luci/nintendo-control.rpc luci/nintendo-control-firewall; do
 [ -s "$file" ] || { echo "Incomplete release bundle: $file" >&2; exit 1; }
done
./nintendo-guard version
umask 077
work=$(mktemp -d /tmp/nintendo-guard-install.XXXXXX)
cleanup() { rm -f "$work/config.json" "$work/rules.nft"; rmdir "$work"; }
trap cleanup EXIT
trap 'exit 1' INT TERM
if [ -e /etc/nintendo-control.json ]; then
 [ ! -L /etc/nintendo-control.json ] || { echo 'Refusing symlink configuration.' >&2; exit 1; }
 ./nintendo-guard validate < /etc/nintendo-control.json > "$work/config.json"
else
 printf '%s\n' '{"mode":"off","mac":"","upstream":"127.0.0.1:53"}' > "$work/config.json"
fi
NINTENDO_GUARD_CONFIG="$work/config.json" ./nintendo-guard rules > "$work/rules.nft"
nft -c -f "$work/rules.nft"
backup="/root/nintendo-control-backup-$(date +%Y%m%d-%H%M%S)-$$"
mkdir -m 700 "$backup"
cp -p /etc/config/firewall "$backup/firewall"
for path in /usr/sbin/nintendo-guard /etc/nintendo-control.json /etc/init.d/nintendo-control /usr/libexec/rpcd/nintendo-control /usr/libexec/nintendo-control-firewall /usr/share/luci/menu.d/luci-app-nintendo-control.json /usr/share/rpcd/acl.d/luci-app-nintendo-control.json /www/luci-static/resources/view/network/nintendo.js; do
 [ ! -L "$path" ] || { echo "Refusing to replace symlink: $path" >&2; exit 1; }
 if [ -e "$path" ]; then
  mkdir -p "$backup$(dirname "$path")"
  cp -p "$path" "$backup$path"
 fi
done
cp nintendo-guard /usr/sbin/nintendo-guard.new
chmod 755 /usr/sbin/nintendo-guard.new
mv /usr/sbin/nintendo-guard.new /usr/sbin/nintendo-guard
cp "$work/config.json" /etc/nintendo-control.json.new
chmod 600 /etc/nintendo-control.json.new
mv /etc/nintendo-control.json.new /etc/nintendo-control.json
cp luci/nintendo-control.init /etc/init.d/nintendo-control
cp luci/nintendo-control.rpc /usr/libexec/rpcd/nintendo-control
cp luci/nintendo-control-firewall /usr/libexec/nintendo-control-firewall
chmod 755 /etc/init.d/nintendo-control /usr/libexec/rpcd/nintendo-control /usr/libexec/nintendo-control-firewall
cp luci/menu.json /usr/share/luci/menu.d/luci-app-nintendo-control.json
cp luci/acl.json /usr/share/rpcd/acl.d/luci-app-nintendo-control.json
cp luci/nintendo.js /www/luci-static/resources/view/network/nintendo.js
chmod 644 /usr/share/luci/menu.d/luci-app-nintendo-control.json /usr/share/rpcd/acl.d/luci-app-nintendo-control.json /www/luci-static/resources/view/network/nintendo.js
uci set firewall.nintendo_control=include
uci set firewall.nintendo_control.type=script
uci set firewall.nintendo_control.path=/usr/libexec/nintendo-control-firewall
uci set firewall.nintendo_control.fw4_compatible=1
uci set firewall.nintendo_control.enabled=1
uci commit firewall
/etc/init.d/nintendo-control enable
if /etc/init.d/nintendo-control running; then /etc/init.d/nintendo-control restart; else /etc/init.d/nintendo-control start; fi
/etc/init.d/rpcd restart
for path in /tmp/luci-indexcache.*.json; do [ ! -f "$path" ] || rm "$path"; done
echo "Installed. Backup: $backup"
echo 'Open LuCI -> Network -> Nintendo Control. Fresh installations are OFF.'
