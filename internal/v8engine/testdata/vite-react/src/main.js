import {App} from "./App.js";

globalThis.__gossamerModuleRuns = (globalThis.__gossamerModuleRuns || 0) + 1;
globalThis.__gossamerBootOrder.push("module:" + document.readyState);
globalThis.__gossamerReactRoot = ReactDOM.createRoot(document.getElementById("root"));
ReactDOM.flushSync(() => __gossamerReactRoot.render(React.createElement(App)));
