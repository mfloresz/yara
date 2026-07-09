import { MessageType, JobStatus, WorkerState, createMessage, parseMessage } from '../shared/protocol.js';
import { getConfig, setConfig } from '../shared/storage.js';

let ws = null;
let state = WorkerState.DISCONNECTED;
let reconnectTimer = null;
let reconnectDelay = 1000;
const MAX_RECONNECT_DELAY = 30000;

let challengeTabId = null;
let challengeOrigin = null;

const log = (msg, ...args) => console.log(`[DebugWorker] ${msg}`, ...args);
const warn = (msg, ...args) => console.warn(`[DebugWorker] ${msg}`, ...args);
const err = (msg, ...args) => console.error(`[DebugWorker] ${msg}`, ...args);

const KEEPALIVE_ALARM = 'keepalive';
const KEEPALIVE_INTERVAL_MS = 20000;
const STALE_THRESHOLD_MS = 60000;
let lastTraffic = 0;

async function init() {
  log('Initializing (DEBUG mode - no auth required)...');
  const config = await getConfig();
  if (config.autoConnect) connect();
  chrome.runtime.onMessage.addListener(handleInternalMessage);
  chrome.alarms.onAlarm.addListener(onAlarm);
  scheduleKeepAlive();
}

function scheduleKeepAlive() {
  chrome.alarms.create(KEEPALIVE_ALARM, { when: Date.now() + KEEPALIVE_INTERVAL_MS });
}

function onAlarm(alarm) {
  if (alarm.name !== KEEPALIVE_ALARM) return;
  keepAlive().finally(scheduleKeepAlive);
}

async function keepAlive() {
  const config = await getConfig();
  if (config.autoConnect === false) return;

  if (!ws || ws.readyState !== WebSocket.OPEN) {
    log('KeepAlive: socket not open, reconnecting...');
    reconnectDelay = 1000;
    connect();
    return;
  }

  if (lastTraffic > 0 && Date.now() - lastTraffic > STALE_THRESHOLD_MS) {
    log('KeepAlive: no traffic from server, reconnecting...');
    reconnectDelay = 1000;
    disconnect();
    connect();
    return;
  }

  try {
    ws.send(createMessage(MessageType.HEARTBEAT, { timestamp: Date.now() }));
  } catch (e) {
    err('KeepAlive: heartbeat failed, reconnecting:', e);
    reconnectDelay = 1000;
    disconnect();
    connect();
  }
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
}

