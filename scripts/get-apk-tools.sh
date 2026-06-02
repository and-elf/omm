#!/usr/bin/env bash
# Build (and cache) the apk-tools 3 `apk` binary used to create and sign apk
# packages, then print its path on stdout. All diagnostics go to stderr, so
# callers can do:  APK="$(scripts/get-apk-tools.sh)"
#
# apk-tools 3 is the package manager OpenWrt switched to (snapshot/25.x). It is
# a build-host tool — it is NOT shipped in the device rootfs — so we build it
# from upstream Alpine sources, pinned to the exact commit OpenWrt ships, the
# same way the release workflow builds usign from source for opkg feed signing.
#
# Build host: Linux. OpenWrt package building is inherently a Linux activity and
# the resulting `apk` binary is a native Linux executable. On a Linux host with
# meson installed we build natively; otherwise we build inside a Debian
# container (podman, or CONTAINER=docker). On non-Linux hosts, run this on a
# Linux box or in CI.
#
# Overrides:
#   APK            pre-built apk binary to use as-is (skips building)
#   APK_TOOLS_REF  git commit/tag to build (default: OpenWrt's pinned 3.0.5)
#   CACHE_DIR      where the built binary is cached (default: build/apk-tools)
#   CONTAINER      container engine for the fallback build (default: podman)
set -euo pipefail
cd "$(dirname "$0")/.."

# Pinned to apk-tools 3.0.5 — the exact commit OpenWrt ships
# (package/system/apk/Makefile: PKG_VERSION=3.0.5, PKG_SOURCE_VERSION).
APK_TOOLS_REF="${APK_TOOLS_REF:-b5a31c0d865342ad80be10d68f1bb3d3ad9b0866}"
APK_TOOLS_URL="https://gitlab.alpinelinux.org/alpine/apk-tools.git"
CACHE_DIR="${CACHE_DIR:-build/apk-tools}"
apk_bin="$CACHE_DIR/apk"

log() { echo "$@" >&2; }
# Print an absolute path so callers can `cd` elsewhere and still run apk.
abspath() { case "$1" in /*) echo "$1";; *) echo "$PWD/$1";; esac; }

# Help generation needs a Lua interpreter; we disable it (we drive apk by flags,
# not its built-in --help), which also drops the only awkward build dependency.
meson_args=(
	-Db_lto=true
	-Ddocs=disabled
	-Dhelp=disabled
	-Ddefault_library=static
	-Durl_backend=wget
	-Dzstd=disabled
	-Dpython=disabled
	-Dtests=disabled
	-Dcrypto_backend=openssl
)

# An explicitly supplied binary wins.
if [ -n "${APK:-}" ] && "${APK}" --version >/dev/null 2>&1; then
	abspath "$APK"
	exit 0
fi

# A previously cached build wins (cheap to re-verify it still runs).
if [ -x "$apk_bin" ] && "$apk_bin" --version >/dev/null 2>&1; then
	log "using cached apk: $apk_bin"
	abspath "$apk_bin"
	exit 0
fi

mkdir -p "$CACHE_DIR"

build_native() {
	command -v meson >/dev/null 2>&1 || return 1
	log "building apk-tools $APK_TOOLS_REF natively"
	local src="$CACHE_DIR/src"
	if [ ! -d "$src/.git" ]; then
		git clone "$APK_TOOLS_URL" "$src" >&2
	fi
	git -C "$src" fetch --depth 1 origin "$APK_TOOLS_REF" >&2
	git -C "$src" checkout --quiet "$APK_TOOLS_REF"
	rm -rf "$src/build"
	( cd "$src" && VERSION=3.0.5 meson setup build "${meson_args[@]}" >&2 && ninja -C build >&2 )
	cp "$src/build/src/apk" "$apk_bin"
}

build_in_container() {
	local engine="${CONTAINER:-podman}"
	command -v "$engine" >/dev/null 2>&1 || return 1
	log "meson not found; building apk-tools $APK_TOOLS_REF inside a container ($engine)"
	# Build in the cache dir (mounted) so the binary lands on the host. The
	# binary is dynamically linked, so it runs on a host with a glibc at least
	# as new as the build image (Debian trixie); that covers Linux build hosts.
	# $VARs below are deliberately single-quoted to expand inside the container,
	# not on the host; APK_TOOLS_URL / meson_args are spliced in explicitly.
	# shellcheck disable=SC2016
	"$engine" run --rm -v "$PWD/$CACHE_DIR:/cache" \
		-e APK_TOOLS_REF="$APK_TOOLS_REF" \
		docker.io/library/debian:trixie-slim /bin/sh -euc '
			export DEBIAN_FRONTEND=noninteractive
			apt-get update -qq
			apt-get install -y -qq git meson ninja-build pkg-config gcc \
				libssl-dev zlib1g-dev >/dev/null
			src=/cache/src
			[ -d "$src/.git" ] || git clone '"$APK_TOOLS_URL"' "$src"
			git -C "$src" fetch --depth 1 origin "$APK_TOOLS_REF"
			git -C "$src" checkout --quiet "$APK_TOOLS_REF"
			rm -rf "$src/build"
			cd "$src"
			VERSION=3.0.5 meson setup build '"${meson_args[*]}"'
			ninja -C build
			cp "$src/build/src/apk" /cache/apk
		' >&2
}

if [ "$(uname -s)" != "Linux" ]; then
	log "ERROR: apk-tools must be built on Linux (apk is a Linux build-host tool)."
	log "       Run this on a Linux host or in CI, or set \$APK to a prebuilt binary."
	exit 1
fi

if ! build_native && ! build_in_container; then
	log "ERROR: cannot build apk-tools — need either 'meson' (with gcc, libssl-dev,"
	log "       zlib1g-dev) or a container engine (podman/docker) on PATH."
	exit 1
fi

"$apk_bin" --version >/dev/null 2>&1 || { log "ERROR: built apk binary does not run"; exit 1; }
log "built apk: $apk_bin ($("$apk_bin" --version 2>&1))"
abspath "$apk_bin"
