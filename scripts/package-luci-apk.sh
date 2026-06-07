#!/usr/bin/env bash
# Build a real, signed apk-v3 package for luci-app-meshd, the apk counterpart
# of package-luci-ipk.sh. Same file layout as the .ipk (rpcd exec plugin, ACL,
# LuCI menu entry, view, uci-defaults and the bundled PWA), produced as a
# genuine apk package installable with `apk add` and verifiable against a
# trusted key in /etc/apk/keys/.
#
# luci-app-meshd is architecture-independent, so it always builds as noarch;
# the depends on meshd is carried via apk's "depends" info field. The rpcd
# plugin uses busybox nc (always present) rather than curl, so there is no
# extra runtime dependency and the package installs offline.
#
# Signing is opt-in, mirroring package-apk.sh: a package is signed when a key
# is provided, otherwise it is built unsigned (apk treats it as UNTRUSTED).
#
# Env:
#   VERSION            package version            (default 0.1.0)
#   OUTPUT_DIR         where the .apk is written  (default build/luci-ipk)
#   APK                apk binary                 (default: scripts/get-apk-tools.sh)
#   APK_SIGN_KEY       EC private key PEM *contents* (e.g. a CI secret)
#   APK_SIGN_KEY_FILE  path to an EC private key PEM (takes precedence)
set -euo pipefail
cd "$(dirname "$0")/.."

pkgname=luci-app-meshd
version="${VERSION:-0.1.0}"
output_dir="${OUTPUT_DIR:-build/luci-ipk}"
APK="${APK:-$(scripts/get-apk-tools.sh)}"
src=package/luci-app-meshd

stage="$(mktemp -d)"
keyfile=""
cleanup() {
	rm -rf "$stage"
	# Only remove a key file we created ourselves from APK_SIGN_KEY contents.
	[ -n "$keyfile" ] && [ -z "${APK_SIGN_KEY_FILE:-}" ] && rm -f "$keyfile"
	return 0
}
trap cleanup EXIT

# Lay out the rootfs exactly as the .ipk does. htdocs/ maps to /www/ on the
# device (the LuCI convention).
root="$stage/root"
mkdir -p \
	"$root/usr/libexec/rpcd" \
	"$root/usr/share/rpcd/acl.d" \
	"$root/usr/share/luci/menu.d" \
	"$root/etc/uci-defaults" \
	"$root/www/luci-static/resources/view/meshd"

install -m 0755 "$src/root/usr/libexec/rpcd/meshd" "$root/usr/libexec/rpcd/meshd"
install -m 0644 "$src/root/usr/share/rpcd/acl.d/luci-app-meshd.json" "$root/usr/share/rpcd/acl.d/luci-app-meshd.json"
install -m 0644 "$src/root/usr/share/luci/menu.d/luci-app-meshd.json" "$root/usr/share/luci/menu.d/luci-app-meshd.json"
install -m 0755 "$src/root/etc/uci-defaults/99-luci-app-meshd" "$root/etc/uci-defaults/99-luci-app-meshd"
install -m 0644 "$src/htdocs/luci-static/resources/view/meshd/meshd.js" "$root/www/luci-static/resources/view/meshd/meshd.js"

# Bundle the built PWA so LuCI serves it locally (the view iframes it). The
# PWA is built with relative asset paths, so it works from this subpath. The
# release builds web/dist first; locally it uses whatever is in web/dist.
pwadir="$root/www/luci-static/resources/view/meshd/pwa"
if [ -f web/dist/index.html ]; then
	mkdir -p "$pwadir"
	tar -C web/dist --exclude=.gitkeep -cf - . | tar -C "$pwadir" -xf -
else
	echo "WARNING: web/dist/index.html missing; LuCI app will ship without the PWA (run web build first)" >&2
fi

# Apply the uci-defaults (secure posture) immediately on a live install,
# re-register the rpcd object/ACL, and drop the LuCI menu cache. Mirrors the
# .ipk postinst.
postinst="$stage/post-install"
cat > "$postinst" <<'EOF'
#!/bin/sh
[ -n "${IPKG_INSTROOT}" ] && exit 0
if [ -f /etc/uci-defaults/99-luci-app-meshd ]; then
	( . /etc/uci-defaults/99-luci-app-meshd ) && rm -f /etc/uci-defaults/99-luci-app-meshd
fi
rm -f /tmp/luci-indexcache* 2>/dev/null
rm -rf /tmp/luci-modulecache 2>/dev/null
/etc/init.d/rpcd reload 2>/dev/null || true
exit 0
EOF
chmod 0755 "$postinst"

# Resolve the signing key: an explicit file wins; otherwise materialise the
# PEM contents from APK_SIGN_KEY into a private temp file.
if [ -n "${APK_SIGN_KEY_FILE:-}" ]; then
	keyfile="$APK_SIGN_KEY_FILE"
elif [ -n "${APK_SIGN_KEY:-}" ]; then
	keyfile="$stage/sign.key"
	( umask 077; printf '%s\n' "$APK_SIGN_KEY" > "$keyfile" )
fi

mkdir -p "$output_dir"
out="$output_dir/${pkgname}-${version}-all.apk"

sign_args=()
if [ -n "$keyfile" ]; then
	sign_args=(--sign "$keyfile")
else
	echo "WARNING: no APK_SIGN_KEY / APK_SIGN_KEY_FILE set; building an UNSIGNED apk" >&2
fi

"$APK" mkpkg \
	--info "name:$pkgname" \
	--info "version:$version" \
	--info "description:LuCI support for OpenWrt Mesh Manager (meshd)" \
	--info "arch:noarch" \
	--info "license:MIT" \
	--info "url:https://github.com/and-elf/omm" \
	--info "maintainer:and-elf <noreply@example.com>" \
	--info "depends:meshd" \
	--script "post-install:$postinst" \
	--files "$root" \
	"${sign_args[@]}" \
	--output "$out"

echo "Created $out${keyfile:+ (signed)}"
