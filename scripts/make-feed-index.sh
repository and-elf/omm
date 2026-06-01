#!/usr/bin/env bash
# Generate an opkg feed index (Packages + Packages.gz) for a directory of .ipk
# files, so the release can be consumed as a feed:
#
#   src/gz omm https://github.com/and-elf/omm/releases/download/<tag>
#   opkg update && opkg install meshd
#
# Each entry is the package's own control metadata plus the Filename/Size/
# SHA256sum opkg needs to fetch and verify the archive. Usage:
#
#   scripts/make-feed-index.sh <dir-of-ipks>
set -euo pipefail

dir="${1:?usage: make-feed-index.sh <dir-of-ipks>}"
cd "$dir"

packages="Packages"
: >"$packages"

shopt -s nullglob
ipks=(*.ipk)
if [ ${#ipks[@]} -eq 0 ]; then
	echo "no .ipk files in $dir" >&2
	exit 1
fi

for ipk in "${ipks[@]}"; do
	# An .ipk is a gzip tar of debian-binary/control.tar.gz/data.tar.gz; the
	# control file inside control.tar.gz carries Package/Version/Architecture/…
	control="$(tar -xzOf "$ipk" ./control.tar.gz | tar -xzO ./control)"
	size="$(wc -c <"$ipk")"
	sha="$(sha256sum "$ipk" | cut -d' ' -f1)"

	{
		# Trim trailing blank lines from control, then append feed fields.
		printf '%s\n' "$control" | sed -e 's/[[:space:]]*$//' -e '/^$/d'
		printf 'Filename: %s\n' "$ipk"
		printf 'Size: %s\n' "$size"
		printf 'SHA256sum: %s\n' "$sha"
		printf '\n'
	} >>"$packages"
done

gzip -9fkn "$packages"
echo "wrote $dir/Packages and $dir/Packages.gz (${#ipks[@]} package(s))"
