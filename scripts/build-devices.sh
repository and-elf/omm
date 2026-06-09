#!/usr/bin/env bash
# Cross-compile meshd for the physical test devices, each to its own
# bin/<device>/meshd. build.sh itself is arch-agnostic — it builds for whatever
# GOARCH/GOARM is in the environment — so this wrapper just pins the right tuple
# per board and labels the output. Mirrors the release workflow's ISA groups
# (one static, CGO-free binary per Go arch, ABI-compatible across the listed
# OpenWrt subtargets) so a local build matches what CI ships.
#
#   device       GOARCH  GOARM  OpenWrt package arch
#   ----------   ------  -----  --------------------------
#   lyra-ac2200  arm     7      arm_cortex-a7_neon-vfpv4   (ipq40xx, Cortex-A7)
#   zb8103ax     arm64   -      aarch64_cortex-a53         (Cortex-A53)
#
# The frontend is embedded via go:embed and is arch-independent, so it is built
# once (first target) and reused (SKIP_FRONTEND=1) for the rest.
#
# Env:
#   PACKAGE=1   also build an installable .ipk + .apk per device, by handing the
#               matching OpenWrt arch to the (unchanged) package-{ipk,apk}.sh
#               primitives. Default off (binary-only).
#   VERSION     package version (default: scripts/derive-version.sh).
#
# Build a subset by passing device labels: ./scripts/build-devices.sh zb8103ax
set -euo pipefail
cd "$(dirname "$0")/.."

# device|goarch|goarm|openwrt_arch|elf_machine
# elf_machine is the ELF e_machine byte the finished binary must report, so a
# build with a stale/ambient GOARCH can't silently ship the wrong ISA — and a
# package can't be stamped with an arch its binary doesn't match
# (40 = ARM, 183 = AArch64).
DEVICES=(
	"lyra-ac2200|arm|7|arm_cortex-a7_neon-vfpv4|40"
	"zb8103ax|arm64||aarch64_cortex-a53|183"
)

# verify_elf PATH EXPECTED_MACHINE: read e_machine (offset 18, low byte) and
# fail loudly on a mismatch — the binary would crash-loop on-device otherwise.
verify_elf() {
	local path=$1 want=$2 got
	got="$(od -An -tu1 -j18 -N1 "$path" | tr -d ' ')"
	if [ "$got" != "$want" ]; then
		echo "FAIL: $path is ELF e_machine=$got, expected $want (wrong arch)" >&2
		exit 1
	fi
}

skip_frontend="${SKIP_FRONTEND:-0}"
package="${PACKAGE:-0}"
# Resolve the package version once so every device artifact carries the same one.
# derive-version.sh maps a ref to an apk/opkg-valid string; locally there is no
# $GITHUB_REF_NAME, so feed it the newest reachable tag (e.g. v0.3.0 -> 0.3.0).
# With no tags at all the tag is empty and derive-version falls back to its
# 0.0.0_git untagged form.
version="${VERSION:-$(./scripts/derive-version.sh "$(git describe --tags --abbrev=0 2>/dev/null)")}"
selected=("$@")

built=0
for spec in "${DEVICES[@]}"; do
	IFS='|' read -r device goarch goarm owrt_arch elf_machine <<<"$spec"

	# When labels are given, build only those.
	if [ "${#selected[@]}" -gt 0 ]; then
		match=0
		for want in "${selected[@]}"; do
			[ "$want" = "$device" ] && match=1
		done
		[ "$match" = 1 ] || continue
	fi

	echo "==> $device (GOARCH=$goarch GOARM=${goarm:-n/a}, OpenWrt arch $owrt_arch)"
	GOOS=linux GOARCH="$goarch" GOARM="$goarm" SKIP_FRONTEND="$skip_frontend" ./scripts/build.sh
	# Guard before packaging: the package scripts trust whatever is at bin/meshd,
	# so the ISA must be checked here, while bin/meshd is the binary we just built.
	verify_elf bin/meshd "$elf_machine"

	mkdir -p "bin/$device"
	if [ "$package" = 1 ]; then
		ARCH="$owrt_arch" VERSION="$version" ./scripts/package-ipk.sh
		ARCH="$owrt_arch" VERSION="$version" ./scripts/package-apk.sh
		mv "build/ipk/meshd_${version}_${owrt_arch}.ipk" "bin/$device/"
		mv "build/meshd-${version}-${owrt_arch}.apk" "bin/$device/"
	fi
	# Move the binary last: the package scripts above read it from bin/meshd.
	mv bin/meshd "bin/$device/meshd"
	echo "    -> bin/$device/meshd (verified arch ok)"
	[ "$package" = 1 ] && echo "    -> bin/$device/meshd_${version}_${owrt_arch}.ipk + .apk"

	# Frontend is now built and embedded; reuse it for the remaining targets.
	skip_frontend=1
	built=$((built + 1))
done

if [ "$built" = 0 ]; then
	echo "no matching device built; known: lyra-ac2200 zb8103ax" >&2
	exit 1
fi
