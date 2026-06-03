//go:build !js || !wasm

package main

// jsGlobal is a no-op stub for non-js/wasm builds.
// js_wasm.go provides the real implementation when targeting js/wasm.
func jsGlobal(_ string, def string) string { return def }

func main() {}
