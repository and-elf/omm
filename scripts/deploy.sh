#!/usr/bin/env bash
# Deploy a freshly-built meshd to a live test device over SSH: detect the
# device's arch, build the matching binary (scripts/build-devices.sh), stage it
# in /tmp, swap it in, optionally apply UCI settings, and restart meshd.
#
# Auth: uses your SSH setup. Set up key auth once so it is passwordless and no
# secret lives in this repo:
#     ssh-copy-id root@controller && ssh-copy-id root@node
# (For password auth, wrap the call with sshpass yourself — never commit a
# password; the repo's gitleaks hook would reject it anyway.)
#
# Usage:
#   ./scripts/deploy.sh <host> [--set option=value]... [--no-build]
#
# Examples:
#   ./scripts/deploy.sh controller --set adopt_policy=onlink
#   ./scripts/deploy.sh node
#   ./scripts/deploy.sh node --set auto_onboard_wired=1 --set backhaul_iface=eth0
#   ./scripts/deploy.sh node --reset --controller controller --join https://10.0.0.1:8443
#
# --reset wipes a node's identity, so on its next join it enrolls under a fresh
# node ID and its old record is left orphaned on the controller (a "down" ghost
# in the topology). Pass --controller <host> with --reset to delete that prior
# record from the controller before it is orphaned, keeping the topology clean.
set -euo pipefail
cd "$(dirname "$0")/.."

host="${1:-}"
if [ -z "$host" ] || [ "$host" = "-h" ] || [ "$host" = "--help" ]; then
	echo "usage: $0 <host> [--set option=value]... [--no-build]" >&2
	exit 1
fi
shift

build=1
swap=1
reset=0
watch=0
install_deps=0
join_url=""
controller=""
sets=()
while [ $# -gt 0 ]; do
	case "$1" in
	--set) sets+=("$2"); shift 2 ;;
	--no-build) build=0; shift ;;
	--no-swap) swap=0; build=0; shift ;;             # skip building/swapping the binary
	--reset) reset=1; shift ;;                       # wipe meshd state (DB) for a clean run
	--watch) watch="$2"; shift 2 ;;                  # tail meshd log for N seconds at the end
	--join) join_url="$2"; shift 2 ;;                # after deploy, enroll into this controller
	--controller) controller="$2"; shift 2 ;;        # with --reset: delete this node's prior record from the controller (SSH host alias)
	--install-dependencies) install_deps=1; shift ;; # swap in mesh-capable wpad + install kmod-batman-adv/batctl
	*) echo "unknown argument: $1" >&2; exit 1 ;;
	esac
done

SSH_OPTS=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=8)
ssh_() { ssh "${SSH_OPTS[@]}" "root@$host" "$@"; }
ssh_ctrl() { ssh "${SSH_OPTS[@]}" "root@$controller" "$@"; }
# -O forces the legacy SCP transfer protocol: OpenWrt's dropbear has no
# sftp-server, so the default (SFTP) fails with "sftp-server: not found".
scp_() { scp -O "${SSH_OPTS[@]}" "$1" "root@$host:$2"; }

# Before --reset wipes the node's identity, capture the node ID it currently
# enrolls under, so we can delete that record from the controller afterwards
# instead of orphaning it as a "down" ghost. Read via the `meshd` ubus object
# (the rpcd plugin proxies to the local API over busybox nc — OpenWrt has no
# curl); meshd must be up to answer. A fresh/unjoined node yields no ID and we
# skip cleanup.
old_node_id=""
if [ "$reset" = 1 ] && [ -n "$controller" ]; then
	echo "==> $host: reading current node ID before reset"
	old_node_id="$(ssh_ 'ubus call meshd setup 2>/dev/null' \
		| sed -n 's/.*"node_id"[[:space:]]*:[[:space:]]*"\([0-9a-f]*\)".*/\1/p')"
	if [ -n "$old_node_id" ]; then
		echo "    will remove $old_node_id from $controller after reset"
	else
		echo "    no current node ID (fresh/unjoined node) — nothing to remove"
	fi
fi

