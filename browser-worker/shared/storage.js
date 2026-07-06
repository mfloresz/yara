const STORAGE_KEY = 'yara_browser_worker';
const WORKER_TOKEN_KEY = 'workerToken';
const WORKER_USER_KEY = 'workerUserId';
const WORKER_CONNECTED_KEY = 'workerConnectedAt';

const defaults = {
  serverAddr: 'localhost:5176',
  autoConnect: true,
  heartbeatInterval: 5000,
};

export async function getConfig() {
  const result = await chrome.storage.local.get(STORAGE_KEY);
  const stored = result[STORAGE_KEY] || {};
  // Migrate old ws:// URL format to new host:port format
  if (stored.serverUrl && !stored.serverAddr) {
    try {
      const u = new URL(stored.serverUrl);
      stored.serverAddr = u.host;
    } catch {
      stored.serverAddr = defaults.serverAddr;
    }
    delete stored.serverUrl;
  }
  return { ...defaults, ...stored };
}

export async function setConfig(patch) {
  const current = await getConfig();
  const merged = { ...current, ...patch };
  await chrome.storage.local.set({ [STORAGE_KEY]: merged });
  return merged;
}

export async function getWorkerToken() {
  const result = await chrome.storage.local.get([WORKER_TOKEN_KEY, WORKER_USER_KEY, WORKER_CONNECTED_KEY]);
  return {
    token: result[WORKER_TOKEN_KEY] || null,
    userId: result[WORKER_USER_KEY] || null,
    connectedAt: result[WORKER_CONNECTED_KEY] || null,
  };
}

export async function setWorkerToken(token, userId) {
  await chrome.storage.local.set({
    [WORKER_TOKEN_KEY]: token,
    [WORKER_USER_KEY]: userId,
    [WORKER_CONNECTED_KEY]: new Date().toISOString(),
  });
}

export async function clearWorkerToken() {
  await chrome.storage.local.remove([WORKER_TOKEN_KEY, WORKER_USER_KEY, WORKER_CONNECTED_KEY]);
}
