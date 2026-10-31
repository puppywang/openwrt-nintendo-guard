#!/bin/sh
set -eu
# Full upstream apk-tools; router installations normally use a minimal build
# without mkpkg. Keep this host tool outside released payloads.
commit=cad864700df5e54571a3be77f4011cfccabe8144
destination=build/apk-tools
if [ ! -d "$destination/.git" ]; then
 git clone --quiet --depth 1 --branch v3.0.5 https://github.com/alpinelinux/apk-tools.git "$destination"
fi
[ "$(git -C "$destination" rev-parse HEAD)" = "$commit" ] || { echo 'apk-tools revision mismatch' >&2; exit 1; }
meson setup "$destination/out" "$destination" -Ddocs=disabled -Dhelp=disabled -Dlua=disabled -Dpython=disabled -Dtests=disabled -Dzstd=enabled -Durl_backend=wget
ninja -C "$destination/out" src/apk
"$destination/out/src/apk" --version
