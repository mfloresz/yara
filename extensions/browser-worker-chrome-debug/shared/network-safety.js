// Redirects must be inspected before access, not after response bytes arrive.
export function createNetworkSafety(isPrivateFetchTarget) {
  async function safeWorkerFetch(url, options) {
    for (let hop = 0; hop < 10; hop++) {
      if (isPrivateFetchTarget(url)) throw new Error('Private worker target refused');
      const resp = await fetch(url, { ...options, redirect: 'manual' });
      if (resp.type === 'opaqueredirect') throw new Error('Redirect requires guarded tab navigation');
      if (resp.status >= 300 && resp.status < 400) {
        const location = resp.headers.get('location');
        if (!location) throw new Error('Redirect cannot be inspected safely');
        url = new URL(location, url).href;
        continue;
      }
      if (resp.redirected || isPrivateFetchTarget(resp.url || url)) throw new Error('Unexpected unsafe fetch redirect');
      return resp;
    }
    throw new Error('Worker redirect limit exceeded');
  }

  const idsFor = tabId => [0, 1, 2].map(n => 1000 + tabId * 3 + n);
  async function guardWorkerTab(tabId) {
    if (!chrome.declarativeNetRequest?.updateSessionRules) throw new Error('Safe tab navigation unavailable');
    const ids = idsFor(tabId);
    // Install on about:blank BEFORE navigation. Covers redirects and every
    // subrequest, including scripts/XHR. Block all IP literals conservatively;
    // DNS names stay supported (split horizon), not safe from DNS rebinding.
    await chrome.declarativeNetRequest.updateSessionRules({
      removeRuleIds: ids,
      addRules: [
        { id: ids[0], priority: 1, action: { type: 'block' }, condition: { tabIds: [tabId], regexFilter: '^' } },
        { id: ids[1], priority: 2, action: { type: 'allow' }, condition: { tabIds: [tabId], regexFilter: '^https?://' } },
        { id: ids[2], priority: 3, action: { type: 'block' }, condition: {
          tabIds: [tabId], regexFilter: '^https?://([^/@]*@)?(\\[[0-9a-f:.]+\\]|[0-9.]+|([^/:]+\\.)?(localhost|local|internal))\\.?(:[0-9]+)?(/|$)', isUrlFilterCaseSensitive: false,
        } },
      ],
    });
  }

  async function updateSafeWorkerTab(tabId, url) {
    if (isPrivateFetchTarget(url)) throw new Error('Private worker target refused');
    await guardWorkerTab(tabId);
    return chrome.tabs.update(tabId, { url, active: false });
  }

  async function createSafeWorkerTab(url) {
    if (isPrivateFetchTarget(url)) throw new Error('Private worker target refused');
    if (!chrome.declarativeNetRequest?.updateSessionRules) throw new Error('Safe tab navigation unavailable');
    const tab = await chrome.tabs.create({ url: 'about:blank', active: false });
    try { return await updateSafeWorkerTab(tab.id, url); }
    catch (e) { await chrome.tabs.remove(tab.id).catch(() => {}); throw e; }
  }

  // Session rules survive MV3 suspension. Remove closed-tab rules to keep the
  // quota bounded; reconcile stale rules when the worker starts again.
  chrome.tabs.onRemoved?.addListener(tabId => {
    chrome.declarativeNetRequest?.updateSessionRules({ removeRuleIds: idsFor(tabId) }).catch(() => {});
  });
  if (chrome.declarativeNetRequest?.getSessionRules) {
    Promise.all([chrome.declarativeNetRequest.getSessionRules(), chrome.tabs.query({})]).then(([rules, tabs]) => {
      const live = new Set(tabs.map(t => t.id));
      const removeRuleIds = rules.filter(r => r.id >= 1000 && !r.condition.tabIds?.some(id => live.has(id))).map(r => r.id);
      if (removeRuleIds.length) return chrome.declarativeNetRequest.updateSessionRules({ removeRuleIds });
    }).catch(() => {});
  }
  return { safeWorkerFetch, createSafeWorkerTab, updateSafeWorkerTab };
}
