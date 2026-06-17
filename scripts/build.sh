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
    # Clear previously-built assets first. vite is configured emptyOutDir:false
    # (so a no-frontend build still has a dist to embed), which otherwise lets
    # stale hashed bundles accumulate — and since the build embeds `all:dist`,
    # an outdated index.html/chunk can get baked into the binary, serving an old
    # PWA against a current backend. Keep the tracked .gitkeep the no-frontend
    # path relies on.
    find web/dist -mindepth 1 ! -name .gitkeep -delete
    (cd web && npm ci && npm run build)
  else
    echo "WARNING: npm not found; embedding existing web/dist (run 'npm run build' in web/ for a fresh frontend)" >&2
  fi
fi

# CGO_ENABLED=0 produces a statically-linked binary that runs on OpenWrt's musl
# userland — the deployment target. A dynamic (glibc) build fails on-device with
# "can't execute '/usr/bin/meshd': No such file or directory" because its ELF
# interpreter is absent. The daemon is pure Go, so a static build has no cost.
# Overridable (CGO_ENABLED=1) for the rare case a dynamic host build is wanted.
CGO_ENABLED="${CGO_ENABLED:-0}" go build -o bin/meshd ./meshd/cmd/meshd

echo "Built bin/meshd"
