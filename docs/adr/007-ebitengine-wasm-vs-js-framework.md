# ADR 007 — Web Console: Ebitengine WASM vs SvelteKit / Next.js

**Status:** Accepted  
**Date:** 2026-06-01

## Context

The cluster needs a web console for interacting with the gateway, viewing
cluster status, and monitoring running jobs. Two architectural choices were
evaluated: an Ebitengine WASM binary compiled from Go, and a JavaScript
framework (SvelteKit or Next.js).

## Decision

**Ebitengine WASM** — a Go game-engine binary compiled to WebAssembly.

## Rationale

### Ebitengine WASM (chosen)

- **Zero JavaScript dependencies**: the entire frontend is Go code; no
  `node_modules`, no npm audit, no JS supply-chain attack surface.
- **Single binary**: the console is one `.wasm` file served from the Go HTTP
  server. No build toolchain required at runtime.
- **Reproducibility**: `GOOS=js GOARCH=wasm go build` produces a byte-for-byte
  identical binary given the same Go toolchain version (see
  `docs/MODEL-REBUILD-GUARANTEE.md`).
- **Consistency**: the same Go code that runs on the cluster nodes runs in the
  browser — shared types, shared validation logic.
- **Ebitengine** provides a stable 2D rendering loop (60 fps) backed by
  WebGL2 / Canvas 2D; suitable for a terminal-style or dashboard UI.

### SvelteKit / Next.js (rejected)

Rejected primarily due to supply-chain risk:

- A typical SvelteKit project has 500–1 000 transitive npm dependencies.
  Each dependency is a potential vector for a `left-pad` or `event-stream`
  style supply-chain attack.
- npm audit is reactive, not preventive; a compromised package can execute
  at install time (`postinstall` hooks).
- The project's threat model prioritises auditability and minimal attack
  surface over ecosystem breadth.

### Accessibility tradeoff

The canvas-based Ebitengine approach renders pixels directly and does not
produce DOM elements. This means:

- Screen readers cannot access UI elements.
- Keyboard navigation must be implemented manually (it is not inherited from
  HTML form elements).
- The console is therefore **not accessible** to users who rely on assistive
  technology.

**Mitigation**: the REST API (gateway, RAG) is the primary interface and is
fully accessible via any HTTP client. The console is a convenience tool, not
a required interface. Operators with accessibility requirements should use
the REST API directly or build a DOM-based wrapper.

This tradeoff was accepted consciously as of Phase 8.14 of the project plan.

## Consequences

- `cmd/console-wasm` is compiled with `GOOS=js GOARCH=wasm`.
- `cmd/console` is a Go HTTP server that serves `index.html`,
  `wasm_exec.js`, and `console.wasm`.
- The JS supply-chain attack surface is limited to `wasm_exec.js` from the
  Go standard library distribution.
- The console is not accessible to screen reader users; this is documented
  as a known limitation.
- If accessibility becomes a hard requirement, a parallel DOM-based UI can
  be added without removing the Ebitengine console.
