# Runbook: Debug a Frozen WASM Canvas

Use this runbook when the Ebitengine console WASM canvas stops responding
in the browser.

## Symptoms

- The `<canvas>` element is visible but not updating (frozen frame)
- No WebSocket messages in browser DevTools
- Browser tab may show high CPU or may be idle

---

## Step 1: Check browser DevTools

Open DevTools (F12) → Console tab:

```
# Look for errors like:
# "WebAssembly.instantiate: ...out of memory"
# "WebSocket connection to ws://... failed"
# Uncaught RuntimeError: unreachable (wasm crash)
```

If you see a WebAssembly crash, reload the tab (`F5`). WASM console state
is stateless — the WebSocket reconnects automatically.

## Step 2: Check the console server pod

```bash
kubectl -n ai-cluster logs deployment/console --tail=50
# Look for: "panic", "websocket: close", "write: broken pipe"
```

If the server panicked:

```bash
kubectl -n ai-cluster rollout restart deployment/console
```

## Step 3: Check the WebSocket proxy

The console server proxies WebSocket messages to the Gateway. If the Gateway
is overloaded, the WS proxy may time out.

```bash
kubectl -n ai-cluster logs deployment/gateway --tail=50 | grep -i "websocket\|ws"
```

## Step 4: Check Ebitengine frame rate

In the browser DevTools → Performance tab, record 5 seconds:

- If `requestAnimationFrame` is firing but canvas is not updating: Ebitengine
  game loop is running but the WASM is blocked on a syscall (likely a large
  embedding or LLM call). Wait for the call to complete or reload.
- If `requestAnimationFrame` has stopped: browser throttled the tab (background
  tabs are throttled to 1 fps). Bring the tab to the foreground.

## Step 5: Rebuild the WASM binary

If the issue persists across reloads and restarts, rebuild and redeploy:

```bash
GOOS=js GOARCH=wasm GOTOOLCHAIN=local \
  go build -o dist/js-wasm/console.wasm ./cmd/console-wasm

# Push to the cluster's registry
docker build -t ghcr.io/opd-ai/cluster/console:latest .
docker push ghcr.io/opd-ai/cluster/console:latest
kubectl -n ai-cluster rollout restart deployment/console
```

## Known limitations

- The `<canvas>` element is not accessible to screen readers (see ADR 007).
  Users requiring accessibility should use the REST API directly.
- WASM binaries larger than ~40 MB may fail to instantiate on low-memory
  mobile devices. Check `dist/js-wasm/console.wasm` size with `make build`.
