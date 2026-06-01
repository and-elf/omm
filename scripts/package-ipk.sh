#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

pkgname=meshd
# Overridable by the release workflow; defaults preserve local/CI behaviour.
version="${VERSION:-0.1.0}"
arch="${ARCH:-all}"
output=build/ipk
pkgdir="$output/pkg"
controldir="$output/control"

rm -rf "$output"
mkdir -p "$pkgdir/usr/bin" "$pkgdir/etc/init.d" "$pkgdir/etc/config" "$pkgdir/etc/meshd" "$controldir"
install -m 0755 bin/meshd "$pkgdir/usr/bin/meshd"
install -m 0755 package/meshd/files/meshd.init "$pkgdir/etc/init.d/meshd"
install -m 0644 package/meshd/files/meshd.config "$pkgdir/etc/config/meshd"

cat > "$controldir/control" <<EOF
Package: $pkgname
Version: $version
Architecture: $arch
Maintainer: and-elf <noreply@example.com>
Description: OpenWrt Mesh Manager daemon
EOF

# Preserve operator edits to the UCI config across upgrades.
echo "/etc/config/meshd" > "$controldir/conffiles"

# Enable and start the service on install (when procd is present).
cat > "$controldir/postinst" <<'EOF'
#!/bin/sh
[ -n "${IPKG_INSTROOT}" ] && exit 0
/etc/init.d/meshd enable 2>/dev/null || true
exit 0
EOF
chmod 0755 "$controldir/postinst"

mkdir -p "$output/ipk"

tar -C "$pkgdir" -czf "$output/data.tar.gz" .

tar -C "$controldir" -czf "$output/control.tar.gz" .

echo -n "2.0" > "$output/debian-binary"

cd "$output"
# OpenWrt ipk packages are a gzip-compressed tar of these three members (not an
# ar archive like Debian .deb), which is what opkg expects.
tar -czf "${pkgname}_${version}_${arch}.ipk" ./debian-binary ./control.tar.gz ./data.tar.gz

echo "Created $output/${pkgname}_${version}_${arch}.ipk"
