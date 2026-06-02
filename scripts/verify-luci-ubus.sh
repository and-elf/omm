#!/usr/bin/env bash
# Verify the luci-app-meshd rpcd plugin against a real OpenWrt userland: start
# ubusd + rpcd with the plugin installed and a meshd behind it, then confirm the
# `meshd` ubus object registers and proxies calls. This exercises the same
# LuCI /ubus -> rpcd -> meshd path the PWA's ubus transport uses.
#
# Requires podman (or set CONTAINER=docker) with network access to pull the
# OpenWrt rootfs image. The container lacks a package feed, so the plugin's
# `curl` fetcher is shimmed over uclient-fetch for GET methods.
set -euo pipefail

CONTAINER="${CONTAINER:-podman}"
IMAGE="${IMAGE:-docker.io/openwrt/rootfs:x86-64-23.05.5}"
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "Building meshd (linux/amd64, static)…"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$work/meshd" "$root/meshd/cmd/meshd"
cp "$root/package/luci-app-meshd/root/usr/libexec/rpcd/meshd" "$work/rpcd-meshd"
cp "$root/package/luci-app-meshd/root/usr/share/rpcd/acl.d/luci-app-meshd.json" "$work/acl.json"

cat > "$work/run.sh" <<'SCRIPT'
#!/bin/sh
set -e
# Shim curl over uclient-fetch for GET (no package feed in the container).
cat > /usr/bin/curl <<'EOF'
#!/bin/sh
for a in "$@"; do url="$a"; done
uclient-fetch -q -O - "$url" 2>/dev/null || wget -q -O - "$url"
EOF
chmod +x /usr/bin/curl
mkdir -p /usr/libexec/rpcd /usr/share/rpcd/acl.d /var/run/ubus /var/lock /etc/meshd
cp /host/meshd /usr/bin/meshd && chmod +x /usr/bin/meshd
cp /host/rpcd-meshd /usr/libexec/rpcd/meshd && chmod +x /usr/libexec/rpcd/meshd
cp /host/acl.json /usr/share/rpcd/acl.d/luci-app-meshd.json
/sbin/ubusd & sleep 1
MESHD_HTTP_ADDR=127.0.0.1:8080 MESHD_DATABASE_PATH=/tmp/m.bolt MESHD_IDENTITY_DIR=/tmp/id \
  MESHD_HOME_ID=h1 MESHD_HOME_NAME=Casa /usr/bin/meshd >/tmp/meshd.log 2>&1 &
for i in $(seq 1 30); do curl http://127.0.0.1:8080/status >/dev/null 2>&1 && break; sleep 0.3; done
/sbin/rpcd & sleep 1

fail=0
ubus list | grep -qx meshd || { echo "FAIL: meshd ubus object not registered"; fail=1; }
echo "ubus call meshd status -> $(ubus -S call meshd status)"
ubus -S call meshd status | grep -q '"status":"ready"' || { echo "FAIL: status proxy"; fail=1; }
ubus -S call meshd get_home '{"home_id":"h1"}' | grep -q '"id":"h1"' || { echo "FAIL: get_home path param"; fail=1; }
[ "$fail" = 0 ] && echo "OK: meshd ubus object registers and proxies" || exit 1
SCRIPT

"$CONTAINER" run --rm -v "$work:/host:ro" "$IMAGE" /bin/sh /host/run.sh