async function connect() {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return;

  const config = await getConfig();
  
  // Use debug endpoint - no token required
  const wsUrl = `ws://${config.serverAddr}/ws/browser-worker-debug`;
  log('Connecting to:', wsUrl);
  setState(WorkerState.CONNECTING);

  try {
    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      log('WebSocket connected, sending registration (no auth)...');
      setState(WorkerState.CONNECTING);
      reconnectDelay = 1000;
      sendRegister();
    };

    ws.onmessage = (event) => {
      lastTraffic = Date.now();
      const msg = parseMessage(event.data);
      if (!msg) return;
      handleServerMessage(msg);
    };

    ws.onclose = (event) => {
      log('WebSocket closed:', event.code);
      setState(WorkerState.DISCONNECTED);
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

function sendRegister() {
  if (ws?.readyState !== WebSocket.OPEN) return;
  const ua = navigator.userAgent;
  let browser = 'chrome';
  if (ua.includes('Firefox')) browser = 'firefox';
  else if (ua.includes('Edg/')) browser = 'edge';
  
  // No token sent - debug mode
  ws.send(createMessage(MessageType.REGISTER, {
    browser: { name: browser, userAgent: ua },
    capabilities: ['cookies', 'dom', 'javascript'],
    version: '1.0.0-debug',
  }));
}

async function handleServerMessage(msg) {
  switch (msg.type) {
    case MessageType.REGISTER_RESPONSE:
      if (msg.payload.ok) {
        log('Registration successful (DEBUG mode)');
        setState(WorkerState.CONNECTED);
        broadcastState();
      } else {
        warn('Registration failed:', msg.payload.error);
        setState(WorkerState.UNAUTHENTICATED);
        broadcastState();
      }
      break;
    case MessageType.JOB_REQUEST:
      await handleJobRequest(msg.payload);
      break;
    case MessageType.PING:
      ws.send(createMessage(MessageType.PONG, { timestamp: Date.now() }));
      break;
    case MessageType.HEARTBEAT_ACK:
      lastTraffic = Date.now();
      break;
    case MessageType.CANCEL_JOB:
      break;
  }
}

async function handleJobRequest(payload) {
  const { jobId, url, operation, params = {} } = payload;
  if (operation === 'fetch_image') {
    log(`Job ${jobId}: fetch_image ${url}`);
    setState(WorkerState.DOWNLOADING);
    try {
      const result = await fetchImage(url, params);
      sendJobResult(jobId, JobStatus.OK, result);
    } catch (e) {
      err(`Job ${jobId} fetch_image failed:`, e);
      sendJobResult(jobId, JobStatus.ERROR, { error: e.message });
    } finally {
      setState(WorkerState.CONNECTED);
    }
    return;
  }
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

// Fetches a binary image (e.g. a novel cover) through the browser so it
// inherits site cookies such as cf_clearance. Hosts that hotlink- or
// Cloudflare-protect their images return a 403 challenge to a plain HTTP GET,
// but the authenticated browser resolves to the real bytes. The image is
// returned base64-encoded because the WebSocket transport carries JSON.
//
// Strategy mirrors how a human loads the cover: the background fetch() shares
// cookies but is still flagged by Cloudflare's bot checks, so on failure we
// fall back to a hidden tab (a real navigation that passes the checks) and
// read the bytes from the page context.
async function fetchImage(url, params = {}) {
  const maxWait = (params.timeout || 120) * 1000;
  try {
    return await fetchImageBackground(url);
  } catch (e) {
    log(`fetch_image background failed (${e.message}), falling back to tab`);
    return await fetchImageViaTab(url, maxWait);
  }
}

async function fetchImageBackground(url) {
  const resp = await fetch(url, {
    credentials: 'include',
    headers: {
      'User-Agent': navigator.userAgent,
      'Accept': 'image/avif,image/webp,image/apng,image/png,image/*,*/*;q=0.8',
      'Accept-Language': 'en-US,en;q=0.9',
      'Referer': new URL(url).origin + '/',
    },
  });
  if (!resp.ok) {
    let snippet = '';
    try { snippet = (await resp.text()).slice(0, 200); } catch { /* ignore */ }
    throw new Error(`HTTP ${resp.status} :: ${snippet}`);
  }
  const buffer = await resp.arrayBuffer();
  const { dataBase64 } = bytesToBase64(buffer);
  const contentType = resp.headers.get('content-type') || '';
  log(`fetch_image (background) succeeded for ${url} (${buffer.byteLength} bytes, ${contentType})`);
  return { dataBase64, contentType, url, size: buffer.byteLength };
}

// Opens a hidden tab to the image (a real navigation that satisfies
// Cloudflare) and extracts the raw bytes from the page context. Falls back to
// a canvas re-encode if the in-page fetch is blocked.
async function fetchImageViaTab(url, maxWait) {
  const parsed = new URL(url);
  const origin = parsed.origin;
  const startTime = Date.now();

  let tab;
  if (challengeTabId !== null && challengeOrigin === origin) {
    try {
      tab = await chrome.tabs.get(challengeTabId);
      await chrome.tabs.update(tab.id, { url, active: false });
    } catch {
      challengeTabId = null; challengeOrigin = null;
      tab = await chrome.tabs.create({ url, active: false });
      challengeTabId = tab.id; challengeOrigin = origin;
    }
  } else {
    if (challengeTabId !== null) { try { await chrome.tabs.remove(challengeTabId); } catch { /* ignore */ } }
    tab = await chrome.tabs.create({ url, active: false });
    challengeTabId = tab.id; challengeOrigin = origin;
  }

  const cleanup = async () => {
    try { await chrome.tabs.remove(tab.id); } catch { /* already closed */ }
    challengeTabId = null; challengeOrigin = null;
  };

  while (Date.now() - startTime < maxWait) {
    let tabInfo;
    try { tabInfo = await chrome.tabs.get(tab.id); } catch { break; }
    if (tabInfo.status === 'complete') {
      const isChallenge = await checkForChallenge(tab.id);
      if (isChallenge) {
        log('fetch_image tab hit Cloudflare challenge, waiting for user...');
        chrome.runtime.sendMessage({ type: 'CHALLENGE_DETECTED', url, tabId: tab.id }).catch(() => {});
        await sleep(3000);
        continue;
      }

      // 1. In-page fetch (lossless, same-origin, carries cf_clearance).
      for (let attempt = 0; attempt < 3; attempt++) {
        try {
          const results = await chrome.scripting.executeScript({
            target: { tabId: tab.id },
            func: () => {
              try {
                const run = async () => {
                  const resp = await fetch(self.location.href, { credentials: 'include', cache: 'force-cache' });
                  if (!resp.ok) return { error: 'page fetch ' + resp.status };
                  const buf = await resp.arrayBuffer();
                  const bytes = new Uint8Array(buf);
                  let binary = '';
                  const c = 0x8000;
                  for (let i = 0; i < bytes.length; i += c) binary += String.fromCharCode.apply(null, bytes.subarray(i, i + c));
                  return { dataBase64: btoa(binary), contentType: resp.headers.get('content-type') || 'image/jpeg', size: bytes.length };
                };
                return run();
              } catch (e) { return { error: e.message }; }
            },
          });
          const r = results[0]?.result;
          if (r && r.dataBase64) { await cleanup(); return r; }
          if (r && r.error) log(`tab image page-fetch error (attempt ${attempt + 1}): ${r.error}`);
        } catch (e) {
          err(`fetch_image tab script failed (attempt ${attempt + 1}):`, e);
        }
        await sleep(2000);
      }

      // 2. Canvas fallback (re-encodes; last resort).
      try {
        const results = await chrome.scripting.executeScript({
          target: { tabId: tab.id },
          func: () => {
            const img = document.querySelector('img');
            if (!img) return { error: 'no img element' };
            const canvas = document.createElement('canvas');
            canvas.width = img.naturalWidth || img.width;
            canvas.height = img.naturalHeight || img.height;
            const ctx = canvas.getContext('2d');
            ctx.drawImage(img, 0, 0);
            const dataUrl = canvas.toDataURL('image/png');
            return { dataUrl, contentType: 'image/png' };
          },
        });
        const r = results[0]?.result;
        if (r && r.dataUrl) {
          const dataBase64 = r.dataUrl.split(',')[1] || '';
          await cleanup();
          log(`fetch_image (canvas fallback) succeeded for ${url} (${dataBase64.length} b64 chars)`);
          return { dataBase64, contentType: r.contentType, url, size: Math.floor(dataBase64.length * 3 / 4) };
        }
      } catch (e) {
        err('fetch_image canvas fallback failed:', e);
      }

      await cleanup();
      throw new Error('failed to extract image bytes from tab');
    }
    await sleep(500);
  }

  await cleanup();
  throw new Error('timeout loading image tab');
}

// Converts an ArrayBuffer to a base64 string using chunked String.fromCharCode
// to avoid call-stack limits on large images.
function bytesToBase64(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode.apply(null, bytes.subarray(i, i + chunkSize));
  }
  return { dataBase64: btoa(binary), contentType: '' };
}

function sendJobResult(jobId, status, data) {
  if (ws?.readyState !== WebSocket.OPEN) return;
  ws.send(createMessage(MessageType.JOB_RESULT, { jobId, status, data }));
}

async function fetchRawPage(url, params = {}) {
  const maxWait = (params.timeout || 120) * 1000;

  const bgResult = await tryBackgroundFetch(url);
  if (bgResult) return bgResult;

  log('Background fetch hit Cloudflare challenge, using tab...');
  return fetchViaChallengeTab(url, maxWait);
}

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

    const buffer = await resp.arrayBuffer();
    let charset = 'utf-8';

    const contentType = resp.headers.get('content-type') || '';
    const csMatch = contentType.match(/charset\s*=\s*([^;]+)/i);
    if (csMatch) {
      charset = csMatch[1].trim().toLowerCase();
    }

    if (charset === 'utf-8' || charset === 'utf8') {
      const peekLen = Math.min(4096, buffer.byteLength);
      const peekBytes = new Uint8Array(buffer, 0, peekLen);
      const peekStr = new TextDecoder('windows-1252').decode(peekBytes);
      if (/charset\s*=\s*["']?\s*gb/i.test(peekStr)) {
        charset = 'gbk';
      }
    }

    let html;
    try {
      html = new TextDecoder(charset, { fatal: false }).decode(buffer);
    } catch (e) {
      log(`TextDecoder failed for charset "${charset}", falling back to utf-8`, e);
      html = new TextDecoder('utf-8', { fatal: false }).decode(buffer);
    }

    // Cloudflare challenge detection is anchored to structural artifacts that
    // ONLY the challenge page injects — never to prose text, which can
    // legitimately contain words like "Access denied" or "Just a moment".
    // This avoids false positives on real chapter content (e.g. Marriage Mate
    // ch.25, whose prose contains those phrases).
    const cfHasTitle = /<title[^>]*>\s*Just a moment\.\.\.\s*<\/title>/i.test(html);
    const cfHasScript = /<script[^>]+src=["'][^"']*\/cdn-cgi\/challenge-platform\//i.test(html);
    const cfHasDom = /id=["']cf-(?:challenge-running|please-wait)["']|id=["']challenge-form["']/i.test(html);
    const cfHasTurnstile = /class=["'][^"']*cf-turnstile[^"']*["']/i.test(html);
    let cfMitigated = false;
    try { cfMitigated = resp.headers.get('cf-mitigated') === 'challenge'; } catch {}

    const isCfChallenge = cfMitigated || cfHasScript || cfHasDom || (cfHasTitle && cfHasTurnstile);
    if (isCfChallenge) {
      const sigs = [];
      if (cfMitigated) sigs.push('header:cf-mitigated');
      if (cfHasTitle) sigs.push('title');
      if (cfHasScript) sigs.push('script');
      if (cfHasDom) sigs.push('dom');
      if (cfHasTurnstile) sigs.push('turnstile');
      log(`Background fetch: Cloudflare challenge detected (${sigs.join(',')}) for ${url}`);
      return null;
    }

    log(`Background fetch succeeded for ${url} (${html.length} bytes)`);

    let title = '';
    const titleMatch = html.match(/<title[^>]*>([^<]*)<\/title>/i);
    if (titleMatch) title = titleMatch[1];

    return { html, text: '', title, url };
  } catch (e) {
    log(`Background fetch error for ${url}:`, e);
    return null;
  }
}

async function fetchViaChallengeTab(url, maxWait) {
  const parsedUrl = new URL(url);
  const origin = parsedUrl.origin;
  const startTime = Date.now();

  let tab;
  if (challengeTabId !== null && challengeOrigin === origin) {
    try {
      tab = await chrome.tabs.get(challengeTabId);
      await chrome.tabs.update(tab.id, { url, active: false });
    } catch {
      challengeTabId = null;
      challengeOrigin = null;
      tab = await chrome.tabs.create({ url, active: false });
      challengeTabId = tab.id;
      challengeOrigin = origin;
    }
  } else {
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
        log('Challenge solved, waiting for redirects to settle...');
        await sleep(4000);

        const verifyChallenge = await checkForChallenge(tab.id);
        if (verifyChallenge) {
          log('Page still shows challenge after wait, continuing to poll...');
          await sleep(3000);
          continue;
        }

        log('Challenge fully resolved, extracting HTML...');

        const tabInfo2 = await chrome.tabs.get(tab.id);
        log(`Tab URL before extraction: ${tabInfo2.url}`);

        let data = null;
        for (let attempt = 0; attempt < 3; attempt++) {
          try {
            const results = await chrome.scripting.executeScript({
              target: { tabId: tab.id },
              func: () => ({
                html: document.documentElement.outerHTML,
                text: document.body.innerText,
                title: document.title,
                url: window.location.href,
              }),
            });
            data = results[0]?.result;
            if (data && data.html && data.html.length > 100) {
              log(`Extraction attempt ${attempt + 1} succeeded (${data.html.length} bytes)`);
              break;
            }
            log(`Extraction attempt ${attempt + 1}: html=${data?.html?.length || 0} bytes, retrying...`);
            await sleep(2000);
          } catch (e) {
            err(`Extraction attempt ${attempt + 1} failed:`, e);
            await sleep(2000);
          }
        }

        try { await chrome.tabs.remove(tab.id); } catch { /* already closed */ }
        challengeTabId = null;
        challengeOrigin = null;

        return data || { html: '', text: '', title: '', url };
      }
      await sleep(500);
    } catch (e) {
      err('Error checking challenge tab:', e);
      await sleep(1000);
    }
  }
  throw new Error('Timeout waiting for Cloudflare challenge to be solved.');
}

async function checkForChallenge(tabId) {
  try {
    const results = await chrome.scripting.executeScript({
      target: { tabId },
      func: () => {
        const signals = [];
        if ((document.title || '').trim() === 'Just a moment...') signals.push('title');
        if (document.querySelector('script[src*="/cdn-cgi/challenge-platform/"]')) signals.push('script');
        if (document.querySelector('#cf-challenge-running, #cf-please-wait, #challenge-form')) signals.push('dom');
        if (document.querySelector('.cf-turnstile, [data-sitekey]')) signals.push('turnstile');
        // A real challenge is still present only when a Cloudflare-exclusive
        // artifact exists. Plain prose (e.g. a chapter titled "Just a moment")
        // never injects these, so we never false-positive on it.
        return {
          isChallenge: signals.includes('script') || signals.includes('dom') || signals.includes('turnstile'),
          signals,
        };
      },
    });
    return results[0]?.result?.isChallenge || false;
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
