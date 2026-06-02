#!/usr/bin/env bash
# Map a git ref to a package version string valid for BOTH opkg and apk.
#
# apk's version grammar (apk-tools 3) is stricter than opkg's: the only legal
# use of '-' is the '-rN' release revision, and the pre/post-release suffix is
# introduced with '_' (e.g. _rc1, _git). So a tag must yield an apk-valid
# version, and non-tag builds use "0.0.0_git" — NOT "0.0.0-dev", which opkg
# accepts but `apk mkpkg` rejects ("package version is invalid").
#
# Release tags are expected to be apk-valid already (e.g. v1.2.3, v1.2.3_rc1);
# a leading "v" is stripped. Usage: derive-version.sh [ref]  (default: the CI
# ref $GITHUB_REF_NAME).
set -euo pipefail

ref="${1-${GITHUB_REF_NAME:-}}"
case "$ref" in
	v*) echo "${ref#v}" ;;
	*)  echo "0.0.0_git" ;;
esac
