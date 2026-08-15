#!/usr/bin/env bash

set -euo pipefail

GOSSAMER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FIXTURE_ROOT="${GOSSAMER_ROOT}/tools/v8/react-fixture"

npm install --prefix "${FIXTURE_ROOT}" --no-audit --no-fund
"${FIXTURE_ROOT}/node_modules/.bin/esbuild" \
  "${FIXTURE_ROOT}/entry.js" \
  --bundle \
  --format=iife \
  --platform=browser \
  --target=es2020 \
  --minify \
  --legal-comments=inline \
  --define:process.env.NODE_ENV='"production"' \
  --outfile="${GOSSAMER_ROOT}/internal/v8engine/testdata/react-19.2.7.production.js"
