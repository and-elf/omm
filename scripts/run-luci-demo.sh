#!/usr/bin/env bash
# Boot a real OpenWrt userland in a container with the full LuCI web UI plus
# our locally built meshd + luci-app-meshd packages, and expose uhttpd on a
# localhost port so the LuCI page (incl. the Mesh Manager view) is browsable.
#
# This is a throwaway demo/inspection environment, not a deployment: meshd and
# the daemons are started by hand (no procd as PID 1) and the root password is
# set to a well-known value so LuCI login works.
#
# Usage: scripts/run-luci-demo.sh [host-port]
set -euo pipefail

CONTAINER="${CONTAINER:-podman}"
IMAGE="${IMAGE:-docker.io/openwrt/rootfs:x86-64-23.05.5}"
PORT="${1:-8088}"
ROOT_PASS="${ROOT_PASS:-admin}"
NAME="${NAME:-omm-luci-demo}"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

echo "==> Building meshd (linux/amd64, static)…"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/meshd ./meshd/cmd/meshd

echo "==> Packaging meshd .ipk…"
./scripts/package-ipk.sh >/dev/null

echo "==> Packaging luci-app-meshd .ipk…"
./scripts/package-luci-ipk.sh >/dev/null

# Stage the built packages where the container can bind-mount them. Keep the
# work dir under /tmp and world-traversable so rootless podman can reach it.
work="$(mktemp -d /tmp/omm-luci-demo.XXXXXX)"
chmod 755 "$work"
cp build/ipk/meshd_*.ipk "$work/"
cp build/luci-ipk/luci-app-meshd_*.ipk "$work/"

cat > "$work/run.sh" <<SCRIPT
#!/bin/sh
set -e
ROOT_PASS='${ROOT_PASS}'
SCRIPT
cat >> "$work/run.sh" <<'SCRIPT'
# The minimal rootfs ships without these runtime dirs; opkg/ubus/rpcd need them.
mkdir -p /var/lock /var/run /var/run/ubus /tmp/lock /etc/meshd

echo "--> opkg update"
opkg update >/dev/null 2>&1 || { echo "FAIL: opkg update"; opkg update 2>&1 | tail -5; exit 1; }

echo "--> installing luci + deps (this pulls uhttpd, rpcd, luci-base…)"
opkg install luci curl >/dev/null 2>&1 || { echo "FAIL: luci install"; exit 1; }

echo "--> installing meshd + luci-app-meshd"
# Packaged as arch 'all', so opkg installs them on any target (the bundled
# binary is amd64, matching this x86-64 container).
opkg install /host/meshd_*.ipk /host/luci-app-meshd_*.ipk

echo "--> setting root password for LuCI login"
printf '%s\n%s\n' "$ROOT_PASS" "$ROOT_PASS" | passwd root >/dev/null 2>&1

# Bring up the service stack by hand. We deliberately do NOT run procd / the
# OpenWrt boot: it would start netifd + the firewall, reconfigure the netns and
# cut off the forwarded port. The one thing procd normally provides that LuCI
# needs is the `system` ubus object (its theme calls `system board` to render
# the header, and 500s on a null). Stub it as an rpcd exec plugin instead — the
# server-side LuCI CGI calls ubus directly as root, so no ACL is involved.
echo "--> installing stub 'system' ubus object (board/info) for the LuCI theme"
cat > /usr/libexec/rpcd/system <<'STUB'
#!/bin/sh
case "$1" in
list) echo '{"board":{},"info":{}}' ;;
call)
	case "$2" in
	board) echo '{"kernel":"container","hostname":"omm-luci-demo","system":"OMM demo","model":"OpenWrt Mesh Manager demo","board_name":"omm","release":{"distribution":"OpenWrt","version":"23.05.5","revision":"omm-demo","target":"x86/64","description":"OpenWrt 23.05.5 (OMM LuCI demo)"}}' ;;
	info)  echo '{"localtime":0,"uptime":0,"load":[0,0,0],"memory":{"total":0,"free":0,"shared":0,"buffered":0,"available":0,"cached":0},"swap":{"total":0,"free":0}}' ;;
	esac ;;
esac
STUB
chmod +x /usr/libexec/rpcd/system

echo "--> starting ubusd / rpcd"
/sbin/ubusd & sleep 1
/sbin/rpcd & sleep 1

# meshd: the luci-app-meshd uci-defaults switched it to split mode with the
# management API on localhost:8080, which is exactly what the rpcd plugin and
# LuCI expect. Start it manually with that posture.
echo "--> starting meshd (mgmt on 127.0.0.1:8080)"
MESHD_ROLE=controller \
MESHD_MGMT_ADDR=127.0.0.1:8080 \
MESHD_MESH_ADDR=0.0.0.0:8081 \
MESHD_DATABASE_PATH=/etc/meshd/meshd.bolt \
MESHD_IDENTITY_DIR=/etc/meshd/identity \
MESHD_HOME_ID=default-home MESHD_HOME_NAME=Home MESHD_CONTROLLER_ID=gw01 \
  /usr/bin/meshd >/tmp/meshd.log 2>&1 &
for i in $(seq 1 20); do
  curl -fs http://127.0.0.1:8080/status >/dev/null 2>&1 && break
  sleep 1
done

echo
echo "================================================================"
echo " LuCI is up. Browse to:   http://127.0.0.1:__PORT__/"
echo " Login:  user 'root'  password '$ROOT_PASS'"
echo " Mesh Manager view:  Network -> Mesh Manager"
echo "================================================================"
echo

# uhttpd in the foreground keeps the container alive. -u /ubus exposes the
# JSON-RPC endpoint LuCI/the PWA use; -x /cgi-bin serves /cgi-bin/luci.
exec uhttpd -f -h /www -r OpenWrt -x /cgi-bin -u /ubus -p 0.0.0.0:80
SCRIPT

# Bake the chosen port into the banner.
sed -i "s/__PORT__/${PORT}/" "$work/run.sh"

echo "==> Removing any previous '${NAME}' container"
"$CONTAINER" rm -f "$NAME" >/dev/null 2>&1 || true

echo "==> Starting container '${NAME}', LuCI on http://127.0.0.1:${PORT}/"
"$CONTAINER" run -d --name "$NAME" \
  -p "127.0.0.1:${PORT}:80" \
  -v "$work:/host:ro" \
  "$IMAGE" /bin/sh /host/run.sh >/dev/null

echo "==> Waiting for LuCI to come up…"
for i in $(seq 1 60); do
  # The unauthenticated login page answers 403 (login required) once LuCI is
  # serving, so match the rendered title rather than a 2xx status.
  if curl -s "http://127.0.0.1:${PORT}/cgi-bin/luci" 2>/dev/null | grep -q "LuCI</title>"; then
    echo
    echo "LuCI ready:  http://127.0.0.1:${PORT}/"
    echo "  login: root / ${ROOT_PASS}"
    echo "  logs:  ${CONTAINER} logs ${NAME}"
    echo "  stop:  ${CONTAINER} rm -f ${NAME}"
    exit 0
  fi
  sleep 2
done

echo "LuCI did not respond in time; container logs:" >&2
"$CONTAINER" logs "$NAME" >&2 || true
exit 1
