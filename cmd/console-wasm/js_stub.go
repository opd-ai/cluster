//go:build ignore

package main

// jsGlobal is a no-op stub kept for reference only.
// The package only targets js/wasm; js_wasm.go provides the real implementation.
func jsGlobal(_ string, def string) string { return def }
