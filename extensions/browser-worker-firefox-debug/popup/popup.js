const connectBtn = document.getElementById('connectBtn');
const statusDot = document.getElementById('statusDot');
const statusLabel = document.getElementById('statusLabel');
const serverAddr = document.getElementById('serverAddr');
const serverInput = document.getElementById('serverInput');

let currentState = 'disconnected';

async function loadState() {
  const response = await chrome.runtime.sendMessage({ type: 'GET_STATE' });
  updateUI(response.state);
}

function updateUI(state) {
  currentState = state;
  statusDot.className = 'status-dot';
  
  switch (state) {
    case 'connected':
      statusDot.classList.add('connected');
      statusLabel.textContent = 'Connected';
      connectBtn.textContent = 'Disconnect';
      connectBtn.className = 'btn btn-danger';
      connectBtn.disabled = false;
      break;
    case 'connecting':
      statusDot.classList.add('connecting');
      statusLabel.textContent = 'Connecting...';
      connectBtn.textContent = 'Cancel';
      connectBtn.className = 'btn btn-danger';
      connectBtn.disabled = false;
      break;
    case 'downloading':
      statusDot.classList.add('connected');
      statusLabel.textContent = 'Fetching page...';
      connectBtn.textContent = 'Disconnect';
      connectBtn.className = 'btn btn-danger';
      connectBtn.disabled = false;
      break;
    default:
      statusDot.classList.add('disconnected');
      statusLabel.textContent = 'Disconnected';
      connectBtn.textContent = 'Connect';
      connectBtn.className = 'btn btn-primary';
      connectBtn.disabled = false;
  }
}

connectBtn.addEventListener('click', async () => {
  if (currentState === 'connected' || currentState === 'connecting' || currentState === 'downloading') {
    chrome.runtime.sendMessage({ type: 'DISCONNECT' });
  } else {
    const server = serverInput.value.trim();
    if (server) {
      await chrome.runtime.sendMessage({ type: 'UPDATE_CONFIG', config: { serverAddr: server } });
    }
    chrome.runtime.sendMessage({ type: 'CONNECT' });
  }
});

serverInput.addEventListener('change', async () => {
  const server = serverInput.value.trim();
  if (server) {
    await chrome.runtime.sendMessage({ type: 'UPDATE_CONFIG', config: { serverAddr: server } });
    serverAddr.textContent = server;
  }
});

chrome.runtime.onMessage.addListener((msg) => {
  if (msg.type === 'STATE_CHANGED') {
    updateUI(msg.state);
  }
});

loadState();
