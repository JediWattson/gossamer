#!/usr/bin/env bash

set -euo pipefail

GOSSAMER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=VERSION
source "${GOSSAMER_ROOT}/tools/v8/VERSION"

V8_WORKSPACE="${GOSSAMER_ROOT}/third_party/v8"
DEPOT_TOOLS_DIR="${V8_WORKSPACE}/depot_tools"
V8_SOURCE_DIR="${V8_WORKSPACE}/v8"
V8_OUTPUT_RELATIVE="out.gn/gossamer-profile"
V8_OUTPUT_DIR="${V8_SOURCE_DIR}/${V8_OUTPUT_RELATIVE}"
GN_BINARY="${V8_SOURCE_DIR}/buildtools/mac/gn"
NINJA_BINARY="${V8_SOURCE_DIR}/third_party/ninja/ninja"
BRIDGE_OVERLAY="${V8_SOURCE_DIR}/gossamer_bridge"

if [[ ! -d "${V8_SOURCE_DIR}/.git" ]]; then
  echo "V8 is not bootstrapped; run tools/v8/bootstrap.sh first." >&2
  exit 1
fi

actual_revision="$(git -C "${V8_SOURCE_DIR}" rev-parse HEAD)"
if [[ "${actual_revision}" != "${V8_REVISION}" ]]; then
  echo "V8 revision mismatch: got ${actual_revision}, want ${V8_REVISION}" >&2
  exit 1
fi

export DEPOT_TOOLS_UPDATE=0
export PATH="${DEPOT_TOOLS_DIR}:${PATH}"
export VPYTHON_ROOT="${TMPDIR:-/tmp}/gossamer-vpython-root"
"${DEPOT_TOOLS_DIR}/ensure_bootstrap"

mkdir -p "${BRIDGE_OVERLAY}"
copy_if_changed() {
  local source="$1"
  local destination="$2"
  if [[ ! -f "${destination}" ]] || ! cmp -s "${source}" "${destination}"; then
    cp "${source}" "${destination}"
  fi
}
copy_if_changed "${GOSSAMER_ROOT}/tools/v8/bridge/BUILD.gn" "${BRIDGE_OVERLAY}/BUILD.gn"
copy_if_changed "${GOSSAMER_ROOT}/tools/v8/bridge/bridge.cc" "${BRIDGE_OVERLAY}/bridge.cc"
copy_if_changed "${GOSSAMER_ROOT}/internal/v8engine/bridge.h" "${BRIDGE_OVERLAY}/bridge.h"

(
cd "${V8_SOURCE_DIR}"
"${GN_BINARY}" gen "${V8_OUTPUT_RELATIVE}" --root-target=//gossamer_bridge:gossamer_profile --args='is_debug=false
target_cpu="arm64"
v8_target_cpu="arm64"
is_component_build=false
v8_monolithic=true
v8_use_external_startup_data=false
v8_enable_i18n_support=true
v8_enable_sandbox=true
symbol_level=2
v8_symbol_level=2
use_custom_libcxx=true
treat_warnings_as_errors=false'

"${NINJA_BINARY}" -C "${V8_OUTPUT_RELATIVE}" gossamer_bridge:gossamer_profile
)

library="${V8_OUTPUT_DIR}/obj/libv8_monolith.a"
if [[ ! -f "${library}" ]]; then
  echo "V8 build did not produce ${library}" >&2
  exit 1
fi

bridge_library="${V8_OUTPUT_DIR}/libgossamer_v8_bridge.dylib"
if [[ ! -f "${bridge_library}" ]]; then
  echo "V8 build did not produce ${bridge_library}" >&2
  exit 1
fi
install_name_tool -id "@rpath/libgossamer_v8_bridge.dylib" "${bridge_library}"

echo "Built stock V8 ${V8_BRANCH} (${V8_REVISION})."
echo "Library: ${library}"
echo "Bridge: ${bridge_library}"
if [[ -f "${V8_OUTPUT_DIR}/icudtl.dat" ]]; then
  echo "ICU data: ${V8_OUTPUT_DIR}/icudtl.dat"
fi
