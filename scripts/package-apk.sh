#!/usr/bin/env bash
# Build a real, signed apk-v3 package for meshd, the apk counterpart of
# package-ipk.sh. Unlike the previous version (a plain gzip tar extracted onto
# the rootfs), this produces a genuine apk package installable with `apk add`
# and verifiable against a trusted key in /etc/apk/keys/.
#
# Signing is opt-in, mirroring the opkg feed: a package is signed when a key is
# provided, otherwise it is built unsigned (apk will treat it as UNTRUSTED).
#
# Env:
#   VERSION            package version            (default 0.1.0)
#   ARCH               apk arch; "all" -> noarch  (default x86_64)
#   OUTPUT_DIR         where the .apk is written  (default build)
#   APK                apk binary                 (default: scripts/get-apk-tools.sh)
#   APK_SIGN_KEY       EC private key PEM *contents* (e.g. a CI secret)
#   APK_SIGN_KEY_FILE  path to an EC private key PEM (takes precedence)
set -euo pipefail
cd "$(dirname "$0")/.."

pkgname=meshd
version="${VERSION:-0.1.0}"
arch="${ARCH:-x86_64}"
output_dir="${OUTPUT_DIR:-build}"
APK="${APK:-$(scripts/get-apk-tools.sh)}"

# apk's noarch token for architecture-independent packages (matches OpenWrt's
# mkpkg rule). The output filename still carries the requested arch token so a
# multi-arch release can attach every variant without collisions.
if [ "$arch" = "all" ]; then
	apk_arch=noarch
else
	apk_arch="$arch"
fi

stage="$(mktemp -d)"
keyfile=""
cleanup() {
	rm -rf "$stage"
	# Only remove a key file we created ourselves from APK_SIGN_KEY contents.
	[ -n "$keyfile" ] && [ -z "${APK_SIGN_KEY_FILE:-}" ] && rm -f "$keyfile"
	return 0
}
trap cleanup EXIT

root="$stage/root"
mkdir -p "$root/usr/bin" "$root/etc/init.d" "$root/etc/config" "$root/etc/meshd"
install -m 0755 bin/meshd "$root/usr/bin/meshd"
install -m 0755 package/meshd/files/meshd.init "$root/etc/init.d/meshd"
install -m 0644 package/meshd/files/meshd.config "$root/etc/config/meshd"

# Enable (but don't start) the service on install, like the .ipk postinst.
postinst="$stage/post-install"
cat > "$postinst" <<'EOF'
#!/bin/sh
[ -n "${IPKG_INSTROOT}" ] && exit 0
/etc/init.d/meshd enable 2>/dev/null || true
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
out="$output_dir/${pkgname}-${version}-${arch}.apk"

sign_args=()
if [ -n "$keyfile" ]; then
	sign_args=(--sign "$keyfile")
else
	echo "WARNING: no APK_SIGN_KEY / APK_SIGN_KEY_FILE set; building an UNSIGNED apk" >&2
fi

"$APK" mkpkg \
	--info "name:$pkgname" \
	--info "version:$version" \
	--info "description:OpenWrt Mesh Manager daemon" \
	--info "arch:$apk_arch" \
	--info "license:MIT" \
	--info "url:https://github.com/and-elf/omm" \
	--info "maintainer:and-elf <noreply@example.com>" \
	--script "post-install:$postinst" \
	--files "$root" \
	"${sign_args[@]}" \
	--output "$out"

echo "Created $out${keyfile:+ (signed)}"
