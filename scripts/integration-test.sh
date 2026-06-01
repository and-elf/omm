#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

./scripts/build.sh

if command -v podman >/dev/null 2>&1; then
	echo "Running Podman-based integration test"
	podman build -f scripts/Containerfile -t omm-integration .
	podman run --rm -d --name omm-integration -p 8080:8080/tcp -p 45678:45678/udp omm-integration
	sleep 2
	curl -fs http://127.0.0.1:8080/health
	python3 - <<'PY'
import socket, time
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.sendto(b'ping', ('127.0.0.1', 45678))
try:
    s.settimeout(2.0)
    data, _ = s.recvfrom(1024)
    print('udp ok', data)
except socket.timeout:
    print('udp no response')
    raise
PY
	podman stop omm-integration
	echo "Podman-based integration test passed"
else
	echo "Podman not found, running local integration test"
	./bin/meshd &
	pid=$!
	trap 'kill "$pid"' EXIT
	sleep 1
	curl -fs http://127.0.0.1:8080/health
	python3 - <<'PY'
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.connect(('127.0.0.1', 45678))
s.send(b'ping')
print('udp probe sent')
PY
	echo "Local integration test passed"
fi
