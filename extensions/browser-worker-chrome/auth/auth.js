// Legacy callback page never accepts credentials. The background validates
// the server callback against a pending tab/origin/nonce-bound authorization.
document.getElementById('title').textContent = 'Autorización no válida';
document.getElementById('message').textContent = 'Inicia la conexión desde el botón de autorización de la extensión.';
const closeBtn = document.getElementById('closeBtn');
closeBtn.style.display = 'inline-block';
closeBtn.onclick = () => window.close();
