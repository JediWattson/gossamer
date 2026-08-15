# Stock-V8 framework fixture

`react-19.2.7.production.js` is a browser IIFE built from the exact React,
ReactDOM, and esbuild versions in `tools/v8/react-fixture/package.json`. It
exports only `React`, `ReactDOM.createRoot`, and `ReactDOM.flushSync` to the
test Realm.

Regenerate it from the repository root with:

```sh
./tools/v8/build-react-fixture.sh
```

The generated code is test-only and is covered by `react-LICENSE`.
