#!/usr/bin/env bash
# Verify the luci-app-meshd integration against a real OpenWrt userland: bring
# up ubusd + rpcd + uhttpd (with the ubus JSON-RPC handler) and a meshd behind
# the plugin, then drive meshd over the authenticated /ubus endpoint exactly as
# the PWA does (window.__MESHD_UBUS__ token -> POST /ubus). This exercises the
# full LuCI /ubus -> session auth -> rpcd ACL -> exec plugin -> meshd path.
#
# Requires podman (or CONTAINER=docker) with network access to pull the OpenWrt
# rootfs image and install uhttpd/curl from the feed.
set -euo pipefail

CONTAINER="${CONTAINER:-podman}"
IMAGE="${IMAGE:-docker.io/openwrt/rootfs:x86-64-23.05.5}"
root="$(cd "$(dirname "$0")/.." && pwd)"
# Keep the work dir directly under /tmp and traversable so a rootless container
# can bind-mount it (a per-user $TMPDIR is often not reachable from the userns).
work="$(mktemp -d /tmp/omm-luci-verify.XXXXXX)"
trap 'rm -rf "$work"' EXIT
chmod 755 "$work"

echo "Building meshd (linux/amd64, static)…"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$work/meshd" "$root/meshd/cmd/meshd"
cp "$root/package/luci-app-meshd/root/usr/libexec/rpcd/meshd" "$work/rpcd-meshd"
cp "$root/package/luci-app-meshd/root/usr/share/rpcd/acl.d/luci-app-meshd.json" "$work/acl.json"

cat > "$work/run.sh" <<'SCRIPT'
#!/bin/sh
set -e
mkdir -p /var/lock /var/run/ubus /usr/libexec/rpcd /usr/share/rpcd/acl.d /etc/meshd /www
opkg update >/dev/null 2>&1
opkg install uhttpd uhttpd-mod-ubus curl >/dev/null 2>&1 || { echo "FAIL: package install (no feed?)"; exit 1; }

cp /host/meshd /usr/bin/meshd && chmod +x /usr/bin/meshd
cp /host/rpcd-meshd /usr/libexec/rpcd/meshd && chmod +x /usr/libexec/rpcd/meshd
cp /host/acl.json /usr/share/rpcd/acl.d/luci-app-meshd.json

# Grant the root login ONLY the luci-app-meshd ACL scope, so reaching meshd
# proves our ACL file grants it (not a blanket permission).
cat > /etc/config/rpcd <<EOF
config login
	option username 'root'
	option password '\$p\$root'
	list read 'luci-app-meshd'
	list write 'luci-app-meshd'
EOF
printf 'test\ntest\n' | passwd root >/dev/null 2>&1

/sbin/ubusd & sleep 1
MESHD_HTTP_ADDR=127.0.0.1:8080 MESHD_DATABASE_PATH=/tmp/m.bolt MESHD_IDENTITY_DIR=/tmp/id \
  MESHD_HOME_ID=h1 MESHD_HOME_NAME=Casa /usr/bin/meshd >/tmp/meshd.log 2>&1 &
for i in $(seq 1 20); do curl -fs http://127.0.0.1:8080/status >/dev/null 2>&1 && break; sleep 1; done
/sbin/rpcd & sleep 1
uhttpd -h /www -p 127.0.0.1:80 -u /ubus & sleep 1

ZERO=00000000000000000000000000000000
call() { # session method
	curl -s -X POST http://127.0.0.1/ubus -d \
		'{"jsonrpc":"2.0","id":2,"method":"call","params":["'"$1"'","meshd","'"$2"'",{}]}'
}

fail=0
tok=$(curl -s -X POST http://127.0.0.1/ubus -d \
	'{"jsonrpc":"2.0","id":1,"method":"call","params":["'"$ZERO"'","session","login",{"username":"root","password":"test"}]}' \
	| sed -n 's/.*"ubus_rpc_session":"\([^"]*\)".*/\1/p')
[ -n "$tok" ] || { echo "FAIL: session login"; exit 1; }

out=$(call "$tok" status)
echo "authenticated session  -> meshd.status: $out"
echo "$out" | grep -q '"status":"ready"' || { echo "FAIL: ACL-granted session cannot reach meshd"; fail=1; }

# Negative: the unauthenticated pre-session (no meshd grant) must be denied.
denied=$(call "$ZERO" status)
echo "unauthenticated session -> meshd.status: $denied"
echo "$denied" | grep -q '"status":"ready"' && { echo "FAIL: unauthenticated session reached meshd (ACL not enforced)"; fail=1; }

[ "$fail" = 0 ] && echo "OK: authenticated /ubus path works and the luci-app-meshd ACL gates it" || exit 1
SCRIPT

"$CONTAINER" run --rm -v "$work:/host:ro" "$IMAGE" /bin/sh /host/run.sh
