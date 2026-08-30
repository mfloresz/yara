# Yara

**[English](README.md) · [Español](README.es.md)**

> **Tu biblioteca + pipeline de traducción + lector.** Un único binario en Go con PocketBase embebido y una SPA en Vue que lo hace todo: importar novelas, traducirlas con IA, refinar el resultado y leer sin fricción.

<p align="center">
  <img src="docs/screenshots/03-biblioteca.png" alt="Biblioteca de Yara — grid de portadas" width="100%">
</p>
<p align="center"><em>Biblioteca · seis novelas de ejemplo con portadas generadas · tema claro “The Quiet Shelf” (warm stone sobre bone paper)</em></p>

---

## Qué es Yara

Yara es un **servidor auto-alojado para lectores que traducen**. Importas una novela (desde una URL, un EPUB o un ZIP), la traduces capítulo a capítulo con el proveedor de IA que elijas, refinas el resultado y la lees en un lector pensado para sesiones largas. Todo corre en un solo proceso Go — sin base de datos externa, sin microservicios.

El DNA del producto (ver `PRODUCT.md`): [Readest](https://readest.com) / Calibre para el pulido de biblioteca y lectura, [Mihon](https://mihon.app)/Tachiyomi para la dualidad *gestionar + consumir* en móvil. El cromo retrocede; el texto y la portada mandan.

### Orígenes

Yara es la evolución de [novel-translator](https://github.com/mfloresz/novel-translator), una aplicación de escritorio escrita en Python y PyQt6. La app original demostró el flujo — importar, traducir, leer — pero Yara lo replantea como un servidor auto-alojado con interfaz web, API REST versionada, operaciones en lote, sincronización del progreso de lectura y capacidades que la versión de escritorio nunca tuvo.

### Cliente Android

[yara-app](https://github.com/mfloresz/yara-app) es la app cliente para Android. Se conecta a un servidor Yara en ejecución y lleva la biblioteca y el lector al teléfono sin pasar por el navegador.

---

## ⚠️ Despliegue: solo uso personal

Yara está pensada como una **aplicación de uso personal**, para correr en tu propia máquina o ser accedida desde dispositivos en la **misma red local**. **No está pensada para exponerse a internet**: actualmente no hay restricciones de registro — cualquiera que pueda alcanzar el servidor puede crear una cuenta. Los controles de registro se añadirán en una versión futura; hasta entonces, mantén Yara dentro de tu LAN.

---

## Galería

### Biblioteca

La vista principal. Un grid de portadas en ratio 2:3 con elevación real solo en las portadas, búsqueda instantánea y menú “Ver como”. Cada novela prefiere su título traducido cuando existe.

![Biblioteca](docs/screenshots/03-biblioteca.png)

<details><summary>Versión móvil</summary>

<img src="docs/screenshots/08-biblioteca-mobile.png" alt="Biblioteca en móvil" width="380">

Grid de tres columnas en 390 px, búsqueda arriba, botón flotante “+”. La barra superior colapsa a cuatro iconos sin perder navegación.

</details>

Modo oscuro (mismo contenido, rampa warm-neutral invertida, acentos semánticos intactos):

<img src="docs/screenshots/09-biblioteca-dark.png" alt="Biblioteca en modo oscuro" width="100%">

### Detalle de novela

Un sidebar a la izquierda (portada grande + CTA **Leer** + acciones de configuración) y pestañas para **Capítulos · Traducir · Limpieza · Exportar · Trabajos**.

![Detalle de novela](docs/screenshots/04-novel-detail.png)

Los estados de cada capítulo se ven de un vistazo: `pendiente` (gris), `traducido` (verde), `refinado` (azul), `fallido` (rojo). La selección múltiple alimenta las pestañas Traducir y Limpieza sin recargar.

### Lector

Tipografía de lectura (Geist, 1.05 rem / interlineado 1.75), medida contenida, navegación capítulo a capítulo y un sidebar de capítulos donde los que no tienen contenido aparecen atenuados.

![Lector](docs/screenshots/05-reader.png)

El lector respeta `prefers-reduced-motion` y persiste la posición de lectura en el servidor, de modo que una sesión puede continuar en otro dispositivo.

### Operaciones (pipeline en lote)

Una tabla de control para bibliotecas grandes: **verificar → descargar → traducir** solo lo seleccionado, con filtros de texto y selección por fila.

![Operaciones](docs/screenshots/07-operations.png)

Las acciones en lote se aceptan de forma asíncrona y se encolan en el servidor; el progreso se ve en el drawer de trabajos.

### Ajustes

Toda la configuración es **por usuario** y persiste en el backend: tema, proveedor de IA activo, parámetros de segmentación, prompts y tokens del browser worker.

![Ajustes](docs/screenshots/06-settings.png)

Las API keys son write-only — el servidor nunca las devuelve, solo un flag `apiKeyConfigured`.

### Autenticación

![Login](docs/screenshots/01-login.png)

Las sesiones usan una cookie HttpOnly con `SameSite=Strict`, y también hay un token bearer disponible para clientes de API.

---

## Sitios soportados

Yara importa novelas directamente desde 11 sitios:

- **Descarga directa** — NovelFire, FenrirRealm, CherryMist, SkyNovels, Literotica, WTR-Lab, NovelArrow.
- **Detrás de Cloudflare** — 69Shuba, EmpireNovel, FloraeGarden, SkyDemonOrder. Estos requieren la extensión browser-worker (ver abajo).

También se pueden importar novelas desde archivos EPUB o ZIP, así que cualquier fuente fuera del catálogo sigue funcionando.

### Extensión browser-worker (sitios protegidos por Cloudflare)

Los retos de Cloudflare no se pueden resolver desde el servidor, así que Yara incluye una extensión de navegador — Chrome y Firefox, Manifest V3 — que reenvía las peticiones a través de un navegador real en tu máquina. Inicias sesión en la extensión con un token de browser-worker (generado en Ajustes → Tokens de Browser Worker), y cuando un sitio protegido muestra un reto, la extensión abre una pestaña en segundo plano para que lo resuelvas una sola vez; las peticiones posteriores a ese origen reusan las cookies cacheadas automáticamente. Las extensiones viven en `extensions/browser-worker-chrome/` y `extensions/browser-worker-firefox/`, y cada sitio del catálogo declara si necesita esta vía mediante `RequiresBrowser()`.

Para desarrollo de parsers existen además variantes `-debug` de la extensión que se conectan sin auth a un proxy de depuración independiente (`cmd/debug-proxy`, puerto 5177).

---

## Flujos típicos

### 1 · Importar y traducir

```
Biblioteca → “Nueva novela” → pegar URL → preview → importar
Detalle → pestaña Traducir → seleccionar capítulos → elegir provider/modelo → traducir
Trabajos (drawer del topbar) → ver el progreso en tiempo real
```

El servidor descarga el primer capítulo de forma sincrónica y encola el resto. Los capítulos largos se auto-segmentan antes de enviarse al proveedor de IA.

### 2 · Lotes en bibliotecas grandes

```
Operaciones → buscar/filtrar → seleccionar actualizables → verificar
→ descargar (capítulos nuevos detectados) → traducir (pendientes)
```

Las verificaciones y descargas comparten una cola; las traducciones y refinamientos usan otra. Si la cola está llena, la respuesta es `503` con `Retry-After: 30`.

### 3 · Exportar para publicar

```
Detalle → pestaña Exportar → elegir variante (original / traducido / refinado) → descargar .epub
```

Los EPUB se generan desde los capítulos persistidos y se sirven bajo demanda.

---

## Diseño

El North Star (ver `DESIGN.md`) es **“The Quiet Shelf”**: una sola rampa warm-stone (bone paper `#fafaf9` → warm ink `#141413`), color reservado al estado semántico, una sola familia tipográfica (Geist/Inter) y elevación real solo en las portadas. Los temas claro/oscuro/sistema invierten la misma rampa en lugar de duplicar la paleta.

Anti-referencias: nada de “admin dashboard” denso ni móvil como breakpoint tardío.

---

## Stack

| Capa | Tecnología |
|---|---|
| Backend | Go 1.26, PocketBase embebido, `goai` para proveedores OpenAI-compatibles, `log/slog` |
| Frontend | Vue 3 + Vite + Naive UI + TypeScript + vue-router + PWA |
| IA | 7 providers registrados: `venice` (default), `openrouter`, `meta`, `opencode-go`, `opencode-zen`, `lmstudio`, `google` — claves almacenadas con AES-GCM |
| Persistencia | SQLite (vía PocketBase), esquema idempotente en `store_schema.go`, flag `--migrate-db` para breaking changes |
| Scraper | `internal/noveldownloader` — 11 parsers (NovelFire, FenrirRealm, FloraeGarden, CherryMist, EmpireNovel, 69Shuba, SkyNovels, SkyDemonOrder, Literotica, WTR-Lab, NovelArrow); `RequiresBrowser()` marca los que están detrás de Cloudflare |
| EPUB | `internal/epubimport` + `internal/epubexport` (paquetes puros, sin dependencias de HTTP/store) |
| Móvil | Build `android-arm64` para Termux, más el cliente [yara-app](https://github.com/mfloresz/yara-app) |

---

## Inicio rápido

### Desarrollo (dos terminales)

```bash
# Terminal 1 — frontend con HMR en :5175 (proxies /api y /ai → :5176)
cd frontend && npm run dev

# Terminal 2 — backend en :5176
go run ./cmd/server --addr :5176 --data-dir ./data
```

Si tocas el frontend, `npm run build` (o `make build`) regenera `frontend/dist/` — el binario Go embebe ese directorio, y un build rancio sirve la SPA vieja en silencio.

### Producción

```bash
make build
./bin/translator-server-linux-amd64-<version> --addr :5176 --data-dir ./data
```

### Android / Termux

```bash
make android
# copiar al teléfono y:
chmod +x translator-server-android-arm64-<version>
./translator-server-android-arm64-<version> --addr 127.0.0.1:5176 --data-dir ./data
```

### Todas las plataformas de una vez

```bash
make all        # linux-amd64, linux-arm64, linux-armv7, android-arm64, android-armv7
make compress   # UPX sobre los binarios (requiere upx)
```

---

## Configuración

Orden de resolución: **flag CLI > variable de entorno > default** (ver `internal/config/config.go`).

**Flags** — `--addr` (default `:5176`), `--port`, `--data-dir` (junto al binario), `--static-dir` (dev: sirve `frontend/dist` desde disco), `--migrate-db`, `--migrate-thumbnails`, `--version`.

**Entorno** — `APP_ENCRYPTION_KEY` (base64/hex, 32 bytes decodificados; se autogenera en `<data-dir>/app.key` si no se define), `STATIC_DIR`, `DOWNLOAD_MIN_DELAY_MS` / `DOWNLOAD_MAX_DELAY_MS` (throttling del import-from-URL), `ADDR` / `PORT` / `DATA_DIR` (fallback de los flags), `VITE_API_URL` (override del baseUrl de la SPA).

---

## API

Superficie única: **`/api/v1/*`** — envelope `{data, meta, links}`, header `X-API-Version: v1` y errores `application/problem+json`. Referencia completa en [`docs/api/README.md`](docs/api/README.md) y spec machine-readable en [`docs/api/openapi.yaml`](docs/api/openapi.yaml).

Paginación canónica `?page=1&per_page=50` (con forma compat `?limit&offset`), campos dispersos vía `?fields=id,sourceTitle,status`, `202 Accepted` para trabajos asíncronos, `204` para borrados.

---

## Estructura del proyecto

```
cmd/server/            Entrypoint (config → encryptor → PocketBase → store → api → ListenAndServe)
cmd/debug-proxy/       Micro-servidor en :5177 para depurar parsers detrás de Cloudflare
internal/api/          Capa HTTP — un router_*.go por recurso, worker de trabajos en runtime_*.go
internal/store/        Persistencia PocketBase — colecciones sembradas por EnsureSchema()
internal/ai/           Interfaz Provider + OpenAIProvider + registry.go
internal/secure/       AES-GCM para las API keys
internal/noveldownloader/  Parsers puros
internal/epubimport/ / epubexport/
frontend/              SPA Vue 3 — pages/, components/, composables/, app/services.ts
frontend_embed.go      //go:embed all:frontend/dist
extensions/            browser-worker-chrome / firefox (+ variantes -debug sin auth)
docs/                  Documentación de API y codemaps de arquitectura
```

---

## Testing

```bash
go test ./...            # unitarios + integración (PocketBase real en t.TempDir, sin mocks)
go test -short ./...     # salta los tests live-URL en noveldownloader/realtest_test.go
npm run build            # el typecheck real (vue-tsc -b && vite build)
go vet ./...
```

Los tests de integración en `internal/api/` bloquean la forma del envelope v1, los status codes y los headers `Location`.

---

## Notas de operación

- PocketBase corre **in-process** — no hay puerto de admin ni UI `/_/` expuesta. El servidor expone `/healthz`, `/ws/browser-worker`, `/api/v1/*`, los entry points de auth y el fallback de la SPA.
- El worker de trabajos es in-process: dos colas con buffer (capacidad 128) y un goroutine cada una. La concurrencia por proveedor (1–10, default 1) va por `errgroup.SetLimit`.
- `EnsureSchema()` no backfillea estadísticas al arrancar; las stats de cada novela se recalculan tras cada mutación de capítulos/trabajos/importaciones.
- Depuración de sitios detrás de Cloudflare: ejecuta `go run ./cmd/debug-proxy` en `:5177`, conecta una extensión `browser-worker-*-debug` y reenvía peticiones por `POST :5177/api/proxy/fetch`.

---

## Licencia

Sin licencia publicada todavía. Asume todos los derechos reservados hasta que se añada una.
