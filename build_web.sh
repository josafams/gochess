#!/bin/sh
# Gera dist/: versão da interface que roda 100% no navegador (engine em WebAssembly).
# Sirva a pasta com qualquer servidor estático, ex.: python3 -m http.server -d dist
set -e
cd "$(dirname "$0")"
mkdir -p dist
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o dist/gochess.wasm .
WASM_EXEC="$(go env GOROOT)/lib/wasm/wasm_exec.js"
[ -f "$WASM_EXEC" ] || WASM_EXEC="$(go env GOROOT)/misc/wasm/wasm_exec.js"
cp "$WASM_EXEC" dist/wasm_exec.js
cp web/index.html web/style.css web/app.js web/worker.js dist/
echo "window.GOCHESS_BACKEND = 'wasm';" > dist/config.js
ls -la dist
