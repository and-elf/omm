#!/usr/bin/env bash
# Generate the EC keypair that signs apk packages and the apk feed index.
#
# Writes two files in the current directory:
#   omm-apk.key  — SECRET key (EC prime256v1, PEM). Never commit it. Store it as
#                  the APK_SIGN_KEY CI secret (see below) and keep a backup
#                  offline.
#   omm-apk.pub  — PUBLIC key (PEM). Safe to commit; the release workflow
#                  publishes it so devices can trust it under /etc/apk/keys/.
#
# apk verifies each package against trusted public keys by key *content* (the
# filename on the device is irrelevant), so this keypair is independent of the
# usign keypair used for the opkg feed — the two schemes share nothing. Run this
# ONCE; rotating the key means re-trusting it on every device.
#
# Uses the local openssl (apk's EC curve is prime256v1, the OpenWrt default).
set -euo pipefail

cn="${KEY_COMMENT:-OMM apk package signing key}"

if [ -e omm-apk.key ] || [ -e omm-apk.pub ]; then
	echo "refusing to overwrite existing omm-apk.key/omm-apk.pub" >&2
	exit 1
fi

if ! command -v openssl >/dev/null 2>&1; then
	echo "openssl not found (required to generate the EC keypair)" >&2
	exit 1
fi

# EC prime256v1 (P-256) — what OpenWrt's apk uses (BUILD_KEY_APK_SEC).
openssl ecparam -name prime256v1 -genkey -noout -out omm-apk.key
openssl ec -in omm-apk.key -pubout -out omm-apk.pub 2>/dev/null
chmod 600 omm-apk.key

echo
echo "Generated ($cn):"
echo "  omm-apk.pub  (public — commit to package/omm-apk.pub)"
echo "  omm-apk.key  (secret — DO NOT commit)"
echo
echo "Next steps:"
echo "  1. gh secret set APK_SIGN_KEY < omm-apk.key      # store the secret key"
echo "     (GitLab: glab variable set APK_SIGN_KEY --scope '*' < omm-apk.key)"
echo "  2. mkdir -p package && cp omm-apk.pub package/omm-apk.pub && git add package/omm-apk.pub && git commit"
echo "  3. back up omm-apk.key offline, then: rm omm-apk.key"
