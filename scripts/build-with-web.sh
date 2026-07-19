#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO="$ROOT/.tools/go/bin/go"
export GOMODCACHE="$ROOT/.tools/gomodcache"
export GOCACHE="$ROOT/.tools/gocache"
export GOPATH="$ROOT/.tools/gopath"

cd "$ROOT"
npm run build

rm -rf server/internal/static/dist
cp -R dist server/internal/static/dist

mkdir -p server/bin
(cd server && "$GO" build -tags webdist -o bin/lumiod ./cmd/lumiod)

echo "built server/bin/lumiod with embedded frontend"
