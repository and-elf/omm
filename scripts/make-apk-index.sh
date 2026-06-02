#!/usr/bin/env bash
# Build a signed apk repository index (packages.adb) over a directory of .apk
# files, so a release doubles as an apk feed:
#
#   /etc/apk/repositories.d/customfeeds.list:
#     https://github.com/and-elf/omm/releases/download/<tag>
#   apk update && apk add meshd
#
# This is the apk counterpart of make-feed-index.sh (the opkg Packages index).
# Signing is opt-in and mirrors package-apk.sh.
#
# Usage: scripts/make-apk-index.sh <dir-of-apks>
# Env:   APK, APK_SIGN_KEY, APK_SIGN_KEY_FILE  (as in package-apk.sh)
set -euo pipefail
cd "$(dirname "$0")/.."

dir="${1:?usage: make-apk-index.sh <dir-of-apks>}"
APK="${APK:-$(scripts/get-apk-tools.sh)}"

keyfile=""
cleanup() {
	[ -n "$keyfile" ] && [ -z "${APK_SIGN_KEY_FILE:-}" ] && rm -f "$keyfile"
	return 0
}
trap cleanup EXIT

if [ -n "${APK_SIGN_KEY_FILE:-}" ]; then
	keyfile="$APK_SIGN_KEY_FILE"
elif [ -n "${APK_SIGN_KEY:-}" ]; then
	keyfile="$(mktemp)"
	( umask 077; printf '%s\n' "$APK_SIGN_KEY" > "$keyfile" )
fi

# Absolute key path: apk runs with the index dir as its working directory.
[ -n "$keyfile" ] && keyfile="$(cd "$(dirname "$keyfile")" && pwd)/$(basename "$keyfile")"

shopt -s nullglob
apks=("$dir"/*.apk)
if [ ${#apks[@]} -eq 0 ]; then
	echo "no .apk files in $dir" >&2
	exit 1
fi

sign_args=()
if [ -n "$keyfile" ]; then
	sign_args=(--sign "$keyfile")
else
	echo "WARNING: no APK_SIGN_KEY / APK_SIGN_KEY_FILE set; building an UNSIGNED index" >&2
fi

# --allow-untrusted: indexing must not require the packages' own signatures to
# be trusted by this build host (they're verified on-device). mkndx --sign then
# signs the index itself.
( cd "$dir" && "$APK" mkndx --allow-untrusted "${sign_args[@]}" --output packages.adb ./*.apk )

echo "wrote $dir/packages.adb${keyfile:+ (signed)} (${#apks[@]} package(s))"
