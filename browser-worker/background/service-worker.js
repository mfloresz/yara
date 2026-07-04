import { MessageType, JobStatus, WorkerState, createMessage, parseMessage } from '../shared/protocol.js';
import { getConfig, setConfig, getWorkerToken } from '../shared/storage.js';

let ws = null;
let state = WorkerState.DISCONNECTED;
let heartbeatTimer = null;
let reconnectTimer = null;
let reconnectDelay = 1000;
const MAX_RECONNECT_DELAY = 30000;

// ── Challenge tab management ───────────────────────────────────────
// We reuse a single hidden tab for Cloudflare challenges. Most fetch_page
// requests are served by background fetch() (which inherits the cf_clearance
// cookie once solved). Only when fetch() returns a challenge page do we
// open / reuse the challenge tab.
let challengeTabId = null;
let challengeOrigin = null; // e.g. "https://www.69shuba.com"

const log = (msg, ...args) => console.log(`[BrowserWorker] ${msg}`, ...args);
const warn = (msg, ...args) => console.warn(`[BrowserWorker] ${msg}`, ...args);
const err = (msg, ...args) => console.error(`[BrowserWorker] ${msg}`, ...args);

async function init() {
  log('Initializing...');
  const config = await getConfig();
  if (config.autoConnect) connect();
  chrome.runtime.onMessage.addListener(handleInternalMessage);
}

function handleInternalMessage(msg, sender, sendResponse) {
  if (msg.type === 'CONNECT') {
    connect().then(() => sendResponse({ ok: true })).catch(e => sendResponse({ ok: false, error: e.message }));
    return true;
  }
  if (msg.type === 'DISCONNECT') {
    disconnect();
    sendResponse({ ok: true });
    return false;
  }
  if (msg.type === 'GET_STATE') {
    sendResponse({ state, connected: ws?.readyState === WebSocket.OPEN });
    return false;
  }
  if (msg.type === 'UPDATE_CONFIG') {
    setConfig(msg.config).then(() => {
      if (msg.config.serverAddr) {
        disconnect();
        connect();
      }
      sendResponse({ ok: true });
    });
    return true;
  }
  if (msg.type === 'auth_complete') {
    log('Auth complete, reconnecting with token...');
    disconnect();
    connect();
    return false;
  }
}

async function connect() {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return;

  const config = await getConfig();
  const tokenData = await getWorkerToken();
  
  if (!tokenData.token) {
    warn('No worker token found, setting state to unauthenticated');
    setState(WorkerState.UNAUTHENTICATED);
    return;
  }

  const wsUrl = `ws://${config.serverAddr}/ws/browser-worker`;
  log('Connecting to:', wsUrl);
  setState(WorkerState.CONNECTING);

  try {
    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      log('WebSocket connected');
      setState(WorkerState.CONNECTED);
      reconnectDelay = 1000;
      sendRegister(tokenData.token);
      startHeartbeat(config.heartbeatInterval);
      broadcastState();
    };

    ws.onmessage = (event) => {
      const msg = parseMessage(event.data);
      if (!msg) return;
      handleServerMessage(msg);
    };

    ws.onclose = (event) => {
      log('WebSocket closed:', event.code);
      setState(WorkerState.DISCONNECTED);
      stopHeartbeat();
      broadcastState();
      if (!event.wasClean) scheduleReconnect();
    };

    ws.onerror = (error) => err('WebSocket error:', error);
  } catch (e) {
    err('Connection failed:', e);
    setState(WorkerState.DISCONNECTED);
    scheduleReconnect();
  }
}

function disconnect() {
  if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
  stopHeartbeat();
  if (ws) { ws.close(1000, 'User disconnect'); ws = null; }
  setState(WorkerState.DISCONNECTED);
  broadcastState();
}

function scheduleReconnect() {
  if (reconnectTimer) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, reconnectDelay);
  reconnectDelay = Math.min(reconnectDelay * 1.5, MAX_RECONNECT_DELAY);
}

