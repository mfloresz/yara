import { getConfig, setConfig, getWorkerToken } from '../shared/storage.js';
import { isSupportedUrl, yaraBaseUrl } from '../shared/supported-sites.js';

const statusEl = document.getElementById('status-bar');
const statusText = document.getElementById('status-text');
const statusServer = document.getElementById('status-server');
const serverAddrInput = document.getElementById('server-addr');
const autoConnectCheckbox = document.getElementById('auto-connect');
const btnConnect = document.getElementById('btn-connect');
const btnDisconnect = document.getElementById('btn-disconnect');
const btnAuth = document.getElementById('btn-auth');
const btnImport = document.getElementById('btn-import');
const btnRefresh = document.getElementById('btn-refresh');
const authPanel = document.getElementById('auth-panel');
const importPanel = document.getElementById('import-panel');
const importHost = document.getElementById('import-host');
const challengePanel = document.getElementById('challenge-panel');
const errorPanel = document.getElementById('error-panel');
const errorText = document.getElementById('error-text');

const stateNames = {
  disconnected: 'Desconectado',
  connecting: 'Conectando...',
  connected: 'Conectado',
  downloading: 'Proxy activo',
  unauthenticated: 'Sin autenticar',
};

let challengeTabId = null;

async function init() {
  const config = await getConfig();
  serverAddrInput.value = config.serverAddr;
  autoConnectCheckbox.checked = config.autoConnect;
  statusServer.textContent = config.serverAddr;

  const tokenData = await getWorkerToken();
  updateAuthUI(tokenData);
  renderImportAction();

  const response = await chrome.runtime.sendMessage({ type: 'GET_STATE' });
  updateUI(response.state, response.connected, tokenData);

  chrome.runtime.onMessage.addListener((msg) => {
    if (msg.type === 'STATE_CHANGED') {
      getWorkerToken().then(td => updateUI(msg.state, null, td));
    }
    if (msg.type === 'CHALLENGE_DETECTED') showChallenge(msg.tabId);
    if (msg.type === 'auth_complete' || msg.type === 'AUTH_COMPLETE') {
      getWorkerToken().then(async (td) => {
        updateAuthUI(td);
        const res = await chrome.runtime.sendMessage({ type: 'GET_STATE' });
        updateUI(res.state, res.connected, td);
      });
    }
  });
}

function updateUI(state, connected, tokenData) {
  const hasToken = !!(tokenData && tokenData.token);

  statusEl.dataset.state = state;
  statusText.textContent = stateNames[state] || state;

  const isConnected = state === 'connected' || state === 'downloading' || connected;

  if (!hasToken) {
    authPanel.classList.remove('hidden');
    btnConnect.disabled = true;
    btnDisconnect.disabled = true;
  } else if (state === 'unauthenticated') {
    authPanel.classList.remove('hidden');
    btnConnect.disabled = true;
    btnDisconnect.disabled = !isConnected;
  } else {
    authPanel.classList.add('hidden');
    btnConnect.disabled = isConnected;
    btnDisconnect.disabled = !isConnected;
  }
}

function updateAuthUI(tokenData) {
  if (tokenData && tokenData.token) {
    btnAuth.textContent = 'Volver a autenticar';
  } else {
    btnAuth.textContent = 'Autenticar con el servidor';
  }
}

// The context-menu entry only shows on supported sites, so the popup button
// stays hidden everywhere else: the UI only offers actions it can perform.
async function renderImportAction() {
  const tab = await activeTab();
  if (!tab || !isSupportedUrl(tab.url)) {
    importPanel.classList.add('hidden');
    return;
  }
  importHost.textContent = new URL(tab.url).hostname.replace(/^www\./, '');
  importPanel.classList.remove('hidden');
}

async function activeTab() {
  const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
  return tabs[0] || null;
}

function showChallenge(tabId) {
  challengeTabId = tabId;
  challengePanel.classList.remove('hidden');
}

function showError(message) {
  errorText.textContent = message;
  errorPanel.classList.remove('hidden');
}

function clearError() {
  errorPanel.classList.add('hidden');
}

btnImport.addEventListener('click', async () => {
  const tab = await activeTab();
  if (!tab || !isSupportedUrl(tab.url)) {
    importPanel.classList.add('hidden');
    return;
  }
  btnImport.disabled = true;
  const res = await chrome.runtime.sendMessage({ type: 'IMPORT_URL', url: tab.url });
  btnImport.disabled = false;
  if (res && res.ok) {
    window.close();
  } else {
    showError((res && res.error) || 'No se pudo abrir Yara con esta página');
  }
});

btnAuth.addEventListener('click', async () => {
  const addr = serverAddrInput.value.trim();
  if (!addr) {
    showError('Configura la dirección del servidor primero');
    serverAddrInput.focus();
    return;
  }

  await setConfig({ serverAddr: addr });
  statusServer.textContent = addr;

  const extId = chrome.runtime.id;
  const authURL = `${yaraBaseUrl(addr)}/api/v1/worker-auth/authorize?extension_id=${encodeURIComponent(extId)}`;
  chrome.tabs.create({ url: authURL });
});

btnConnect.addEventListener('click', async () => {
  const addr = serverAddrInput.value.trim();
  if (!addr) {
    showError('Configura la dirección del servidor primero');
    serverAddrInput.focus();
    return;
  }

  const tokenData = await getWorkerToken();
  if (!tokenData || !tokenData.token) {
    authPanel.classList.remove('hidden');
    return;
  }

  clearError();
  await setConfig({ serverAddr: addr, autoConnect: autoConnectCheckbox.checked });
  statusServer.textContent = addr;
  chrome.runtime.sendMessage({ type: 'UPDATE_CONFIG', config: { serverAddr: addr, autoConnect: autoConnectCheckbox.checked } });
  chrome.runtime.sendMessage({ type: 'CONNECT' });
});

btnDisconnect.addEventListener('click', () => {
  chrome.runtime.sendMessage({ type: 'DISCONNECT' });
});

btnRefresh.addEventListener('click', async () => {
  if (challengeTabId) {
    try { await chrome.tabs.reload(challengeTabId); challengePanel.classList.add('hidden'); } catch { /* ignore */ }
  }
});

serverAddrInput.addEventListener('change', () => {
  const addr = serverAddrInput.value.trim();
  if (!addr) return;
  setConfig({ serverAddr: addr });
  statusServer.textContent = addr;
});

autoConnectCheckbox.addEventListener('change', () => {
  setConfig({ autoConnect: autoConnectCheckbox.checked });
});

init();
