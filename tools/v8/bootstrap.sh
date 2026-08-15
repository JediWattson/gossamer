#!/usr/bin/env bash

set -euo pipefail

GOSSAMER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=VERSION
source "${GOSSAMER_ROOT}/tools/v8/VERSION"

V8_WORKSPACE="${GOSSAMER_ROOT}/third_party/v8"
DEPOT_TOOLS_DIR="${V8_WORKSPACE}/depot_tools"
V8_SOURCE_DIR="${V8_WORKSPACE}/v8"

mkdir -p "${V8_WORKSPACE}"

if [[ ! -d "${DEPOT_TOOLS_DIR}/.git" ]]; then
  git clone https://chromium.googlesource.com/chromium/tools/depot_tools.git "${DEPOT_TOOLS_DIR}"
fi

git -C "${DEPOT_TOOLS_DIR}" fetch origin "${DEPOT_TOOLS_REVISION}" --depth=1
git -C "${DEPOT_TOOLS_DIR}" checkout --detach "${DEPOT_TOOLS_REVISION}"
"${DEPOT_TOOLS_DIR}/ensure_bootstrap"

export DEPOT_TOOLS_UPDATE=0
export PATH="${DEPOT_TOOLS_DIR}:${PATH}"

if [[ ! -d "${V8_SOURCE_DIR}/.git" ]]; then
  (
    cd "${V8_WORKSPACE}"
    fetch --no-history v8
  )
fi

git -C "${V8_SOURCE_DIR}" fetch origin "${V8_REVISION}" --depth=1
git -C "${V8_SOURCE_DIR}" checkout --detach "${V8_REVISION}"

(
  cd "${V8_WORKSPACE}"
  gclient sync --delete_unversioned_trees --revision "v8@${V8_REVISION}"
)

actual_revision="$(git -C "${V8_SOURCE_DIR}" rev-parse HEAD)"
if [[ "${actual_revision}" != "${V8_REVISION}" ]]; then
  echo "V8 revision mismatch: got ${actual_revision}, want ${V8_REVISION}" >&2
  exit 1
fi

echo "Stock V8 ${V8_BRANCH} is ready at ${V8_REVISION}."
