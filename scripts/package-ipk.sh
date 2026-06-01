#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

pkgname=meshd
version=0.1.0
arch=all
output=build/ipk
pkgdir="$output/pkg"
controldir="$output/control"

rm -rf "$output"
mkdir -p "$pkgdir/usr/bin" "$controldir"
install -m 0755 bin/meshd "$pkgdir/usr/bin/meshd"

cat > "$controldir/control" <<EOF
Package: $pkgname
Version: $version
Architecture: $arch
Maintainer: and-elf <noreply@example.com>
Description: OpenWrt Mesh Manager daemon
EOF

mkdir -p "$output/ipk"

tar -C "$pkgdir" -czf "$output/data.tar.gz" .

tar -C "$controldir" -czf "$output/control.tar.gz" .

echo -n "2.0" > "$output/debian-binary"

cd "$output"
ar r "${pkgname}_${version}_${arch}.ipk" debian-binary control.tar.gz data.tar.gz

echo "Created $output/${pkgname}_${version}_${arch}.ipk"
