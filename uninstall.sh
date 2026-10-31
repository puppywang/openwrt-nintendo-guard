#!/bin/sh
set -eu
[ "$(id -u)" = 0 ] || { echo 'Run as root.' >&2; exit 1; }
if { command -v apk >/dev/null && apk info -e nintendo-guard >/dev/null 2>&1; } ||
   { command -v opkg >/dev/null && opkg list-installed nintendo-guard | grep -q '^nintendo-guard '; }; then
 echo 'Use apk del or opkg remove for a package-managed installation.' >&2; exit 1
fi
backup="/root/nintendo-control-uninstall-$(date +%Y%m%d-%H%M%S)-$$.json"
cp -p /etc/nintendo-control.json "$backup"
/etc/init.d/nintendo-control disable
/etc/init.d/nintendo-control stop
/usr/sbin/nintendo-guard cleanup
uci -q delete firewall.nintendo_control || true
uci commit firewall
for path in /usr/sbin/nintendo-guard /etc/nintendo-control.json /etc/init.d/nintendo-control /usr/libexec/rpcd/nintendo-control /usr/libexec/nintendo-control-firewall /usr/share/luci/menu.d/luci-app-nintendo-control.json /usr/share/rpcd/acl.d/luci-app-nintendo-control.json /www/luci-static/resources/view/network/nintendo.js; do
 [ ! -e "$path" ] || rm "$path"
done
/etc/init.d/rpcd restart
for path in /tmp/luci-indexcache.*.json; do [ ! -f "$path" ] || rm "$path"; done
echo "Removed Nintendo Guard. Configuration backup: $backup"
