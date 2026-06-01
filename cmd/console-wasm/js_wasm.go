//go:build js && wasm

package main

import "syscall/js"

// jsGlobal reads a string property from the JS global object (window).
// Returns def if the property does not exist or is not a string.
func jsGlobal(name, def string) string {
	v := js.Global().Get(name)
	if v.IsUndefined() || v.IsNull() {
		return def
	}
	return v.String()
}
