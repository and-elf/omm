#!/usr/bin/env bash
# Round-trip test for apk package + feed-index signing, the apk analogue of the
# usign round-trip we rely on for the opkg feed.
#
# With a throwaway EC keypair it asserts that:
#   * package-apk.sh emits a REAL apk-v3 package (readable by `apk adbdump`),
#     not the old extract-only gzip tar;
#   * `apk verify` accepts the package against the trusted key, and REJECTS it
#     with a wrong key, with in-body corruption, and when truncated;
#   * make-apk-index.sh produces a signed packages.adb that verifies, and a
#     tampered index is rejected;
#   * an unsigned build (no key) produces a package apk treats as UNTRUSTED.
#
# Runs both a per-arch package and an arch:all (noarch) package. Linux only
# (apk is a Linux build-host tool). Exits non-zero on the first failure.
set -euo pipefail
cd "$(dirname "$0")/.."

APK="${APK:-$(scripts/get-apk-tools.sh)}"
export APK
echo "using apk: $APK ($("$APK" --version 2>&1))"

work="$(mktemp -d)"
created_stub_meshd=0
cleanup() {
	rm -rf "$work"
	[ "$created_stub_meshd" = 1 ] && rm -f bin/meshd
	return 0
}
trap cleanup EXIT

# package-apk.sh installs bin/meshd; for a signing test the bytes are irrelevant,
# so stub it if a real build is not present.
if [ ! -e bin/meshd ]; then
	mkdir -p bin
	printf '#!/bin/sh\necho meshd-stub\n' > bin/meshd
	chmod 0755 bin/meshd
	created_stub_meshd=1
fi

pass=0 fail=0
ok()   { echo "PASS: $1"; pass=$((pass+1)); }
bad()  { echo "FAIL: $1" >&2; fail=$((fail+1)); }
# Assert a command succeeds / fails.
expect_ok()   { if "$@" >/dev/null 2>&1; then ok "$desc"; else bad "$desc"; fi; }
expect_fail() { if "$@" >/dev/null 2>&1; then bad "$desc (should have failed)"; else ok "$desc"; fi; }

# Trusted keypair and a different "wrong" key.
openssl ecparam -name prime256v1 -genkey -noout -out "$work/signer.key"
mkdir -p "$work/keys"
openssl ec -in "$work/signer.key" -pubout -out "$work/keys/omm-apk.pub" 2>/dev/null
mkdir -p "$work/wrongkeys"
openssl ecparam -name prime256v1 -genkey -noout -out "$work/other.key"
openssl ec -in "$work/other.key" -pubout -out "$work/wrongkeys/omm-apk.pub" 2>/dev/null

verify() { "$APK" verify --keys-dir "$1" "$2"; }   # $1 must be an ABSOLUTE dir

for arch in x86_64 all; do
	echo "--- arch:$arch ---"
	rm -rf build/apk-test; mkdir -p build/apk-test
	APK_SIGN_KEY_FILE="$work/signer.key" VERSION=9.9.9 ARCH="$arch" \
		OUTPUT_DIR=build/apk-test scripts/package-apk.sh >/dev/null
	pkg="build/apk-test/meshd-9.9.9-$arch.apk"

	desc="arch:$arch package exists"; expect_ok test -f "$pkg"
	desc="arch:$arch is a real apk (adbdump-readable)"; expect_ok "$APK" adbdump "$pkg"
	desc="arch:$arch verifies with the trusted key"; expect_ok verify "$work/keys" "$pkg"
	desc="arch:$arch rejected with a wrong key"; expect_fail verify "$work/wrongkeys" "$pkg"

	# In-body corruption: overwrite bytes in the middle of the signed container
	# (appending trailing bytes is NOT detected — apk ignores them).
	sz="$(wc -c < "$pkg")"; mid="$pkg.mid"; cp "$pkg" "$mid"
	printf '\xa5\xa5\xa5\xa5\xa5\xa5\xa5\xa5' |
		dd of="$mid" bs=1 seek="$((sz/2))" count=8 conv=notrunc status=none
	desc="arch:$arch rejected after in-body corruption"; expect_fail verify "$work/keys" "$mid"

	trunc="$pkg.trunc"; head -c "$((sz-20))" "$pkg" > "$trunc"
	desc="arch:$arch rejected when truncated"; expect_fail verify "$work/keys" "$trunc"
done

echo "--- signed feed index ---"
rm -rf build/apk-test; mkdir -p build/apk-test
for arch in x86_64 aarch64_generic; do
	APK_SIGN_KEY_FILE="$work/signer.key" VERSION=9.9.9 ARCH="$arch" \
		OUTPUT_DIR=build/apk-test scripts/package-apk.sh >/dev/null
done
APK_SIGN_KEY_FILE="$work/signer.key" scripts/make-apk-index.sh build/apk-test >/dev/null
idx="build/apk-test/packages.adb"
desc="signed index exists"; expect_ok test -f "$idx"
desc="signed index is adb-readable"; expect_ok "$APK" adbdump "$idx"
desc="signed index verifies with the trusted key"; expect_ok verify "$work/keys" "$idx"
isz="$(wc -c < "$idx")"; cp "$idx" "$idx.mid"
printf '\xa5\xa5\xa5\xa5\xa5\xa5\xa5\xa5' |
	dd of="$idx.mid" bs=1 seek="$((isz/2))" count=8 conv=notrunc status=none
desc="tampered index is rejected"; expect_fail verify "$work/keys" "$idx.mid"

echo "--- unsigned build is untrusted ---"
rm -rf build/apk-test; mkdir -p build/apk-test
VERSION=9.9.9 ARCH=x86_64 OUTPUT_DIR=build/apk-test scripts/package-apk.sh >/dev/null
unsigned="build/apk-test/meshd-9.9.9-x86_64.apk"
desc="unsigned package is still a real apk"; expect_ok "$APK" adbdump "$unsigned"
desc="unsigned package is not trusted"; expect_fail verify "$work/keys" "$unsigned"

rm -rf build/apk-test
echo "============================="
echo "apk signing: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
