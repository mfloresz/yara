// Firefox-compatible fallback for Manifest V3 background scripts.
// The shared implementation remains an ES module so Chrome and Firefox use
// the same proxy logic. Firefox loads this classic background script and then
// imports the module explicitly.
void import(chrome.runtime.getURL('background/service-worker.js')).catch((error) => {
  console.error('[DebugWorker] Failed to load background module:', error);
});
