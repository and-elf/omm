#!/usr/bin/env bash
# Sign an opkg feed index with usign so opkg can verify it on `opkg update`.
#
# opkg verifies the signature over the *uncompressed* Packages file, so that is
# what we sign, producing Packages.sig alongside it. The matching public key
# must be present on the device under /etc/opkg/keys/ (see doc/openwrt.md).
#
#   scripts/sign-feed.sh <dir> <secret-key-file>
#
# Requires the `usign` tool (https://github.com/openwrt/usign); pass its path in
# $USIGN if it is not on PATH.
set -euo pipefail

dir="${1:?usage: sign-feed.sh <dir> <secret-key-file>}"
key="${2:?usage: sign-feed.sh <dir> <secret-key-file>}"
usign="${USIGN:-usign}"

if [ ! -f "$dir/Packages" ]; then
	echo "no $dir/Packages (run make-feed-index.sh first)" >&2
	exit 1
fi
if ! command -v "$usign" >/dev/null 2>&1; then
	echo "usign not found (set \$USIGN to its path)" >&2
	exit 1
fi

"$usign" -S -m "$dir/Packages" -s "$key" -x "$dir/Packages.sig"
echo "signed $dir/Packages -> $dir/Packages.sig"
