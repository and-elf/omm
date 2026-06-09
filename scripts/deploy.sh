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
join_url=""
sets=()
while [ $# -gt 0 ]; do
	case "$1" in
	--set) sets+=("$2"); shift 2 ;;
	--no-build) build=0; shift ;;
	--no-swap) swap=0; build=0; shift ;; # skip building/swapping the binary
	--reset) reset=1; shift ;;           # wipe meshd state (DB) for a clean run
	--watch) watch="$2"; shift 2 ;;      # tail meshd log for N seconds at the end
	--join) join_url="$2"; shift 2 ;;    # after deploy, enroll into this controller
	*) echo "unknown argument: $1" >&2; exit 1 ;;
	esac
done

SSH_OPTS=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=8)
ssh_() { ssh "${SSH_OPTS[@]}" "root@$host" "$@"; }
# -O forces the legacy SCP transfer protocol: OpenWrt's dropbear has no
# sftp-server, so the default (SFTP) fails with "sftp-server: not found".
scp_() { scp -O "${SSH_OPTS[@]}" "$1" "root@$host:$2"; }

if [ "$swap" = 1 ]; then
	echo "==> $host: detecting arch"
	arch="$(ssh_ 'uname -m')"
	case "$arch" in
	aarch64)      device=zb8103ax ;;
	armv7l|armv7) device=lyra-ac2200 ;;
	*) echo "unsupported arch '$arch' on $host (known: aarch64, armv7l)" >&2; exit 1 ;;
	esac
	echo "    $arch -> $device"

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

# Restart when the binary changed, state was reset, or config changed; a
# join-only run doesn't.
if [ "$swap" = 1 ] || [ "$reset" = 1 ] || [ "$applied_sets" = 1 ]; then
	echo "==> (re)starting meshd"
	ssh_ '/etc/init.d/meshd restart; sleep 2; logread -e meshd | tail -4'
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
