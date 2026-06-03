#!/usr/bin/env bash
# Boot two meshd daemons in OpenWrt containers — a controller and an unclaimed
# node — each serving its embedded PWA, so the enrollment flow can be driven by
# hand in a browser. Combined mode (one HTTP plane), started directly (no
# procd), with the setup AP disabled (containers have no radios).
#
#   scripts/run-dev-stack.sh          # build, package, start controller + node
#   scripts/run-dev-stack.sh down     # tear everything down
#   SKIP_BUILD=1 scripts/run-dev-stack.sh   # reuse the existing build/ipk
#   AUTO_ADOPT=1 scripts/run-dev-stack.sh   # controller auto-approves (no manual step)
#   DEV_CORS=1   scripts/run-dev-stack.sh   # allow a cross-origin companion app
#                                           # (Vite dev server / /onboard wizard)
#
# Manual flow once up:
#   1. Node PWA  -> Setup -> "Join an existing Home" -> http://omm-ctrl:8080
#      (the container alias; the node dials the controller daemon-to-daemon).
#      The Join spins — POST /enroll/join blocks until the controller approves.
#   2. Controller PWA -> Enroll -> Approve the pending node.
#   3. Node reaches "active"; it appears in the controller's Nodes view.
set -euo pipefail

CONTAINER="${CONTAINER:-podman}"
IMAGE="${IMAGE:-docker.io/openwrt/rootfs:x86-64-23.05.5}"
NET="${NET:-omm-dev}"
CTRL_PORT="${CTRL_PORT:-8080}"
NODE_PORT="${NODE_PORT:-8081}"
AUTO_ADOPT="${AUTO_ADOPT:-0}"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

# Remove the containers and network (but not staged work dirs).
stop_containers() {
  "$CONTAINER" rm -f omm-ctrl omm-node >/dev/null 2>&1 || true
  "$CONTAINER" network rm "$NET" >/dev/null 2>&1 || true
}

if [ "${1:-}" = "down" ]; then
  echo "==> tearing down the dev stack"
  stop_containers
  rm -rf /tmp/omm-dev-stack.* 2>/dev/null || true
  echo "done"
  exit 0
fi

if [ "${SKIP_BUILD:-0}" != "1" ]; then
  echo "==> Building meshd + frontend (SKIP_BUILD=1 to reuse the existing build)…"
  # build.sh builds the PWA then compiles meshd for the host arch (amd64 here,
  # matching the x86-64 container), embedding the current frontend.
  CGO_ENABLED=0 ./scripts/build.sh >/dev/null
  echo "==> Packaging meshd .ipk…"
  ./scripts/package-ipk.sh >/dev/null
fi

ipk="$(ls build/ipk/meshd_*.ipk 2>/dev/null | head -1 || true)"
if [ -z "$ipk" ]; then
  echo "No build/ipk/meshd_*.ipk found — run without SKIP_BUILD first." >&2
  exit 1
fi

# Stage the package where rootless podman can bind-mount it.
work="$(mktemp -d /tmp/omm-dev-stack.XXXXXX)"
chmod 755 "$work"
cp "$ipk" "$work/"
cat > "$work/run.sh" <<'SCRIPT'
#!/bin/sh
set -e
mkdir -p /var/lock /var/run /etc/meshd
opkg install /host/meshd_*.ipk >/dev/null 2>&1 || opkg install /host/meshd_*.ipk
# meshd reads its MESHD_* settings from the environment (passed via -e).
exec /usr/bin/meshd
SCRIPT

echo "==> (re)creating network and containers"
stop_containers
"$CONTAINER" network create "$NET" >/dev/null

common=(
  -v "$work:/host:ro"
  -e MESHD_HTTP_ADDR=0.0.0.0:8080
  -e MESHD_SETUP_AP=0
  -e MESHD_DATABASE_PATH=/etc/meshd/m.bolt
  -e MESHD_IDENTITY_DIR=/etc/meshd/id
  # DEV_CORS=1 lets a companion app on another origin (e.g. the Vite dev server,
  # or the /onboard wizard) call these daemons directly. Dev only.
  -e "MESHD_DEV_CORS=${DEV_CORS:-0}"
)

# Controller: its own Home, manual adopt by default.
"$CONTAINER" run -d --name omm-ctrl --network "$NET" -p "127.0.0.1:${CTRL_PORT}:8080" \
  "${common[@]}" \
  -e MESHD_AUTO_ADOPT="$AUTO_ADOPT" \
  -e MESHD_HOME_ID=home-main -e MESHD_HOME_NAME=Main_House -e MESHD_CONTROLLER_ID=gw01 \
  "$IMAGE" /bin/sh /host/run.sh >/dev/null

# Node: unclaimed, its own (last-resort) Home.
"$CONTAINER" run -d --name omm-node --network "$NET" -p "127.0.0.1:${NODE_PORT}:8080" \
  "${common[@]}" \
  -e MESHD_HOME_ID=home-node -e MESHD_HOME_NAME=NewNode -e MESHD_SERIAL=node-1 \
  "$IMAGE" /bin/sh /host/run.sh >/dev/null

echo "==> waiting for both daemons…"
for pp in "$CTRL_PORT" "$NODE_PORT"; do
  ok=""
  for _ in $(seq 1 30); do
    if curl -fs "http://127.0.0.1:${pp}/status" >/dev/null 2>&1; then ok=1; break; fi
    sleep 1
  done
  if [ -z "$ok" ]; then
    echo "daemon on :${pp} did not come up; logs:" >&2
    "$CONTAINER" logs omm-ctrl omm-node >&2 || true
    exit 1
  fi
done

cat <<BANNER

================================================================
 Dev stack up (auto_adopt=${AUTO_ADOPT}).
   Controller PWA : http://127.0.0.1:${CTRL_PORT}/   (Home "Main House")
   Node PWA       : http://127.0.0.1:${NODE_PORT}/   (unclaimed)

 Manual enrollment:
   1) Node PWA  -> Setup -> Join an existing Home -> http://omm-ctrl:8080
   2) Controller PWA -> Enroll -> Approve the pending node
   3) Controller PWA -> Nodes  -> the node shows current_home=home-main

 Logs:  ${CONTAINER} logs -f omm-ctrl   (or omm-node)
 Stop:  scripts/run-dev-stack.sh down
================================================================
BANNER
