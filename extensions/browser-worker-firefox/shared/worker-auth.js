// Authorization belongs to one privileged extension flow, not to any page
// whose path resembles a callback. Local storage survives MV3 suspension.
export function createWorkerAuthorization({ getConfig, yaraBaseUrl, onAuthenticated }) {
  const key = 'pendingWorkerAuth';
  let queue = Promise.resolve();
  const serial = action => {
    const result = queue.then(action);
    queue = result.catch(() => {});
    return result;
  };
  function beginWorkerAuth() {
    return serial(async () => {
      const config = await getConfig();
      const origin = yaraBaseUrl(config.serverAddr);
      const bytes = crypto.getRandomValues(new Uint8Array(32));
      const state = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
      const tab = await chrome.tabs.create({ url: 'about:blank' });
      try {
        await chrome.storage.local.set({ [key]: { state, origin, tabId: tab.id, expiresAt: Date.now() + 5 * 60 * 1000 } });
        const url = new URL('/api/v1/worker-auth/authorize', origin);
        url.searchParams.set('extension_id', chrome.runtime.id);
        url.searchParams.set('client_state', state);
        await chrome.tabs.update(tab.id, { url: url.href, active: true });
      } catch (e) {
        await chrome.storage.local.remove(key);
        await chrome.tabs.remove(tab.id).catch(() => {});
        throw e;
      }
    });
  }
  function handleTabUpdate(tabId, changeInfo, tab) {
    if (changeInfo.status !== 'complete') return;
    return serial(async () => {
      let url;
      try { url = new URL(tab.url); } catch { return; }
      if (url.pathname !== '/api/v1/worker-auth/callback') return;
      const { [key]: pending } = await chrome.storage.local.get(key);
      if (!pending) return;
      if (pending.expiresAt <= Date.now()) { await chrome.storage.local.remove(key); return; }
      const config = await getConfig();
      if (tabId !== pending.tabId || url.origin !== pending.origin ||
          yaraBaseUrl(config.serverAddr) !== pending.origin || url.username || url.password ||
          url.searchParams.getAll('client_state').length !== 1 || url.searchParams.get('client_state') !== pending.state ||
          url.searchParams.getAll('token').length !== 1 || url.searchParams.getAll('user').length !== 1) return;
      const token = url.searchParams.get('token');
      const userId = url.searchParams.get('user');
      if (!token || !userId) return;
      // Consume before storing credentials; serial execution prevents duplicate
      // complete events from racing the read/consume operation.
      await chrome.storage.local.remove(key);
      await chrome.storage.local.set({ workerToken: token, workerUserId: userId, workerConnectedAt: new Date().toISOString() });
      await chrome.tabs.remove(tabId).catch(() => {});
      // Never broadcast bearer credentials to other extension contexts.
      await chrome.runtime.sendMessage({ type: 'auth_complete' }).catch(() => {});
      await onAuthenticated();
    });
  }
  return { beginWorkerAuth, handleTabUpdate };
}
