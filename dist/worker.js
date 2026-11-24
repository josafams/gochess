// Roda a engine (WebAssembly) fora da thread da interface,
// para o tabuleiro não travar enquanto ela pensa.
importScripts('wasm_exec.js');

let ready = false;
const queue = [];

self.gochessReady = () => {
  ready = true;
  postMessage({ type: 'ready' });
  for (const d of queue.splice(0)) handle(d);
};

function handle(d) {
  const out = self.gochessCall(JSON.stringify(d.req));
  postMessage({ type: 'resp', id: d.id, resp: JSON.parse(out) });
}

self.onmessage = e => (ready ? handle(e.data) : queue.push(e.data));

(async () => {
  try {
    const go = new Go();
    const bytes = await (await fetch('gochess.wasm')).arrayBuffer();
    const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
    go.run(instance);
  } catch (err) {
    postMessage({ type: 'error', message: String(err && err.message || err) });
  }
})();
