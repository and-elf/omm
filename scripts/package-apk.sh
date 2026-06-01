#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

pkgname=meshd
version=0.1.0
arch=x86_64
output=build/apk
rootdir="$output/root"

mkdir -p "$rootdir/usr/bin" "$rootdir/etc/init.d" "$rootdir/etc/config" "$rootdir/etc/meshd"
install -m 0755 bin/meshd "$rootdir/usr/bin/meshd"
install -m 0755 package/meshd.init "$rootdir/etc/init.d/meshd"
install -m 0644 package/meshd.config "$rootdir/etc/config/meshd"

cat > "$rootdir/.PKGINFO" <<EOF
pkgname: $pkgname
pkgver: $version
pkgdesc: OpenWrt Mesh Manager daemon
url: https://github.com/and-elf/omm
arch: $arch
maintainer: and-elf <noreply@example.com>
EOF

mkdir -p build

tar -C "$rootdir" -czf "build/${pkgname}-${version}.apk" .

echo "Created build/${pkgname}-${version}.apk"
