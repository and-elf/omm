#!/usr/bin/env bash
# Print the CHANGELOG.md section body for a given version, so the release
# workflow can use it as the GitHub Release notes instead of an auto-generated
# commit list. The heading line itself is omitted; leading/trailing blank lines
# are trimmed. Exits non-zero if the version has no section.
#
# Usage: changelog-extract.sh <version> [changelog-path]   e.g. 0.1.0
set -euo pipefail

version="${1:?usage: changelog-extract.sh <version> [changelog-path]}"
file="${2:-CHANGELOG.md}"

# Capture lines after "## [<version>] …" up to (but not including) the next
# "## [" heading. The version is matched literally (a '.' is escaped).
section="$(awk -v ver="$version" '
	BEGIN { esc = ver; gsub(/\./, "\\.", esc); pat = "^## \\[" esc "\\]" }
	$0 ~ pat { inseg = 1; next }
	# Stop at the next version heading, or at the trailing link-reference block
	# (e.g. "[0.1.0]: https://…") when this is the last section.
	inseg && (/^## \[/ || /^\[[^]]+\]: /) { exit }
	inseg { print }
' "$file")"

# Trim leading and trailing blank lines.
section="$(printf '%s\n' "$section" | sed -e '/./,$!d' | sed -e ':a' -e '/^\n*$/{$d;N;ba}')"

if [ -z "$section" ]; then
	echo "no CHANGELOG section for version '$version' in $file" >&2
	exit 1
fi

printf '%s\n' "$section"
