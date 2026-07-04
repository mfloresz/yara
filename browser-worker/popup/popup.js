import { getConfig, setConfig, getWorkerToken, setWorkerToken, clearWorkerToken } from '../shared/storage.js';

const statusEl = document.getElementById('status-bar');
const statusText = document.getElementById('status-text');
const serverAddrInput = document.getElementById('server-addr');
const autoConnectCheckbox = document.getElementById('auto-connect');
const btnConnect = document.getElementById('btn-connect');
const btnDisconnect = document.getElementById('btn-disconnect');
const btnAuth = document.getElementById('btn-auth');
const authPanel = document.getElementById('auth-panel');
const infoPanel = document.getElementById('info-panel');
const infoBrowser = document.getElementById('info-browser');
const infoUptime = document.getElementById('info-uptime');
const infoToken = document.getElementById('info-token');
const errorPanel = document.getElementById('error-panel');
const errorText = document.getElementById('error-text');
const challengePanel = document.getElementById('challenge-panel');
const btnRefresh = document.getElementById('btn-refresh');

const stateNames = {
  disconnected: 'Desconectado',
  connecting: 'Conectando...',
  connected: 'Conectado',
  downloading: 'Proxy activo',
  unauthenticated: 'Sin autenticar',
};

let connectedAt = null;
let challengeTabId = null;

async function init() {
  const config = await getConfig();
  serverAddrInput.value = config.serverAddr;
  autoConnectCheckbox.checked = config.autoConnect;

  const response = await chrome.runtime.sendMessage({ type: 'GET_STATE' });
  updateUI(response.state, response.connected);

  const tokenData = await getWorkerToken();
  updateAuthUI(tokenData);

  chrome.runtime.onMessage.addListener((msg) => {
    if (msg.type === 'STATE_CHANGED') updateUI(msg.state);
    if (msg.type === 'CHALLENGE_DETECTED') showChallenge(msg.url, msg.tabId);
    if (msg.type === 'AUTH_COMPLETE') {
      getWorkerToken().then(updateAuthUI);
    }
  });
}

function updateUI(state, connected) {
  statusEl.className = `status-bar ${state}`;
  statusText.textContent = stateNames[state] || state;

  const isConnected = state === 'connected' || state === 'downloading' || connected;
  btnConnect.disabled = isConnected;
  btnDisconnect.disabled = !isConnected;

  if (state === 'unauthenticated') {
    authPanel.classList.remove('hidden');
    btnConnect.disabled = true;
  } else {
    authPanel.classList.add('hidden');
  }

  if (state === 'connected' || state === 'downloading') {
    connectedAt = connectedAt || Date.now();
    infoPanel.classList.remove('hidden');
    updateInfo();
  } else {
    infoPanel.classList.add('hidden');
    if (state === 'disconnected') connectedAt = null;
  }
}

async function updateAuthUI(tokenData) {
  if (tokenData && tokenData.token) {
    infoToken.textContent = tokenData.token.substring(0, 8) + '...';
    infoToken.title = 'Token activo';
    btnAuth.textContent = 'Re-autenticar';
    btnAuth.className = 'btn btn-secondary';
  } else {
    infoToken.textContent = 'No configurado';
    btnAuth.textContent = 'Autenticar con el Servidor';
    btnAuth.className = 'btn btn-primary';
  }
}

function updateInfo() {
  const ua = navigator.userAgent;
  let browser = 'Chrome';
  if (ua.includes('Firefox')) browser = 'Firefox';
  else if (ua.includes('Edg/')) browser = 'Edge';
  infoBrowser.textContent = browser;

  if (connectedAt) {
    const s = Math.floor((Date.now() - connectedAt) / 1000);
    infoUptime.textContent = `${Math.floor(s / 60)}m ${s % 60}s`;
  }
}

function showChallenge(url, tabId) {
  challengeTabId = tabId;
  challengePanel.classList.remove('hidden');
}

setInterval(updateInfo, 1000);

btnAuth.addEventListener('click', async () => {
  const addr = serverAddrInput.value.trim();
  if (!addr) {
    errorPanel.classList.remove('hidden');
    errorText.textContent = 'Configura la dirección del servidor primero';
    return;
  }
  
  await setConfig({ serverAddr: addr });
  
  const extId = chrome.runtime.id;
  const authURL = `http://${addr}/api/worker-auth/authorize?extension_id=${extId}`;
  chrome.tabs.create({ url: authURL });
});

btnConnect.addEventListener('click', async () => {
  const addr = serverAddrInput.value.trim();
  if (!addr) return;
  errorPanel.classList.add('hidden');
  await setConfig({ serverAddr: addr, autoConnect: autoConnectCheckbox.checked });
  chrome.runtime.sendMessage({ type: 'UPDATE_CONFIG', config: { serverAddr: addr, autoConnect: autoConnectCheckbox.checked } });
  chrome.runtime.sendMessage({ type: 'CONNECT' });
});

btnDisconnect.addEventListener('click', () => {
  chrome.runtime.sendMessage({ type: 'DISCONNECT' });
});

btnRefresh.addEventListener('click', async () => {
  if (challengeTabId) {
    try { await chrome.tabs.reload(challengeTabId); challengePanel.classList.add('hidden'); } catch {}
  }
});

serverAddrInput.addEventListener('change', () => {
  const addr = serverAddrInput.value.trim();
  if (addr) setConfig({ serverAddr: addr });
});

autoConnectCheckbox.addEventListener('change', () => {
  setConfig({ autoConnect: autoConnectCheckbox.checked });
});

init();
