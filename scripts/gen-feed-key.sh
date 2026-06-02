#!/usr/bin/env bash
# Generate the usign keypair that signs the opkg feed.
#
# Writes two files in the current directory:
#   omm-feed.sec  — SECRET key. Never commit it. Store it as the OPKG_SIGN_KEY
#                   GitHub Actions secret (see below) and keep a backup offline.
#   omm-feed.pub  — PUBLIC key. Safe to commit (the release workflow publishes
#                   it so devices can `opkg-key add` it).
#
# Uses the local `usign` if present, otherwise generates inside an OpenWrt
# container (podman, or CONTAINER=docker). Run this ONCE; rotating the key means
# re-trusting it on every device.
set -euo pipefail

cn="${KEY_COMMENT:-OMM package feed}"

if [ -e omm-feed.sec ] || [ -e omm-feed.pub ]; then
	echo "refusing to overwrite existing omm-feed.sec/omm-feed.pub" >&2
	exit 1
fi

if command -v usign >/dev/null 2>&1; then
	usign -G -s omm-feed.sec -p omm-feed.pub -c "$cn"
else
	echo "usign not found locally; generating inside an OpenWrt container…" >&2
	CONTAINER="${CONTAINER:-podman}"
	IMAGE="${IMAGE:-docker.io/openwrt/rootfs:x86-64-23.05.5}"
	"$CONTAINER" run --rm -v "$PWD:/out" "$IMAGE" /bin/sh -c '
		mkdir -p /var/lock && opkg update >/dev/null 2>&1 && opkg install usign >/dev/null 2>&1 || { echo "FAIL: cannot install usign"; exit 1; }
		cd /out && usign -G -s omm-feed.sec -p omm-feed.pub -c "'"$cn"'"
	'
fi

chmod 600 omm-feed.sec
echo
echo "Generated:"
echo "  omm-feed.pub  (public — commit to package/omm-feed.pub)"
echo "  omm-feed.sec  (secret — DO NOT commit)"
echo
echo "Next steps:"
echo "  1. gh secret set OPKG_SIGN_KEY < omm-feed.sec     # store the secret key"
echo "  2. mkdir -p package && cp omm-feed.pub package/omm-feed.pub && git add package/omm-feed.pub && git commit"
echo "  3. back up omm-feed.sec offline, then: rm omm-feed.sec"
