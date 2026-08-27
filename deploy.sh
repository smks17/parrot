#!/usr/bin/env bash
# Rebuilds and deploys Parrot end to end: WASM engine, Cloudflare Worker
# (backend), then the static Next.js frontend to Cloudflare Pages.
#
# Requires: Go toolchain, Node 22+, and `npx wrangler login` already done.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PAGES_PROJECT="${PAGES_PROJECT:-parrot}"
WORKER_NAME="${WORKER_NAME:-parrot-api}"

echo "== 1/4  Building engine.wasm =="
cd "$ROOT"
GOOS=js GOARCH=wasm go build -o web/public/engine.wasm ./cmd/wasm

echo "== 2/4  Deploying backend ($WORKER_NAME) =="
cd "$ROOT/backend"
npx wrangler deploy --name "$WORKER_NAME"

echo "== 3/4  Building frontend static export =="
cd "$ROOT/web"
npm run build

echo "== 4/4  Deploying frontend to Cloudflare Pages ($PAGES_PROJECT) =="
npx wrangler pages deploy out --project-name "$PAGES_PROJECT"

echo "== Done =="
