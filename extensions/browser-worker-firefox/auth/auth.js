const params = new URLSearchParams(window.location.search);
const token = params.get('token');
const userId = params.get('user');
const error = params.get('error');

const icon = document.getElementById('icon');
const title = document.getElementById('title');
const message = document.getElementById('message');
const closeBtn = document.getElementById('closeBtn');

if (error) {
    icon.className = 'icon error';
    icon.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>';
    title.textContent = 'Error de Autenticación';
    message.textContent = decodeURIComponent(error);
    closeBtn.style.display = 'inline-block';
    closeBtn.onclick = () => window.close();
} else if (token && userId) {
    chrome.storage.local.set({
        workerToken: token,
        workerUserId: userId,
        workerConnectedAt: new Date().toISOString()
    }, () => {
        chrome.runtime.sendMessage({
            type: 'auth_complete',
            token: token,
            userId: userId
        });
        
        title.textContent = 'Conexión Establecida';
        message.textContent = 'La extensión está conectada a tu cuenta.';
        closeBtn.style.display = 'inline-block';
        closeBtn.onclick = () => window.close();
        
        setTimeout(() => window.close(), 2000);
    });
} else {
    icon.className = 'icon error';
    icon.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>';
    title.textContent = 'Parámetros Inválidos';
    message.textContent = 'No se recibió un token válido del servidor.';
    closeBtn.style.display = 'inline-block';
    closeBtn.onclick = () => window.close();
}
