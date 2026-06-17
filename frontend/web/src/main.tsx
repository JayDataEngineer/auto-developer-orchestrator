import React from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app";
import "./index.css";

// ── HACK: Disable React Compiler runtime ──
// React 19.2+ ships React.__COMPILER_RUNTIME.c which calls
// resolveDispatcher().useMemoCache(size).  @assistant-ui/tap's own compiler
// runtime (the `c()` function in react-shim/compiler-runtime.js) checks for
// this global and prefers it over the fallback cPolyfill that uses plain
// useMemo().  Unfortunately React's useMemoCache is not properly tracked as
// a first-class hook by the dispatcher in 19.2.x, causing React to detect a
// change in the order of hooks every re-render of any TAP-compiled component
// (ThreadPrimitive.ViewportScrollable → useTopAnchorTurn → useAuiEvent …).
// Clearing __COMPILER_RUNTIME forces @assistant-ui/tap to use cPolyfill
// everywhere, which relies on standard useMemo() and keeps the hook order
// stable.
(React as any).__COMPILER_RUNTIME = undefined;

createRoot(document.getElementById("root")!).render(
	<App />
);