function startHeartbeat(interval) {
  stopHeartbeat();
  heartbeatTimer = setInterval(() => {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(createMessage(MessageType.HEARTBEAT, { state, timestamp: Date.now() }));
    }
  }, interval || 5000);
}

function stopHeartbeat() {
  if (heartbeatTimer) { clearInterval(heartbeatTimer); heartbeatTimer = null; }
}

function sendRegister(token) {
  if (ws?.readyState !== WebSocket.OPEN) return;
  const ua = navigator.userAgent;
  let browser = 'chrome';
  if (ua.includes('Firefox')) browser = 'firefox';
  else if (ua.includes('Edg/')) browser = 'edge';
  ws.send(createMessage(MessageType.REGISTER, {
    browser: { name: browser, userAgent: ua },
    capabilities: ['cookies', 'dom', 'javascript'],
    version: '1.0.0',
    token: token,
  }));
}

async function handleServerMessage(msg) {
  switch (msg.type) {
    case MessageType.JOB_REQUEST:
      await handleJobRequest(msg.payload);
      break;
    case MessageType.PING:
      ws.send(createMessage(MessageType.PONG, { timestamp: Date.now() }));
      break;
    case MessageType.CANCEL_JOB:
      break;
  }
}

// ── Job handler ────────────────────────────────────────────────────
async function handleJobRequest(payload) {
  const { jobId, url, params } = payload;
  log(`Job ${jobId}: fetch_page ${url}`);
  setState(WorkerState.DOWNLOADING);

  try {
    const result = await fetchRawPage(url, params);
    sendJobResult(jobId, JobStatus.OK, result);
  } catch (e) {
    err(`Job ${jobId} failed:`, e);
    const isChallenge = e.message?.includes('challenge') || e.message?.includes('captcha');
    sendJobResult(jobId, isChallenge ? JobStatus.CHALLENGE : JobStatus.ERROR, { error: e.message });
  } finally {
    setState(WorkerState.CONNECTED);
  }
}

function sendJobResult(jobId, status, data) {
  if (ws?.readyState !== WebSocket.OPEN) return;
  ws.send(createMessage(MessageType.JOB_RESULT, { jobId, status, data }));
}

// ── Core proxy logic ───────────────────────────────────────────────
// Strategy:
//   1. Try background fetch() first (shares cookies, incl. cf_clearance).
//   2. If the response is a Cloudflare challenge page, fall back to a
//      dedicated challenge tab where the user can solve it once.
//   3. Subsequent requests to the same origin use background fetch()
//      since cf_clearance is now valid — no tab navigation needed.
async function fetchRawPage(url, params = {}) {
  const maxWait = (params.timeout || 120) * 1000;

  // ── Phase 1: background fetch() ──────────────────────────────────
  const bgResult = await tryBackgroundFetch(url);
  if (bgResult) return bgResult;

  // ── Phase 2: challenge page fallback ─────────────────────────────
  log('Background fetch hit Cloudflare challenge, using tab...');
  return fetchViaChallengeTab(url, maxWait);
}

// ── Background fetch ───────────────────────────────────────────────
// Uses the extension's own fetch() which inherits browser cookies,
// including cf_clearance from previously solved Cloudflare challenges.
// Returns null if the page is a Cloudflare challenge.
async function tryBackgroundFetch(url) {
  try {
    const resp = await fetch(url, {
      credentials: 'include',
      headers: {
        'User-Agent': navigator.userAgent,
        'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8',
        'Accept-Language': 'zh-CN,zh;q=0.9,en;q=0.8',
      },
    });

    if (!resp.ok) {
      log(`Background fetch returned ${resp.status} for ${url}`);
      return null;
    }

    const html = await resp.text();
    const lowerHtml = html.toLowerCase();

    // Check for Cloudflare / challenge indicators
    const challengeIndicators = [
      'just a moment', 'checking your browser', 'verifying you are human',
      'cf-challenge', 'challenge-platform', 'turnstile',
      'attention required', 'access denied',
    ];
    for (const indicator of challengeIndicators) {
      if (lowerHtml.includes(indicator)) {
        log(`Background fetch: challenge detected via "${indicator}" for ${url}`);
        return null;
      }
    }

    log(`Background fetch succeeded for ${url} (${html.length} bytes)`);

    // Extract title for the result object
    let title = '';
    const titleMatch = html.match(/<title[^>]*>([^<]*)<\/title>/i);
    if (titleMatch) title = titleMatch[1];

    return { html, text: '', title, url };
  } catch (e) {
    log(`Background fetch error for ${url}:`, e);
    return null;
  }
}

