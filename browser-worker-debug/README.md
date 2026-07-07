# Yara Browser Worker (Debug)

Extensión de depuración del browser worker que **no requiere autenticación**. Diseñada para desarrollo y testing con sitios protegidos por Cloudflare.

## Diferencias con la extensión principal

| Característica | Principal | Debug |
|---------------|-----------|-------|
| Autenticación | Requiere token de usuario | No requiere token |
| Endpoint WebSocket | `/ws/browser-worker` | `/ws/browser-worker-debug` |
| Almacenamiento | `yara_browser_worker` | `yara_browser_worker_debug` |
| Uso | Producción | Desarrollo/Testing |

## Funcionalidad maintainida

- Proxy HTTP completo con manejo de cookies
- Detección automática de Cloudflare challenges
- Manejo de pestañas para resolver challenges
- Extracción de HTML con soporte charset (GBK, UTF-8)
- Reconexión automática
- Mismo protocolo WebSocket que la extensión principal

## Instalación

1. Abrir Chrome y navegar a `chrome://extensions/`
2. Activar "Developer mode" (esquina superior derecha)
3. Click "Load unpacked"
4. Seleccionar la carpeta `browser-worker-debug/`
5. La extensión aparecerá con badge "DEBUG"

## Uso

1. Iniciar el servidor: `./bin/translator-server`
2. Click en el ícono de la extensión debug
3. Click "Connect" (no pedirá credenciales)
4. El servidor acceptará la conexión automáticamente

## Flujo de trabajo para sitios Cloudflare-protected

Cuando necesites scrape un sitio protegido por Cloudflare:

1. La extensión debug debe estar conectada
2. El servidor enviará la petición de fetch
3. Si Cloudflare detecta la petición, la extensión abrirá una pestaña
4. Resuelve el challenge manualmente en la pestaña
5. La extensión extraerá el HTML y lo devolverá al servidor
6. La pestaña se cerrará automáticamente

## Notas

- Esta extensión es solo para desarrollo
- No usar en producción
- El storage está separado de la extensión principal (no hay conflicto)
- Pueden estar instaladas ambas extensiones simultáneamente
