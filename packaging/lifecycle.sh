#!/bin/sh
# Shared package lifecycle for opkg and apk. Configuration is intentionally
# generated on-device rather than shipped as an overwriteable payload file.
set -eu
[ -z "${IPKG_INSTROOT:-}" ] || exit 0
case "${1:-}" in
 configure)
  if [ ! -e /etc/nintendo-control.json ]; then
   umask 077
   cp /usr/share/nintendo-guard/default.json /etc/nintendo-control.json
  fi
  /usr/sbin/nintendo-guard validate < /etc/nintendo-control.json >/dev/null
  uci set firewall.nintendo_control=include
  uci set firewall.nintendo_control.type=script
  uci set firewall.nintendo_control.path=/usr/libexec/nintendo-control-firewall
  uci set firewall.nintendo_control.fw4_compatible=1
  uci set firewall.nintendo_control.enabled=1
  uci commit firewall
  /etc/init.d/nintendo-control enable
  if /etc/init.d/nintendo-control running; then
   /etc/init.d/nintendo-control restart
  else
   /etc/init.d/nintendo-control start
  fi
  ;;
 remove)
  /etc/init.d/nintendo-control disable
  /etc/init.d/nintendo-control stop
  /usr/sbin/nintendo-guard cleanup
  uci -q delete firewall.nintendo_control || true
  uci commit firewall
  # Keep user configuration for a later reinstall, as OpenWrt packages do.
  ;;
 ui)
  for path in /tmp/luci-indexcache.*.json; do [ ! -f "$path" ] || rm "$path"; done
  [ ! -x /etc/init.d/rpcd ] || /etc/init.d/rpcd restart
  ;;
 *) echo 'usage: nintendo-control-package configure|remove|ui' >&2; exit 1 ;;
esac