if [ "$swap" = 1 ]; then
	echo "==> $host: detecting device"
	# Two of the test boards share one ISA — the Xiaomi AX3600 and the ZB8103AX
	# are both aarch64_cortex-a53, so `uname -m` alone can't tell them apart. Read
	# the exact board model first and special-case those, then fall back to arch
	# for any board we don't name explicitly (their per-ISA binaries are
	# interchangeable within the group anyway; the label just picks bin/<device>).
	board="$(ssh_ 'cat /tmp/sysinfo/board_name 2>/dev/null')"
	arch="$(ssh_ 'uname -m')"
	case "$board" in
	xiaomi,ax3600) device=ax3600 ;;
	*)
		case "$arch" in
		aarch64)      device=zb8103ax ;;
		armv7l|armv7) device=lyra-ac2200 ;;
		*) echo "unsupported board '$board' / arch '$arch' on $host (known boards: xiaomi,ax3600; archs: aarch64, armv7l)" >&2; exit 1 ;;
		esac ;;
	esac
	echo "    ${board:-$arch} -> $device"

	if [ "$build" = 1 ]; then
		echo "==> building $device"
		./scripts/build-devices.sh "$device"
	fi
	[ -f "bin/$device/meshd" ] || { echo "missing bin/$device/meshd (build first)" >&2; exit 1; }

	echo "==> staging binary + init script$([ "$reset" = 1 ] && echo ' + fresh config')"
	# Deploy the package, not just the binary: the init script maps UCI->env and
	# changes alongside the binary, so a stale init (e.g. an old config_get
	# default) would mask new behaviour. On --reset also restore the default
	# config so the device is fully clean (network/other UCI untouched — the LAN
	# address etc. live in /etc/config/network).
	scp_ "bin/$device/meshd" /tmp/meshd.new
	scp_ package/meshd/files/meshd.init /tmp/meshd.init.new
	reset_cmd=""
	cfg_cmd=""
	if [ "$reset" = 1 ]; then
		# Factory-wipe state (bolt DB + identity/Home CA; meshd regenerates both).
		reset_cmd='rm -rf /etc/meshd/meshd.bolt /etc/meshd/identity; '
		scp_ package/meshd/files/meshd.config /tmp/meshd.config.new
		cfg_cmd='mv /tmp/meshd.config.new /etc/config/meshd; '
	fi
	# Stop before swapping: a running executable can't be overwritten in place
	# (ETXTBSY), so mv the staged files over the stopped service.
	ssh_ "/etc/init.d/meshd stop; ${reset_cmd}${cfg_cmd}mv /tmp/meshd.new /usr/bin/meshd; chmod 0755 /usr/bin/meshd; mv /tmp/meshd.init.new /etc/init.d/meshd; chmod 0755 /etc/init.d/meshd"

	# Sync the freshly built PWA to the LuCI-served copy too. The LuCI topology
	# view loads the PWA in an iframe from this path, shipped by the
	# luci-app-meshd package; the binary swap above updates only meshd's own
	# embedded copy on :8080, not this one — so without this the LuCI UI stays
	# frozen at whatever the package last installed. Only when the luci-app is
	# present (its view dir exists) and a frontend was built.
	luci_view=/www/luci-static/resources/view/meshd
	if [ -f web/dist/index.html ] && ssh_ "[ -d $luci_view ]"; then
		echo "==> $host: syncing PWA to LuCI ($luci_view/pwa)"
		ssh_ "rm -rf $luci_view/pwa && mkdir -p $luci_view/pwa"
		tar -C web/dist --exclude=.gitkeep -cf - . | ssh_ "tar -C $luci_view/pwa -xf -"
	fi
elif [ "$reset" = 1 ]; then
	echo "==> factory-wiping meshd state (no swap)"
	ssh_ '/etc/init.d/meshd stop; rm -rf /etc/meshd/meshd.bolt /etc/meshd/identity'
fi

applied_sets=0
for kv in "${sets[@]:-}"; do
	[ -n "$kv" ] || continue
	key="${kv%%=*}"; val="${kv#*=}"
	echo "==> uci set meshd.main.$key=$val"
	ssh_ "uci set meshd.main.$key='$val'; uci commit meshd"
	applied_sets=1
done

# Ensure the on-device dependencies meshd needs for a full mesh, before the
# restart so meshd re-applies the profile with them present:
#   - a mesh-capable wpad so 802.11s can form (the stock wpad-basic-* can't), and
#   - the batman-adv stack (kmod + netifd proto + batctl) so the mesh routes
#     multi-hop instead of degrading to a direct mesh-on-lan bridge.
# Online-only and idempotent; detects the installed wpad crypto variant so it
# matches the image (mbedtls/wolfssl/openssl).
if [ "$install_deps" = 1 ]; then
	echo "==> installing mesh dependencies (wpad-mesh + batman-adv)"
	ssh_ 'sh -s' <<'REMOTE'
