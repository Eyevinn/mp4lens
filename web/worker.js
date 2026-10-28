// Runs the mp4lens decoder (Go, compiled to WebAssembly) off the main thread. Files are read in slices
// with FileReaderSync, so a large file is never loaded into memory as a whole.
importScripts("wasm_exec.js");

const ready = (async () => {
  const go = new Go();
  let result;
  try {
    result = await WebAssembly.instantiateStreaming(fetch("mp4lens.wasm"), go.importObject);
  } catch (e) {
    const bytes = await (await fetch("mp4lens.wasm")).arrayBuffer();
    result = await WebAssembly.instantiate(bytes, go.importObject);
  }
  go.run(result.instance); // Sets self.mp4lensDecode, mp4lensSearch, mp4lensLayout and mp4lensSummary, then waits.
})();

ready.then(
  () => postMessage({ type: "ready" }),
  (e) => postMessage({ type: "fatal", message: String((e && e.message) || e) })
);

// decoded identifies the file and levels of the tree that the decoder holds, which searches use.
let decoded = null;

onmessage = async (e) => {
  const m = e.data;
  try {
    await ready;
    const t0 = performance.now();
    const id = m.fileId + "|" + m.levels;
    let json;
    if (m.type === "decode" || decoded !== id) {
      json = self.mp4lensDecode(m.source, m.levels);
      decoded = id;
    }
    if (m.type === "search") json = self.mp4lensSearch(m.query);
    if (m.type === "layout") json = self.mp4lensLayout(m.key);
    if (m.type === "summary") json = self.mp4lensSummary();
    postMessage({ type: "result", id: m.id, json, ms: performance.now() - t0 });
  } catch (err) {
    postMessage({ type: "result", id: m.id, error: String((err && err.message) || err) });
  }
};
