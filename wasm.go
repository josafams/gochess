//go:build js && wasm

package main

import (
	"encoding/json"
	"syscall/js"
)

// No navegador, a engine roda dentro de um Web Worker e expõe
// gochessCall(jsonRequest) -> jsonResponse.
func main() {
	apiHashMB = 32
	js.Global().Set("gochessCall", js.FuncOf(func(this js.Value, args []js.Value) any {
		var req APIRequest
		if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
			b, _ := json.Marshal(APIResponse{Error: err.Error()})
			return string(b)
		}
		b, _ := json.Marshal(HandleAPI(req))
		return string(b)
	}))
	if ready := js.Global().Get("gochessReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}
