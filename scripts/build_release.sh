#!/usr/bin/env bash
set -euo pipefail

# Set GOARCH=arm64 if your droplet is ARM; default amd64
: "${GOOS:=linux}"
: "${GOARCH:=amd64}"

REV="$(git rev-parse --short HEAD)"
STAMP="$(date -u +%Y%m%d%H%M%S)"
PKG="traininglog-${STAMP}-${REV}"

rm -rf .release
mkdir -p ".release/${PKG}/web/templates" ".release/${PKG}/web/static/dist"

# CSS (minified)
./tailwindcss -i web/static/src/input.css -o web/static/dist/app.css --minify

# Copy assets
cp -r web/templates/* ".release/${PKG}/web/templates/"
cp -r web/static/dist/* ".release/${PKG}/web/static/dist/"

# Build Linux binary
CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags="-s -w" -o ".release/${PKG}/traininglog" ./cmd/server

# Package
tar -C .release -czf "${PKG}.tar.gz" "${PKG}"
echo "${PKG}.tar.gz"
