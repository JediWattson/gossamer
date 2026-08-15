import React from "react";
import { flushSync } from "react-dom";
import { createRoot } from "react-dom/client";

globalThis.React = React;
globalThis.ReactDOM = { createRoot, flushSync };