set -e
# Works with both package managers: apk (OpenWrt 25+/snapshot) and opkg (<=24.10).
if command -v apk >/dev/null 2>&1; then PM=apk; else PM=opkg; fi
inst() { if [ "$PM" = apk ]; then apk info 2>/dev/null; else opkg list-installed 2>/dev/null; fi; }
pm_update() { if [ "$PM" = apk ]; then apk update; else opkg update; fi; }
pm_add() { if [ "$PM" = apk ]; then apk add "$@"; else opkg install "$@"; fi; }

updated=0
ensure_update() { [ "$updated" = 1 ] || { pm_update; updated=1; }; }

# 1) mesh-capable wpad ------------------------------------------------------
if inst | grep -qE 'wpad-(mesh|wolfssl|openssl|mbedtls)'; then
	echo "wpad already mesh-capable: $(inst | grep -oE 'wpad[^ ]*' | head -1)"
else
	cur=$(inst | grep -oE 'wpad-basic-(mbedtls|wolfssl|openssl)' | head -1)
	if [ -z "$cur" ]; then
		echo "no wpad-basic-* found; install a wpad-mesh-* manually"
	else
		crypto=${cur##*-}
		echo "[$PM] swapping $cur -> wpad-mesh-$crypto"
		ensure_update
		# The basic and mesh variants conflict (same wpad provides) and neither
		# manager auto-replaces a conflicting provider, so remove then add. Brief
		# wpad gap; wifi reconfigures on the meshd restart that follows.
		if [ "$PM" = apk ]; then apk del "$cur"; else opkg remove "$cur" >/dev/null 2>&1 || true; fi
		pm_add "wpad-mesh-$crypto"
		echo "installed: $(inst | grep -oE 'wpad[^ ]*' | head -1)"
	fi
fi

# 2) batman-adv routing stack ----------------------------------------------
# kmod-batman-adv is the kernel module and ships the netifd batadv/batadv_hardif
# protocol handlers meshd authors (there is no separate userspace batman-adv
# package); batctl is the topology read tool. Without them meshd degrades to a
# single-hop lan bridge.
for pkg in kmod-batman-adv batctl; do
	if inst | grep -qE "^${pkg}( |-[0-9]|$)"; then
		echo "$pkg already installed"
	else
		echo "[$PM] installing $pkg"
		ensure_update
		pm_add "$pkg"
	fi
done
REMOTE
	# netifd loads its protocol handlers (batadv/batadv_hardif) only at process
	# start. If batman-adv was installed after netifd came up, the running netifd
	# doesn't know the batadv proto — the bat0 soft interface stays `proto none`
	# and never instantiates. A plain `network reload` does NOT reload handlers,
	# so restart netifd here. This drops our SSH briefly (tolerated), then we wait
	# for the network to recover before the meshd restart below.
	echo "==> restarting netifd so it loads the batman-adv proto handlers"
	ssh_ '/etc/init.d/network restart' >/dev/null 2>&1 || true
	sleep 8
fi

# Restart when the binary changed, state was reset, or config changed; a
# join-only run doesn't.
if [ "$swap" = 1 ] || [ "$reset" = 1 ] || [ "$applied_sets" = 1 ] || [ "$install_deps" = 1 ]; then
	echo "==> (re)starting meshd"
	ssh_ '/etc/init.d/meshd restart; sleep 2; logread -e meshd | tail -4'
fi

# Remove the node's pre-reset record from the controller so it doesn't linger as
# an orphaned "down" vertex. delete_node maps to DELETE /nodes/{id}, which also
# drops the enrollment record. Best-effort: the call returns cleanly whether the
# record existed or not, and an unreachable controller must not fail the deploy.
if [ "$reset" = 1 ] && [ -n "$controller" ] && [ -n "$old_node_id" ]; then
	echo "==> $controller: removing orphaned node record $old_node_id"
	ssh_ctrl "ubus call meshd delete_node '{\"node_id\":\"$old_node_id\"}'" >/dev/null 2>&1 \
		|| echo "    (controller unreachable — skipped)"
fi

if [ -n "$join_url" ]; then
	echo "==> joining $join_url"
	ssh_ "ubus call meshd join_home '{\"controller_url\":\"$join_url\"}'"
fi

if [ "${watch:-0}" -gt 0 ] 2>/dev/null; then
	echo "==> watching ${watch}s of meshd log"
	ssh_ "sleep $watch; logread -e meshd | tail -20"
fi

echo "OK: $host"
