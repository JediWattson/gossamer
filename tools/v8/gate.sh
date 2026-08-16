#!/usr/bin/env bash

set -euo pipefail

GOSSAMER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${GOSSAMER_ROOT}"

printf 'Gossamer gate: native DOM, transfer, and form slices\n'
go test ./internal/dom ./internal/browser ./internal/runtime ./internal/runtime/memory \
  -run 'Test(FormValidity|FormRequestSubmit|PostFormSubmission|PageGeometry|PagePostMessage|CrossRealmStructuredClone|StructuredClone|NavigationScriptSequence|StaticModuleSpecifiers|ResolveModuleSpecifier)' \
  -count=1

printf 'Gossamer gate: race-enabled ownership slices\n'
go test -race ./internal/browser ./internal/runtime ./internal/runtime/memory \
  -run 'Test(FormRequestSubmit|PostFormSubmission|PageGeometry|PagePostMessage|CrossRealmStructuredClone|StructuredClone|NavigationScriptSequence|StaticModuleSpecifiers|ResolveModuleSpecifier)' \
  -count=1

printf 'Gossamer gate: stock V8 conformance, React, GC, and replacement stress\n'
"${GOSSAMER_ROOT}/tools/v8/test.sh" \
  -run 'TestStockV8(EvaluationMicrotasksProfilingAndTeardown|FocusedWebPlatformSubsets|FormSubmissionNavigatesAndTearsDownOldRealm|ReactDOMCompatibilityGate|BootsHTTPViteShapedProductionReactPage|CSSOMViewRootScrollAndReactMeasurement|AnimationFramesPerformanceClockAndViewportResize|ResizeAndIntersectionObserversRetainTargetsAndTeardown|ReactPositionedLayoutStackingAndFixedGeometry|ReactFlexLayoutGeometryAndDelegatedHitTesting|GraphiteShellRoutesNativeInputAndTeardown|HistoryTraversalReplacesRealmsAndInvalidatesWrappers|ReplacementSubmissionLeakAndPerformanceGate)' \
  -count=1 \
  -timeout=60s

printf 'Gossamer conformance gate passed\n'
