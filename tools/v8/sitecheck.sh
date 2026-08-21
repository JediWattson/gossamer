#!/usr/bin/env bash

set -euo pipefail

GOSSAMER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
V8_OUTPUT_DIR="${GOSSAMER_ROOT}/third_party/v8/v8/out.gn/gossamer-profile"

if [[ ! -f "${V8_OUTPUT_DIR}/libgossamer_v8_bridge.dylib" || ! -f "${V8_OUTPUT_DIR}/icudtl.dat" ]]; then
  echo "Stock V8 is not built; run tools/v8/bootstrap.sh and tools/v8/build.sh first." >&2
  exit 1
fi

export GOSSAMER_V8_ICU_DATA="${V8_OUTPUT_DIR}/icudtl.dat"
cd "${GOSSAMER_ROOT}"
go run -tags=v8 ./cmd/gossamer-sitecheck "$@"
