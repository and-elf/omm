#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

pkgname=meshd
version=0.1.0
arch=x86_64
output=build/apk
rootdir="$output/root"

mkdir -p "$rootdir/usr/bin"
install -m 0755 bin/meshd "$rootdir/usr/bin/meshd"

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
