#!/usr/bin/env bash
# Package luci-app-meshd as an architecture-independent .ipk: the rpcd exec
# plugin, its ACL, the LuCI menu entry and the view. Mirrors package-ipk.sh
# (a gzip tar of debian-binary/control.tar.gz/data.tar.gz, what opkg expects).
set -euo pipefail
cd "$(dirname "$0")/.."

pkgname=luci-app-meshd
version="${VERSION:-0.1.0}"
arch=all
output=build/luci-ipk
pkgdir="$output/pkg"
controldir="$output/control"
src=package/luci-app-meshd

rm -rf "$output"
mkdir -p "$controldir" \
	"$pkgdir/usr/libexec/rpcd" \
	"$pkgdir/usr/share/rpcd/acl.d" \
	"$pkgdir/usr/share/luci/menu.d" \
	"$pkgdir/www/luci-static/resources/view/meshd"

# htdocs/ maps to /www/ on the device (the LuCI convention).
install -m 0755 "$src/root/usr/libexec/rpcd/meshd" "$pkgdir/usr/libexec/rpcd/meshd"
install -m 0644 "$src/root/usr/share/rpcd/acl.d/luci-app-meshd.json" "$pkgdir/usr/share/rpcd/acl.d/luci-app-meshd.json"
install -m 0644 "$src/root/usr/share/luci/menu.d/luci-app-meshd.json" "$pkgdir/usr/share/luci/menu.d/luci-app-meshd.json"
install -m 0644 "$src/htdocs/luci-static/resources/view/meshd/meshd.js" "$pkgdir/www/luci-static/resources/view/meshd/meshd.js"

# Bundle the built PWA so LuCI serves it locally (the view iframes it). The
# PWA is built with relative asset paths, so it works from this subpath. The
# release builds web/dist first; locally it uses whatever is in web/dist.
pwadir="$pkgdir/www/luci-static/resources/view/meshd/pwa"
if [ -f web/dist/index.html ]; then
	mkdir -p "$pwadir"
	tar -C web/dist --exclude=.gitkeep -cf - . | tar -C "$pwadir" -xf -
else
	echo "WARNING: web/dist/index.html missing; LuCI app will ship without the PWA (run web build first)" >&2
fi

cat > "$controldir/control" <<EOF
Package: $pkgname
Version: $version
Architecture: $arch
Maintainer: and-elf <noreply@example.com>
Depends: meshd, curl
Description: LuCI support for OpenWrt Mesh Manager (meshd)
EOF

# Re-register the rpcd object/ACL and drop the LuCI menu cache on install.
cat > "$controldir/postinst" <<'EOF'
#!/bin/sh
[ -n "${IPKG_INSTROOT}" ] && exit 0
rm -f /tmp/luci-indexcache* 2>/dev/null
rm -rf /tmp/luci-modulecache 2>/dev/null
/etc/init.d/rpcd reload 2>/dev/null || true
exit 0
EOF
chmod 0755 "$controldir/postinst"

mkdir -p "$output/ipk"
tar -C "$pkgdir" -czf "$output/data.tar.gz" .
tar -C "$controldir" -czf "$output/control.tar.gz" .
echo -n "2.0" > "$output/debian-binary"

cd "$output"
tar -czf "${pkgname}_${version}_${arch}.ipk" ./debian-binary ./control.tar.gz ./data.tar.gz

echo "Created $output/${pkgname}_${version}_${arch}.ipk"
