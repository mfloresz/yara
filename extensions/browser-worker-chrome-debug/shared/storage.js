const STORAGE_KEY = 'yara_browser_worker_debug';

const defaults = {
  serverAddr: 'localhost:5177',
  autoConnect: true,
};

export async function getConfig() {
  const result = await chrome.storage.local.get(STORAGE_KEY);
  const stored = result[STORAGE_KEY] || {};
  return { ...defaults, ...stored };
}

export async function setConfig(patch) {
  const current = await getConfig();
  const merged = { ...current, ...patch };
  await chrome.storage.local.set({ [STORAGE_KEY]: merged });
  return merged;
}