// ── Challenge tab ──────────────────────────────────────────────────
// Opens (or reuses) a hidden tab for the site's origin when Cloudflare
// needs user interaction. After the challenge is solved, the tab stays
// open so the cf_clearance cookie remains active.
async function fetchViaChallengeTab(url, maxWait) {
  const parsedUrl = new URL(url);
  const origin = parsedUrl.origin;
  const startTime = Date.now();

  // If we already have a challenge tab for this origin, navigate it.
  // Otherwise create a new one.
  let tab;
  if (challengeTabId !== null && challengeOrigin === origin) {
    try {
      tab = await chrome.tabs.get(challengeTabId);
      await chrome.tabs.update(tab.id, { url, active: false });
    } catch {
      // Tab was closed, create a new one
      challengeTabId = null;
      challengeOrigin = null;
      tab = await chrome.tabs.create({ url, active: false });
      challengeTabId = tab.id;
      challengeOrigin = origin;
    }
  } else {
    // If we have a challenge tab for a different origin, close it first
    if (challengeTabId !== null) {
      try { await chrome.tabs.remove(challengeTabId); } catch { /* ignore */ }
    }
    tab = await chrome.tabs.create({ url, active: false });
    challengeTabId = tab.id;
    challengeOrigin = origin;
  }

  log('Waiting for page load on challenge tab (max', maxWait / 1000, 's)...');

  while (Date.now() - startTime < maxWait) {
    try {
      const tabInfo = await chrome.tabs.get(tab.id);
      if (tabInfo.status === 'complete') {
        const isChallenge = await checkForChallenge(tab.id);
        if (isChallenge) {
          log('Cloudflare challenge detected, waiting for user to solve it...');
          chrome.runtime.sendMessage({ type: 'CHALLENGE_DETECTED', url, tabId: tab.id }).catch(() => {});
          await sleep(3000);
          continue;
        }
        log('Challenge solved, extracting HTML...');
        const results = await chrome.scripting.executeScript({
          target: { tabId: tab.id },
          func: () => ({
            html: document.documentElement.outerHTML,
            text: document.body.innerText,
            title: document.title,
            url: window.location.href,
          }),
        });
        return results[0]?.result || { html: '', text: '', title: '', url };
      }
      await sleep(500);
    } catch (e) {
      err('Error checking challenge tab:', e);
      await sleep(1000);
    }
  }
  throw new Error('Timeout waiting for Cloudflare challenge to be solved.');
}

// ── Challenge detection ────────────────────────────────────────────
async function checkForChallenge(tabId) {
  try {
    const results = await chrome.scripting.executeScript({
      target: { tabId },
      func: () => {
        const t = (document.title || '').toLowerCase();
        const b = (document.body?.innerText || '').toLowerCase();
        const indicators = [
          'just a moment', 'checking your browser', 'verifying you are human',
          'challenge', 'cloudflare', 'cf-challenge', 'ray id',
          'attention required', 'access denied',
        ];
        if (indicators.some(i => t.includes(i) || b.includes(i))) return true;
        if (document.querySelector('script[src*="challenge"], script[src*="turnstile"]')) return true;
        if (document.querySelector('form[action*="challenge"]')) return true;
        return false;
      },
    });
    return results[0]?.result || false;
  } catch {
    return false;
  }
}

function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

function setState(newState) {
  if (state !== newState) { state = newState; broadcastState(); }
}

function broadcastState() {
  chrome.runtime.sendMessage({ type: 'STATE_CHANGED', state }).catch(() => {});
}

init();
