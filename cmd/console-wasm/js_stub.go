//go:build !js || !wasm

package main

// jsGlobal is a no-op stub for non-WASM builds.
func jsGlobal(_ string, def string) string { return def }
