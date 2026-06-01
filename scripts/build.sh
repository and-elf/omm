#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p bin

# Build the PWA frontend first so its assets are embedded into the meshd
# binary via the //go:embed directive in web/embed.go. Skip with
# SKIP_FRONTEND=1 (e.g. when iterating on Go-only changes).
if [ "${SKIP_FRONTEND:-0}" != "1" ]; then
  if command -v npm >/dev/null 2>&1; then
    echo "Building PWA frontend"
    (cd web && npm ci && npm run build)
  else
    echo "WARNING: npm not found; embedding existing web/dist (run 'npm run build' in web/ for a fresh frontend)" >&2
  fi
fi

go build -o bin/meshd ./meshd/cmd/meshd

echo "Built bin/meshd"
