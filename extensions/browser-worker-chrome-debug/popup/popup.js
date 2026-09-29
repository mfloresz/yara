import { getConfig, setConfig } from '../shared/storage.js';
import { isSupportedUrl } from '../shared/supported-sites.js';

const statusEl = document.getElementById('status-bar');
const statusText = document.getElementById('status-text');
const statusServer = document.getElementById('status-server');
const serverAddrInput = document.getElementById('server-addr');
const autoConnectCheckbox = document.getElementById('auto-connect');
const btnToggle = document.getElementById('btn-toggle');
const btnImport = document.getElementById('btn-import');
const btnRefresh = document.getElementById('btn-refresh');
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
let isActive = false;

async function init() {
  const config = await getConfig();
  serverAddrInput.value = config.serverAddr;
  autoConnectCheckbox.checked = config.autoConnect;
  statusServer.textContent = config.serverAddr;

  renderImportAction();

  const response = await chrome.runtime.sendMessage({ type: 'GET_STATE' });
  updateUI(response.state, response.connected);

  chrome.runtime.onMessage.addListener((msg) => {
    if (msg.type === 'STATE_CHANGED') updateUI(msg.state, null);
    if (msg.type === 'CHALLENGE_DETECTED') showChallenge(msg.tabId);
  });
}

function updateUI(state, connected) {
  const connecting = state === 'connecting';
  isActive = state === 'connected' || state === 'downloading' || !!connected;

  statusEl.dataset.state = state;
  statusText.textContent = stateNames[state] || state;

  btnToggle.textContent = connecting ? 'Cancelar' : isActive ? 'Desconectar' : 'Conectar';
  btnToggle.className = connecting || isActive ? 'btn btn-secondary' : 'btn btn-primary';
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

btnToggle.addEventListener('click', async () => {
  if (isActive || btnToggle.textContent === 'Cancelar') {
    chrome.runtime.sendMessage({ type: 'DISCONNECT' });
    return;
  }

  const addr = serverAddrInput.value.trim();
  if (!addr) {
    showError('Configura la dirección del servidor primero');
    serverAddrInput.focus();
    return;
  }

  clearError();
  await setConfig({ serverAddr: addr, autoConnect: autoConnectCheckbox.checked });
  statusServer.textContent = addr;
  chrome.runtime.sendMessage({ type: 'UPDATE_CONFIG', config: { serverAddr: addr, autoConnect: autoConnectCheckbox.checked } });
  chrome.runtime.sendMessage({ type: 'CONNECT' });
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
