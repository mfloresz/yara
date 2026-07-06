# Browser Worker Proxy — Yara Extension Feature

> Archivo generado automáticamente. Documenta el feature de extensión como proxy
> (Browser Worker), el soporte del sitio 69shuba.com, y el análisis de fallos del
> parser para la URL https://www.69shuba.com/book/59083.htm.

Propósito de este archivo: proporcionar a un modelo de lenguaje todo el código
fuente necesario para comprender y analizar la implementación completa de este
feature, organizado por capas arquitectónicas, más un análisis detallado de los
posibles puntos de fallo del parser 69shuba.

Generado el: 2026-07-05
Total de archivos: **29**

---

## Arquitectura General

```

┌─────────────────────────────────────────────────────┐
│                  Chrome Extension                   │
│  (browser-worker/)                                  │
│  ┌──────────────┐  ┌───────────────────────────┐   │
│  │ service-worker│  │ popup / auth             │   │
│  │ · fetch()     │  │ · UI state               │   │
│  │ · challenge   │  │ · OAuth flow             │   │
│  │   tab mgmt    │  │ · server config          │   │
│  └──────┬───────┘  └───────────────────────────┘   │
└─────────┼───────────────────────────────────────────┘
          │ WebSocket (ws://host:port/ws/browser-worker)
          ▼
┌─────────────────────────────────────────────────────┐
│                  Go Server                           │
│  internal/api/                                       │
│  ┌──────────────────────────────────────────────┐   │
│  │ router_browser_worker.go                     │   │
│  │ · WebSocket upgrade & dispatch               │   │
│  │ · SendJobToBrowserWorker / waitForResult     │   │
│  │ · BrowserWorker struct                       │   │
│  ├──────────────────────────────────────────────┤   │
│  │ router_proxy.go                              │   │
│  │ · fetchViaBrowserWorker                      │   │
│  │ · ProxyFetchResult                           │   │
│  ├──────────────────────────────────────────────┤   │
│  │ proxy_http_client.go                         │   │
│  │ · ProxyHTTPClient (implements HTTPClient)   │   │
│  ├──────────────────────────────────────────────┤   │
│  │ browser_worker_fallback.go                   │   │
│  │ · getNovelInfoWithFallback                   │   │
│  │ · getNovelInfoViaProxy                       │   │
│  ├──────────────────────────────────────────────┤   │
│  │ router_worker_auth.go                        │   │
│  │ · OAuth2 token authorize/approve/revoke      │   │
│  ├──────────────────────────────────────────────┤   │
│  │ runtime_worker.go                            │   │
│  │ · processDownloadJob (proxy download)        │   │
│  ├──────────────────────────────────────────────┤   │
│  │ router_import.go                             │   │
│  │ · import-from-url (proxy download)           │   │
│  └──────────────────────────────────────────────┘   │
│                                                       │
│  internal/store/                                      │
│  ┌──────────────────────────────────────────────┐   │
│  │ store_worker_tokens.go                       │   │
│  │ · WorkerToken CRUD (SHA-256 hashed)          │   │
│  └──────────────────────────────────────────────┘   │
│                                                       │
│  internal/noveldownloader/                            │
│  ┌──────────────────────────────────────────────┐   │
│  │ browser_required.go  · browser_worker_provider│   │
│  │ 69shuba.go / 69shuba_metadata.go / ...       │   │
│  │ downloader.go (parser registry)              │   │
│  └──────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘

```

## Flujo de datos

### Flujo: Importar desde URL (preview / import)

1. Usuario POSTea a `/api/db/novels/preview-from-url` con URL `https://www.69shuba.com/book/59083.htm`
2. `router_import.go:221` → `getNovelInfoWithFallback(ctx, url)`
3. `browser_worker_fallback.go:16` → crea Downloader → busca parser → encuentra `69shuba` parser
4. `browser_worker_fallback.go:22` → `dl.GetNovelInfo(ctx, url)` (HTTP directo)
    a. `69shuba_metadata.go:32` → `client.Fetch(ctx, u)` (HTTP directo → 30s timeout)
    b. Cloudflare devuelve página de challenge (no el contenido real)
    c. `DecodeHTMLBody` no modifica (challenge es UTF-8 válido)
    d. No encuentra `articlename`, `author`, etc. en JS → campos vacíos
    e. Extrae `bookID` = `59083` de la URL
    f. `fetchChapterList(ctx, client, /book/59083/)` → HTTP directo → Cloudflare otra vez
    g. Selectores CSS no matchean → fallback `/txt/` links tampoco → 0 chapters
    h. `extractChaptersFromInfoPage` en page original (challenge) → 0 chapters
    i. Returns error: `only got 0/20 chapters via direct HTTP`
5. `browser_worker_fallback.go:29` → `s.HasBrowserWorker()`
    a. Si NO hay worker conectado → error: "HTTP fetch failed and no browser worker connected"
    b. Si HAY worker conectado → `getNovelInfoViaProxy(ctx, url, parser)`
6. `browser_worker_fallback.go:43` → crea ProxyHTTPClient, crea Downloader con proxy client
7. `browser_worker_fallback.go:49` → `parser.GetNovelInfo(ctx, proxyClient, url)`
    a. `proxyClient.Fetch()` → `EnqueueBrowserJob("fetch_page", url, ...)` → WebSocket
    b. Browser worker recibe job → `tryBackgroundFetch(url)`
    c. Si hay cf_clearance cookie → fetch exitoso → HTML devuelto
    d. Si hay Cloudflare challenge → `fetchViaChallengeTab` → hidden tab → espera resolución
8. HTML llega al parser → `getInfoFromInfoPage` con contenido real
    a. Extrae título, autor, descripción del HTML real
    b. `fetchChapterList` en catalog page `/book/{id}/` → LOGIN REQUERIDO incluso con proxy
    c. Si login requerido → página de redirect/login → selectors fallan → 0 chapters
    d. O si el catalog sí funciona → chapters extraídos y revertidos (newest first)
9. Resultado devuelto al frontend

### Flujo: Download job (procesar descarga batch)

1. `runtime_worker.go:227` → `processDownloadJob`
2. Busca parser con `dl.FindParser(opts.URL)` → encuentra 69shuba
3. `runtime_worker.go:267` → `hasWorker && IsBrowserRequiredSite` → proxy requerido
4. Crea `proxyDL = DownloaderFactoryWithClient(NewProxyHTTPClient(s))`
5. Por cada chapter: intenta `dl.DownloadChapters` (HTTP directo) primero
6. Si falla → `proxyDL.DownloadChapters` (vía WebSocket → Browser Worker)
7. El chapter se guarda en PocketBase via `UpsertChapterWithoutStats`

## Modelos de datos

- `NovelInfo` — título, autor, descripción, cover_url, source_url, chapters[]
- `ChapterURL` — url, title (para listado de capítulos)
- `Chapter` — title, content (HTML), markdown, source_url, index
- `BrowserWorkerResult` — jobId, status (ok/error), data (map)
- `ProxyFetchResult` — url, title, html, text, status
- `BrowserWorkerJobRequest` — jobId, operation, url, timeout, params

---

## Bugs encontrados y corregidos (merge conflict)

Al mergear `origin/main` en `feat/browser-worker-auth` (commit `c20ffa0`), se
resolvieron conflictos en `router_import.go` y `runtime_worker.go`. Durante la
resolución se introdujeron dos bugs en el flujo del browser worker proxy:

---

### Bug 1 (CRÍTICO): dispatchBrowserJob roba el resultado del canal — race condition

**Archivo:** `internal/api/router_browser_worker.go:315-329` (antes de la corrección)

`dispatchBrowserJob` y `EnqueueBrowserJob` leen del **mismo canal** (`job.Result`/`resultCh`).
Cuando `deliverBrowserJobResult` envía un valor al canal (buffer 1), solo UN lector lo recibe:

- Si `dispatchBrowserJob` gana → `EnqueueBrowserJob` recibe `ok=false` (canal cerrado) →
  error `ErrBrowserWorkerTimeout`
- Si `EnqueueBrowserJob` gana → `dispatchBrowserJob` se bloquea 5 minutos (timeout)
  → **toda la cola de browser workers se atasca**

**Código original (roto):**
```go
select {
case result := <-job.Result:   // LEE del mismo canal que EnqueueBrowserJob
    _ = result
case <-time.After(5 * time.Minute):
    job.Result <- &BrowserWorkerJobResult{Status: "error", ...}
}
close(job.Result)
```

**Corrección:** `dispatchBrowserJob` ya no lee de `job.Result`. Solo gestiona el timeout
y cierra el canal después del dispatch. `deliverBrowserJobResult` envía el resultado,
`EnqueueBrowserJob` lo recibe.
```go
select {
case <-time.After(5 * time.Minute):
    // solo timeout; no leer de job.Result
    job.Result <- &BrowserWorkerJobResult{Status: "error", ...}
}
close(job.Result)
```

**Síntoma:** Las descargas via browser worker fallan intermitentemente con "timeout" o
la cola entera se congela por 5 min. Esto explica por qué a veces funciona y a veces no.

---

### Bug 2 (ALTO): EnqueueBrowserJob usa len(browserWorkers) en vez de HasBrowserWorker()

**Archivo:** `internal/api/router_browser_worker.go:347` (antes de la corrección)

El early-return de `EnqueueBrowserJob` verificaba `len(browserWorkers) > 0`, que incluye
workers en estado "connected" o "unauthenticated". Pero `dispatchBrowserJob` solo acepta
workers con `State == "authenticated"`. Esto permitía encolar jobs que nadie podía procesar.

**Código original (roto):**
```go
browserWorkersMu.RLock()
hasWorker := len(browserWorkers) > 0   // incluye no-autenticados
browserWorkersMu.RUnlock()
```

**Corrección:**
```go
if !s.HasBrowserWorker() {  // solo workers autenticados
    return nil, ErrNoBrowserWorker
}
```

**Síntoma:** Si el browser worker está conectado pero no autenticado (OAuth no completado),
el server acepta el trabajo pero nadie lo procesa → el usuario ve "no data received".

---

### Nota sobre runtime_worker.go

El merge también cambió la lógica de descarga en `processDownloadJob`:
- **Antes:** `useProxy = IsBrowserRequiredSite && HasBrowserWorker` → usaba proxy DIRECTAMENTE
- **Ahora:** Primero intenta HTTP directo, si falla reintenta con proxy

Para 69shuba (Cloudflare) el HTTP directo siempre va a fallar, añadiendo ~30s de latencia
por capítulo, pero no rompe la funcionalidad.

---

### Cómo se originaron los bugs

El merge commit `c20ffa0` resolvió conflictos en `router_import.go` y `runtime_worker.go`.
Los cambios de `main` introdujeron mejoras en la deduplicación de capítulos y el campo `Order`
en `DownloadChapterInfo`. La resolución del merge fue correcta para esos archivos, pero los
bugs 1 y 2 existían desde `a001b42` (la introducción del auth).

Antes del merge, el flujo de descarga era: `useProxy → only proxy`. Este flujo solo hacía UNA
llamada por capítulo (via proxy), lo que reducía la probabilidad de que la race condition del Bug 1
se manifestara. Después del merge, el flujo cambió a: `try direct → retry proxy`, que hace DOS
llamadas (direct + proxy) con más oportunidades para que la race condition ocurra.

Además, el Bug 2 (check de workers no autenticados) empeoró porque el nuevo flujo de importación
desde URL ahora puede hacer más llamadas al browser worker (preview + download first chapter +
download remaining), exponiendo más el problema.

---

## Análisis del parser 69shuba — libro 59083

### Resumen del problema

El parser para `https://www.69shuba.com/book/59083.htm` no está funcionando.
Los síntomas más probables se enumeran a continuación, ordenados por probabilidad.

### 🟡 ALTA: Selectores CSS pueden estar obsoletos (sin login)

**Archivos:** `internal/noveldownloader/69shuba_metadata.go:98-113` y `69shuba_metadata.go:233-267`

69shuba **NO requiere login** para acceder al catálogo completo. Sin embargo, el
CSS de la página del catálogo (`/book/{id}/`) puede haber cambiado desde que se
escribió el parser. La función `fetchChapterList` intenta estos selectores en orden:

```
#catalog ul li a
div.catalog ul li a
ul.chapter-list li a
.listmain li a
#list li a
.booklist li a
.volume li a
.qustime li a
```

Si 69shuba cambió su estructura HTML, ninguno de estos selectores matcheará.
El fallback busca cualquier link que contenga `/txt/`, `/chapter/`, o `/read/`
en el href. Si la clase o estructura cambió, el fallback tampoco encuentra nada.

**Verificar:**
- Abrir `https://www.69shuba.com/book/59083/` y ver qué selector CSS contiene la lista
- Si la clase cambió (e.g. `.chapter-list` → `.catalog-list`), actualizar el array
- Si no hay capítulos, ver el HTML que devuelve el proxy (no el browser directo)

**Datos:** La función `extractChaptersFromInfoPage` intenta `.qustime ul li a` en
la info page (`/book/59083.htm`). El fallback general busca cualquier `<a>` con href
que contenga `/txt/`, `/chapter/`, o `/read/`.

### 🔴 CRÍTICO: Cloudflare bloquea HTTP directo

**Archivo:** `internal/noveldownloader/client.go:48-76`

El HTTP client por defecto (`NewHTTPClient`) no tiene manejo de cookies,
no resuelve Cloudflare challenges, y tiene timeout de 30s. 69shuba.com está
detrás de Cloudflare, por lo que cualquier request HTTP directo devuelve una
página de challenge, no el contenido real.

El flujo de fallback (`getNovelInfoWithFallback`) intenta HTTP directo primero,
y sólo si falla usa el proxy. Esto significa **siempre** hay un round-trip
fallido antes de usar el proxy, lo que añade ~30s de latencia.

**Si no hay browser worker conectado**, el parser falla definitivamente:
no hay manera de obtener contenido de 69shuba sin el proxy.

### 🟡 ALTA: Selectores CSS pueden estar obsoletos

**Archivo:** `internal/noveldownloader/69shuba_metadata.go:233-267`

La función `fetchChapterList` intenta estos selectores en orden:

```
#catalog ul li a
div.catalog ul li a
ul.chapter-list li a
.listmain li a
#list li a
.booklist li a
.volume li a
.qustime li a
```

69shuba puede haber cambiado su estructura HTML. Si ningún selector matchea,
se usa un fallback que busca cualquier link que contenga `/txt/`, `/chapter/`,
o `/read/` en el href.

**Verificar:**
- Abrir `https://www.69shuba.com/book/59083/` (con login) e inspeccionar HTML
- Ver qué selector CSS realmente contiene la lista de capítulos
- Si la estructura cambió, actualizar el array de selectores

### 🟡 ALTA: extractChaptersFromInfoPage selector obsoleto

**Archivo:** `internal/noveldownloader/69shuba_metadata.go:174-206`

El fallback `extractChaptersFromInfoPage` usa el selector `.qustime ul li a`.
Este selector busca capítulos en la info page (página principal del libro, no
el catálogo completo). 69shuba puede haber cambiado la clase CSS.

**Verificar:**
- Abrir `https://www.69shuba.com/book/59083.htm` sin login
- Verificar si `.qustime` existe en el DOM
- Si no existe, actualizar el selector

### 🟡 ALTA: getChapterContent no encuentra content container

**Archivo:** `internal/noveldownloader/69shuba_chapters.go:36-43`

El extractor de contenido de capítulo busca `.txtnav` o `#content`. Si 69shuba
cambió el nombre de clase/ID del contenedor de contenido, el parser devuelve:
`69shuba: no content found at {url}`

**Verificar:**
- Abrir `https://www.69shuba.com/txt/59083/{chapterNum}.html`
- Ver cómo se llama el contenedor del contenido del capítulo

### 🟡 MEDIA: ensureChapterExtension puede ser incorrecta

**Archivo:** `internal/noveldownloader/69shuba_metadata.go:323-328`

```go
func ensureChapterExtension(url string) string {
    if strings.Contains(url, "/txt/") && !strings.HasSuffix(url, ".html") {
        return url + ".html"
    }
    return url
}
```

Asume que las URLs de capítulos contienen `/txt/`. Si 69shuba cambió el patrón
de URL (e.g., a `/book/59083/chapter/123` o similar), esta función no añadirá
la extensión `.html` correctamente, resultando en URLs inválidas.

**Verificar:**
- Inspeccionar los hrefs de los links de capítulos en el HTML
- Ver si contienen `/txt/` o usan otro patrón

### 🟡 MEDIA: Regex de URL pueden no matchear

**Archivo:** `internal/noveldownloader/69shuba.go:12-14`

```go
sixtyNineShubaInfoRe    = regexp.MustCompile(`69shuba\.com/book/(\d+)\.htm`)
sixtyNineShubaChapsRe   = regexp.MustCompile(`69shuba\.com/book/(\d+)/?$`)
sixtyNineShubaChapterRe = regexp.MustCompile(`69shuba\.com/txt/(\d+)/(\d+)`)
```

`sixtyNineShubaChapterRe` espera `/txt/{bookID}/{chapterID}`. Si el formato
real de las URLs de capítulos es diferente, `GetNovelInfo` llamará a
`getInfoFromInfoPage` en lugar de `getInfoFromChapter`, lo cual está bien
porque `getInfoFromInfoPage` extrae el bookID de otros regex.

Sin embargo, `extract69ShubaBookID` también usa estos regex. Si bookID no se
puede extraer, el parser no puede construir la URL del catálogo.

### 🟢 BAJA: GBK decoding duplicado

**Archivos:** `internal/noveldownloader/client.go:167-169` y `69shuba_*.go`

`client.Fetch()` ya llama a `decodeChineseCharset()` internamente. Luego los
parsers (e.g. `getInfoFromInfoPage`, `getChapterContent`) llaman a `DecodeHTMLBody()`
sobre el resultado. `DecodeHTMLBody` a su vez llama a `decodeChineseCharset()`.
Si el contenido ya es UTF-8 válido (porque ya fue decodificado), la segunda
llamada es un no-op (early return por `utf8.Valid(raw)`). No es un bug, pero
es código redundante.

### 🟢 BAJA: Double HTTP call on fallback siempre falla

**Archivo:** `internal/api/browser_worker_fallback.go:15-39`

`getNovelInfoWithFallback` siempre intenta HTTP directo primero, incluso para
sitios que están en `BrowserRequiredSites`. Para 69shuba esto es garantizado
que va a fallar (Cloudflare). Después del fallo, intenta con el proxy.

Esto no es un bug de funcionalidad (el fallback funciona), pero añade ~30s de
latencia a cada operación de preview/import. Una optimización sería: si
`IsBrowserRequiredSite(url)` es true y hay browser worker, ir directamente
al proxy sin intentar HTTP directo.

---

## Decisiones arquitectónicas

### Proxy reusa parsers Go existentes

El proxy no implementa lógica de parsing en JS. En lugar de eso, el service
worker de Chrome sólo hace fetch de la página HTML y lo devuelve al servidor
Go. Los parsers Go existentes (69shuba, novelfire, etc.) procesan el HTML
como si hubiera sido obtenido por HTTP directo.

Ventaja: toda la lógica de parsing está en Go, fácil de mantener y testear.
Desventaja: el proxy debe devolver HTML exactamente como lo serviría el sitio
(incluyendo charset correcto), o los parsers Go pueden no reconocerlo.

### BrowserRequiredSites como flag de site-level

El map `BrowserRequiredSites` es la única fuente de verdad para decidir si
un sitio necesita proxy. Actualmente sólo tiene `69shuba.com`. Si se añaden
más sitios Cloudflare, hay que agregarlos aquí.

Nota: `IsBrowserRequiredSite` se llama desde `runtime_worker.go:267` para
decidir si crear un proxyDL incluso cuando hay parser disponible.

### Cola FIFO de browser jobs

Todos los jobs del browser worker pasan por un solo canal (`browserQueue`
con buffer 64) y una sola goroutine (`processBrowserJobs`). Esto garantiza
que sólo se procesa una página a la vez, crítico para Cloudflare (una página
por sesión).

Cada job tiene timeout de 5 minutos (para resolución de Cloudflare challenge).
Si el browser worker se desconecta mientras hay jobs pendientes, se notifica
error a todos los callers.

---

## HTTP Router — route registration

**Archivo:** `internal/api/router.go`

The main Router() and registerRoutes() functions that wire the /ws/browser-worker WebSocket, /api/proxy/* REST endpoints, /api/browser-workers status, and /api/worker-auth/* OAuth routes into the HTTP mux. Also defines the Server struct with the browserWorkerResultCh channel.


```go
package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/config"
	"translator-server/internal/noveldownloader"
	"translator-server/internal/store"
)

// BrowserJob is a browser worker request waiting to be dispatched.
// The Result channel is written to by the consumer goroutine and read
// by the caller that originally enqueued the job.
type BrowserJob struct {
	Request BrowserWorkerJobRequest
	Result  chan *BrowserWorkerJobResult
	UserID  string
}

type Server struct {
	Store                  *store.Store
	Cfg                    *config.Config
	downloadQueue          chan string
	translateQueue         chan string
	queuedJobs             map[string]struct{}
	queueMu                sync.Mutex
	cancelMu               sync.Mutex
	jobCancels             map[string]context.CancelFunc
	DownloaderFactory      func() *noveldownloader.Downloader
	previewCacheMu         sync.RWMutex
	previewCache           map[string]previewCacheEntry
	browserQueue           chan BrowserJob
	pendingBrowserJobs     map[string]chan *BrowserWorkerJobResult
	pendingBrowserJobsMu   sync.Mutex
}

func New(st *store.Store, cfg *config.Config) *Server {
	s := &Server{
		Store:              st,
		Cfg:                cfg,
		queuedJobs:         map[string]struct{}{},
		jobCancels:         map[string]context.CancelFunc{},
		previewCache:       make(map[string]previewCacheEntry),
		browserQueue:       make(chan BrowserJob, 64),
		pendingBrowserJobs: make(map[string]chan *BrowserWorkerJobResult),
	}
	s.DownloaderFactory = func() *noveldownloader.Downloader {
		dl := noveldownloader.NewDownloader()
		if cfg != nil {
			if cfg.DownloadMinDelayMs > 0 {
				dl.MinChapterDelay = time.Duration(cfg.DownloadMinDelayMs) * time.Millisecond
			}
			if cfg.DownloadMaxDelayMs > 0 {
				dl.MaxChapterDelay = time.Duration(cfg.DownloadMaxDelayMs) * time.Millisecond
			}
		}
		return dl
	}
	s.startJobWorker()
	go s.processBrowserJobs()
	return s
}

func (s *Server) registerJobCancel(jobID string, cancel context.CancelFunc) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.jobCancels[jobID] = cancel
}

func (s *Server) unregisterJobCancel(jobID string) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	delete(s.jobCancels, jobID)
}

func (s *Server) cancelJob(jobID string) {
	s.cancelMu.Lock()
	cancel := s.jobCancels[jobID]
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func Router(s *Server) http.Handler {
	router, err := apis.NewRouter(s.Store.App)
	if err != nil {
		panic(err)
	}
	registerRoutes(router, s)
	mux, err := router.BuildMux()
	if err != nil {
		panic(err)
	}
	return mux
}

func registerRoutes(router *pbrouter.Router[*core.RequestEvent], s *Server) {
	router.GET("/healthz", func(e *core.RequestEvent) error {
		return e.JSON(http.StatusOK, map[string]any{"ok": true})
	})

	router.GET("/api/browser-workers", func(e *core.RequestEvent) error {
		browserWorkersMu.RLock()
		workers := make([]map[string]any, 0, len(browserWorkers))
		for _, w := range browserWorkers {
			w.mu.Lock()
			workers = append(workers, map[string]any{
				"id":            w.ID,
				"browser":       w.Browser,
				"version":       w.Version,
				"state":         w.State,
				"capabilities":  w.Capabilities,
				"connectedAt":   w.ConnectedAt,
				"lastHeartbeat": w.LastHeartbeat,
			})
			w.mu.Unlock()
		}
		browserWorkersMu.RUnlock()
		return e.JSON(http.StatusOK, map[string]any{
			"count":   len(workers),
			"workers": workers,
		})
	})

	router.GET("/api/proxy/status", func(e *core.RequestEvent) error {
		browserWorkersMu.RLock()
		workers := make([]map[string]any, 0, len(browserWorkers))
		for _, w := range browserWorkers {
			w.mu.Lock()
			workers = append(workers, map[string]any{
				"id":          w.ID,
				"browser":     w.Browser,
				"state":       w.State,
				"connectedAt": w.ConnectedAt,
			})
			w.mu.Unlock()
		}
		browserWorkersMu.RUnlock()
		return e.JSON(http.StatusOK, map[string]any{
			"connected": len(workers) > 0,
			"count":     len(workers),
			"workers":   workers,
		})
	})

	router.POST("/api/proxy/fetch", func(e *core.RequestEvent) error {
		body := struct {
			URL     string `json:"url"`
			Timeout int    `json:"timeout"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if strings.TrimSpace(body.URL) == "" {
			return e.BadRequestError("url is required", nil)
		}
		timeout := body.Timeout
		if timeout <= 0 {
			timeout = 120
		}
		if timeout > 300 {
			timeout = 300
		}

		if !s.HasBrowserWorker() {
			return e.BadRequestError("no browser worker connected", nil)
		}

		result, err := s.fetchViaBrowserWorker(body.URL, timeout, "")
		if err != nil {
			if err == ErrBrowserWorkerTimeout {
				return e.BadRequestError("timeout waiting for browser worker", nil)
			}
			return e.InternalServerError("fetch failed", err)
		}

		return e.JSON(http.StatusOK, map[string]any{
			"url":    result.URL,
			"title":  result.Title,
			"html":   result.HTML,
			"text":   result.Text,
			"status": result.Status,
		})
	})

	router.GET("/ws/browser-worker", func(e *core.RequestEvent) error {
		s.handleBrowserWorkerWS(e.Response, e.Request)
		return nil
	})

	registerAuthRoutes(router, s)
	registerWorkerAuthPublicRoutes(router, s)
	registerProtectedRoutes(router, s)
	registerStaticHandler(router, s.Cfg.StaticDir)
}

func registerProtectedRoutes(router *pbrouter.Router[*core.RequestEvent], s *Server) {
	api := router.Group("/api")
	api.Bind(loadAuthFromCookie())
	api.Bind(apis.RequireAuth())

	registerWorkerAuthProtectedRoutes(api, s)
	registerSettingsRoutes(api, s)
	registerProviderRoutes(api, s)
	registerPromptRoutes(api, s)
	registerImportRoutes(api, s)
	registerNovelRoutes(api, s)
	registerChapterRoutes(api, s)
	registerJobRoutes(api, s)
	registerEpubRoutes(api, s)
	registerEpubExportRoutes(api, s)
	registerReadingProgressRoutes(api, s)
	registerBackupRoutes(api, s)
}

```

---

## WebSocket handler — BrowserWorker lifecycle

**Archivo:** `internal/api/router_browser_worker.go`

WebSocket upgrade, message dispatch (register/auth, heartbeat, job_result, pong), BrowserWorker struct, SendJobToBrowserWorker which sends a job request over WS and waits for the result, HasBrowserWorker/GetBrowserWorkerCount helpers, ID generation.


```go
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:    func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type BrowserWorker struct {
	ID            string
	Conn          *websocket.Conn
	Browser       string
	Capabilities  []string
	Version       string
	State         string
	UserID        string
	TokenID       string
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	mu            sync.Mutex
}

type BrowserWorkerMessage struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"timestamp"`
}

type BrowserWorkerJobRequest struct {
	JobID     string                 `json:"jobId"`
	Operation string                 `json:"operation"`
	URL       string                 `json:"url"`
	Timeout   int                    `json:"timeout,omitempty"`
	Params    map[string]interface{} `json:"params"`
}

type BrowserWorkerJobResult struct {
	JobID  string                 `json:"jobId"`
	Status string                 `json:"status"`
	Data   map[string]interface{} `json:"data"`
}

var (
	browserWorkers   = make(map[string]*BrowserWorker)
	browserWorkersMu sync.RWMutex
)

// ── WebSocket handler ──────────────────────────────────────────────────────

func (s *Server) handleBrowserWorkerWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("browser worker websocket upgrade", "error", err)
		return
	}

	worker := &BrowserWorker{
		ID:          generateWorkerID(),
		Conn:        conn,
		State:       "connected",
		ConnectedAt: time.Now(),
	}

	browserWorkersMu.Lock()
	browserWorkers[worker.ID] = worker
	browserWorkersMu.Unlock()

	slog.Info("browser worker connected", "workerId", worker.ID, "remote", r.RemoteAddr)

	defer func() {
		browserWorkersMu.Lock()
		delete(browserWorkers, worker.ID)
		browserWorkersMu.Unlock()
		conn.Close()
		slog.Info("browser worker disconnected", "workerId", worker.ID)
	}()

	conn.SetReadLimit(10 * 1024 * 1024)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		worker.mu.Lock()
		worker.LastHeartbeat = time.Now()
		worker.mu.Unlock()
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				worker.mu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				worker.mu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Error("browser worker read error", "workerId", worker.ID, "error", err)
			}
			break
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var msg BrowserWorkerMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			slog.Error("browser worker invalid message", "workerId", worker.ID, "error", err)
			continue
		}

		s.handleWorkerMessage(worker, &msg)
	}
}

// ── Message dispatch ───────────────────────────────────────────────────────

func (s *Server) handleWorkerMessage(worker *BrowserWorker, msg *BrowserWorkerMessage) {
	switch msg.Type {
	case "register":
		var payload struct {
			Browser      map[string]string `json:"browser"`
			Capabilities []string          `json:"capabilities"`
			Version      string            `json:"version"`
			Token        string            `json:"token"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			slog.Error("browser worker invalid register payload", "workerId", worker.ID, "error", err)
			s.sendWorkerMessage(worker, "register_response", map[string]interface{}{
				"ok":    false,
				"error": "invalid payload",
			})
			return
		}

		if payload.Token == "" {
			slog.Warn("browser worker registered without token", "workerId", worker.ID)
			worker.mu.Lock()
			worker.State = "unauthenticated"
			worker.mu.Unlock()
			s.sendWorkerMessage(worker, "register_response", map[string]interface{}{
				"ok":    false,
				"error": "no token provided",
			})
			return
		}

		validated, err := s.Store.ValidateWorkerToken(payload.Token)
		if err != nil {
			slog.Warn("browser worker invalid token", "workerId", worker.ID, "error", err)
			worker.mu.Lock()
			worker.State = "unauthenticated"
			worker.mu.Unlock()
			s.sendWorkerMessage(worker, "register_response", map[string]interface{}{
				"ok":    false,
				"error": "invalid token",
			})
			return
		}

		worker.mu.Lock()
		if payload.Browser != nil {
			worker.Browser = payload.Browser["name"]
		}
		worker.Capabilities = payload.Capabilities
		worker.Version = payload.Version
		worker.UserID = validated.UserID
		worker.TokenID = validated.ID
		worker.State = "authenticated"
		worker.mu.Unlock()

		slog.Info("browser worker registered and authenticated",
			"workerId", worker.ID,
			"browser", worker.Browser,
			"version", worker.Version,
			"userId", validated.UserID)

		s.sendWorkerMessage(worker, "register_response", map[string]interface{}{
			"ok":     true,
			"userId": validated.UserID,
		})

	case "job_result":
		var result BrowserWorkerJobResult
		if err := json.Unmarshal(msg.Payload, &result); err != nil {
			slog.Error("browser worker invalid job result", "workerId", worker.ID, "error", err)
			return
		}
		s.deliverBrowserJobResult(worker, &result)
	}
}

// ── Per-job result delivery ────────────────────────────────────────────────

// deliverBrowserWorkerResult writes a job result to the per-job channel
// registered in pendingBrowserJobs. If the caller has already timed out
// (channel closed), the result is silently dropped.
func (s *Server) deliverBrowserJobResult(worker *BrowserWorker, result *BrowserWorkerJobResult) {
	slog.Info("browser worker job result",
		"workerId", worker.ID,
		"jobId", result.JobID,
		"status", result.Status)

	s.pendingBrowserJobsMu.Lock()
	ch, ok := s.pendingBrowserJobs[result.JobID]
	s.pendingBrowserJobsMu.Unlock()

	if !ok {
		slog.Warn("browser worker result for unknown job, dropping", "jobId", result.JobID)
		return
	}

	// Non-blocking send: if the caller timed out and closed the channel,
	// this drops the result silently instead of blocking.
	select {
	case ch <- result:
	default:
		slog.Warn("browser worker result channel closed (caller timed out), dropping", "jobId", result.JobID)
	}
}

// ── FIFO consumer goroutine ────────────────────────────────────────────────

// processBrowserJobs runs in its own goroutine and is the sole writer to
// browser WebSocket connections. This guarantees strict sequentiality:
// all requests go through one goroutine, one at a time.
//
// Per-job result channels are stored in pendingBrowserJobs so that
// callers receive only their own result. Each job is given a 5-minute
// timeout (covers Cloudflare challenge resolution).
func (s *Server) processBrowserJobs() {
	slog.Info("browser job worker started")
	for job := range s.browserQueue {
		s.dispatchBrowserJob(job)
	}
	slog.Info("browser job worker stopped")
}

func (s *Server) dispatchBrowserJob(job BrowserJob) {
	browserWorkersMu.RLock()
	var worker *BrowserWorker
	for _, w := range browserWorkers {
		if w.Conn == nil || w.State != "authenticated" {
			continue
		}
		if job.UserID != "" && w.UserID != job.UserID {
			continue
		}
		worker = w
		break
	}
	browserWorkersMu.RUnlock()

	if worker == nil {
		slog.Warn("no browser worker available for job", "jobId", job.Request.JobID)
		job.Result <- &BrowserWorkerJobResult{
			JobID:  job.Request.JobID,
			Status: "error",
			Data:   map[string]interface{}{"error": "no browser worker connected"},
		}
		close(job.Result)
		return
	}

	payload, _ := json.Marshal(job.Request)
	msg := BrowserWorkerMessage{
		Type:      "job_request",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}

	worker.mu.Lock()
	err := worker.Conn.WriteJSON(msg)
	worker.mu.Unlock()

	if err != nil {
		slog.Error("failed to send job to browser worker",
			"workerId", worker.ID, "jobId", job.Request.JobID, "error", err)
		job.Result <- &BrowserWorkerJobResult{
			JobID:  job.Request.JobID,
			Status: "error",
			Data:   map[string]interface{}{"error": err.Error()},
		}
		close(job.Result)
		return
	}

	slog.Info("dispatched job to browser worker",
		"workerId", worker.ID,
		"jobId", job.Request.JobID,
		"operation", job.Request.Operation,
		"url", job.Request.URL)

	// Don't read from job.Result here — EnqueueBrowserJob reads from it.
	// We only manage the timeout and close the channel after dispatch.
	// deliverBrowserJobResult sends the actual result to the same channel.
	select {
	case <-time.After(5 * time.Minute):
		slog.Warn("browser worker job timed out", "jobId", job.Request.JobID)
		select {
		case job.Result <- &BrowserWorkerJobResult{
			JobID:  job.Request.JobID,
			Status: "error",
			Data:   map[string]interface{}{"error": "browser worker job timed out (5m)"},
		}:
		default:
		}
	}
	close(job.Result)
}

// ── Public enqueue API ─────────────────────────────────────────────────────

// EnqueueBrowserJob sends a job request to the browser worker queue and
// waits for the result. All callers are serialized through a single
// goroutine, which guarantees sequential execution — critical for
// Cloudflare challenge handling where only one page can be loaded at a time.
//
// The caller blocks until a result arrives or the consumer goroutine
// times out (5 minutes). The channel is always closed after dispatch.
func (s *Server) EnqueueBrowserJob(operation, url string, params map[string]interface{}, userID string) (*BrowserWorkerJobResult, error) {
	s.pendingBrowserJobsMu.Lock()
	if len(s.pendingBrowserJobs) == 0 && len(s.browserQueue) == 0 {
		s.pendingBrowserJobsMu.Unlock()
		if !s.HasBrowserWorker() {
			return nil, ErrNoBrowserWorker
		}
	} else {
		s.pendingBrowserJobsMu.Unlock()
	}

	jobID := generateJobID()
	resultCh := make(chan *BrowserWorkerJobResult, 1)

	req := BrowserWorkerJobRequest{
		JobID:     jobID,
		Operation: operation,
		URL:       url,
		Params:    params,
	}

	s.pendingBrowserJobsMu.Lock()
	s.pendingBrowserJobs[jobID] = resultCh
	s.pendingBrowserJobsMu.Unlock()

	defer func() {
		s.pendingBrowserJobsMu.Lock()
		delete(s.pendingBrowserJobs, jobID)
		s.pendingBrowserJobsMu.Unlock()
	}()

	s.browserQueue <- BrowserJob{Request: req, Result: resultCh, UserID: userID}

	result, ok := <-resultCh
	if !ok {
		return nil, ErrBrowserWorkerTimeout
	}

	if result.Status != "ok" {
		errMsg := "browser worker error: " + result.Status
		if e, ok := result.Data["error"].(string); ok {
			errMsg = e
		}
		return nil, &BrowserWorkerError{msg: errMsg}
	}

	return result, nil
}

// ── Helpers ────────────────────────────────────────────────────────────────

func (s *Server) HasBrowserWorker() bool {
	browserWorkersMu.RLock()
	defer browserWorkersMu.RUnlock()
	for _, w := range browserWorkers {
		if w.State == "authenticated" {
			return true
		}
	}
	return false
}

func GetBrowserWorkerCount() int {
	browserWorkersMu.RLock()
	defer browserWorkersMu.RUnlock()
	return len(browserWorkers)
}

func CloseWorkerByTokenID(tokenID string) {
	browserWorkersMu.Lock()
	defer browserWorkersMu.Unlock()
	for _, w := range browserWorkers {
		if w.TokenID == tokenID {
			w.Conn.Close()
			delete(browserWorkers, w.ID)
			slog.Info("closed worker after token revocation", "workerId", w.ID, "tokenID", tokenID)
			return
		}
	}
}

func (s *Server) sendWorkerMessage(worker *BrowserWorker, msgType string, payload interface{}) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.Conn == nil {
		return
	}
	msg := BrowserWorkerMessage{
		Type:      msgType,
		Timestamp: time.Now().UnixMilli(),
	}
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			slog.Error("failed to marshal worker message payload", "error", err)
			return
		}
		msg.Payload = data
	}
	if err := worker.Conn.WriteJSON(msg); err != nil {
		slog.Error("failed to send message to worker", "workerId", worker.ID, "type", msgType, "error", err)
	}
}

func generateWorkerID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "bw-" + hex.EncodeToString(b)
}

func generateJobID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "bj-" + hex.EncodeToString(b)
}

var (
	ErrNoBrowserWorker    = &BrowserWorkerError{"no browser worker connected"}
	ErrBrowserWorkerTimeout = &BrowserWorkerError{"browser worker response timeout"}
)

type BrowserWorkerError struct {
	msg string
}

func (e *BrowserWorkerError) Error() string {
	return e.msg
}

```

---

## HTTP fetch proxy — REST-facing fetch through WebSocket

**Archivo:** `internal/api/router_proxy.go`

fetchViaBrowserWorker — sends a fetch_page job over WebSocket and polls for the result. Returns ProxyFetchResult with HTML/Text/Title. Also defines an (unused) registerProxyRoutes handler.


```go
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
)

// fetchViaBrowserWorker sends a fetch_page job through the browser job queue.
// All requests are serialized (FIFO) through a single goroutine, which
// guarantees sequential page loads — critical for Cloudflare session handling.
func (s *Server) fetchViaBrowserWorker(url string, timeoutSec int, userID string) (*ProxyFetchResult, error) {
	params := map[string]interface{}{
		"timeout": timeoutSec,
	}
	result, err := s.EnqueueBrowserJob("fetch_page", url, params, userID)
	if err != nil {
		return nil, err
	}
	return &ProxyFetchResult{
		URL:    url,
		Title:  getStringFromData(result.Data, "title"),
		HTML:   getStringFromData(result.Data, "html"),
		Text:   getStringFromData(result.Data, "text"),
		Status: "ok",
	}, nil
}

// registerProxyRoutes registers the raw HTML proxy endpoint.
func registerProxyRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/proxy/fetch", func(e *core.RequestEvent) error {
		body := struct {
			URL     string `json:"url"`
			Timeout int    `json:"timeout"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if strings.TrimSpace(body.URL) == "" {
			return e.BadRequestError("url is required", nil)
		}
		timeout := body.Timeout
		if timeout <= 0 {
			timeout = 120
		}
		if timeout > 300 {
			timeout = 300
		}

		slog.Info("proxy fetch request", "url", body.URL, "timeout", timeout)

		if !s.HasBrowserWorker() {
			return e.BadRequestError("no browser worker connected", nil)
		}

		result, err := s.fetchViaBrowserWorker(body.URL, timeout, e.Auth.Id)
		if err != nil {
			if err == ErrBrowserWorkerTimeout {
				return e.BadRequestError("timeout waiting for browser worker", nil)
			}
			return e.InternalServerError("fetch failed", err)
		}

		return e.JSON(http.StatusOK, map[string]any{
			"url":    result.URL,
			"title":  result.Title,
			"html":   result.HTML,
			"text":   result.Text,
			"status": result.Status,
		})
	})
}

type ProxyFetchResult struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	HTML   string `json:"html"`
	Text   string `json:"text"`
	Status string `json:"status"`
}

func getStringFromData(data map[string]interface{}, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

// Ensure unused imports are referenced.
var _ = json.Marshal
var _ = time.Now

```

---

## ProxyHTTPClient — HTTPClient adapter for Go parsers

**Archivo:** `internal/api/proxy_http_client.go`

Implements noveldownloader.HTTPClient (Fetch, FetchDocument, Do) by routing through the browser worker WebSocket. This is what lets the existing Go parsers work unmodified on Cloudflare-protected sites.


```go
package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ProxyHTTPClient implements noveldownloader.HTTPClient by fetching pages
// through the Browser Worker extension. This allows the Go parsers to work
// on Cloudflare-protected sites without any site-specific JS in the extension.
//
// All calls are serialized through the browser job queue (EnqueueBrowserJob),
// which guarantees sequential execution — critical for Cloudflare challenge
// handling.
type ProxyHTTPClient struct {
	server *Server
}

func NewProxyHTTPClient(s *Server) *ProxyHTTPClient {
	return &ProxyHTTPClient{server: s}
}

func (c *ProxyHTTPClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	result, err := c.server.EnqueueBrowserJob("fetch_page", url, nil, "")
	if err != nil {
		return nil, fmt.Errorf("proxy fetch: %w", err)
	}
	html := getStringFromData(result.Data, "html")
	if html == "" {
		return nil, fmt.Errorf("proxy returned empty HTML for %s", url)
	}
	return []byte(html), nil
}

func (c *ProxyHTTPClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}
	return doc, nil
}

func (c *ProxyHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, err := c.Fetch(req.Context(), req.URL.String())
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     make(http.Header),
	}, nil
}

// Ensure interface compliance at compile time.
var _ interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
	FetchDocument(ctx context.Context, url string) (*goquery.Document, error)
	Do(req *http.Request) (*http.Response, error)
} = (*ProxyHTTPClient)(nil)

```

---

## Fallback orchestration — direct HTTP → proxy

**Archivo:** `internal/api/browser_worker_fallback.go`

getNovelInfoWithFallback tries Go parsers with direct HTTP first; if the site requires a browser (Cloudflare) or direct HTTP fails, falls back to getNovelInfoViaProxy which reuses the same Go parsers but through the ProxyHTTPClient.


**⚠️ Problema conocido:**
- Siempre intenta HTTP directo primero (garantizado a fallar para sitios Cloudflare),
  añadiendo ~30s de latencia. Podría checkear `IsBrowserRequiredSite` primero.


```go
package api

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"translator-server/internal/noveldownloader"
)

// getNovelInfoWithFallback tries Go parsers first (with normal HTTP).
// If the site requires a browser (Cloudflare), it falls back to fetching
// the page through the Browser Worker proxy and then parsing with Go.
func (s *Server) getNovelInfoWithFallback(ctx context.Context, url string) (*noveldownloader.NovelInfo, error) {
	dl := s.DownloaderFactory()

	// 1. Try the Go parser with normal HTTP first
	parser := dl.FindParser(url)
	if parser != nil {
		slog.Info("found HTTP parser, trying direct fetch", "parser", parser.Name(), "url", url)
		info, err := dl.GetNovelInfo(ctx, url)
		if err == nil {
			return info, nil
		}
		slog.Info("direct HTTP failed, will try browser proxy", "error", err)
	}

	// 2. If no parser or HTTP failed, try via browser proxy
	if !s.HasBrowserWorker() {
		if parser != nil {
			return nil, fmt.Errorf("HTTP fetch failed and no browser worker connected")
		}
		return nil, fmt.Errorf("unsupported URL: no parser found and no browser worker connected")
	}

	slog.Info("fetching via browser proxy", "url", url)
	return s.getNovelInfoViaProxy(ctx, url, parser)
}

// getNovelInfoViaProxy fetches the page HTML through the browser worker,
// then parses it with the same Go parsers used for direct HTTP.
func (s *Server) getNovelInfoViaProxy(ctx context.Context, url string, parser noveldownloader.Parser) (*noveldownloader.NovelInfo, error) {
	proxyClient := NewProxyHTTPClient(s)
	dl := s.DownloaderFactoryWithClient(proxyClient)

	// If we have a parser, use it
	if parser != nil {
		info, err := parser.GetNovelInfo(ctx, proxyClient, url)
		if err != nil {
			return nil, fmt.Errorf("parser %s failed via proxy: %w", parser.Name(), err)
		}
		slog.Info("proxy fetch + parse succeeded", "parser", parser.Name(), "title", info.Title, "chapters", len(info.Chapters))
		return info, nil
	}

	// No parser known - try all parsers with the proxy HTML
	info, err := dl.GetNovelInfo(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("no parser could handle %s via proxy: %w", url, err)
	}
	return info, nil
}

// DownloaderFactoryWithClient creates a Downloader with a custom HTTP client.
func (s *Server) DownloaderFactoryWithClient(client noveldownloader.HTTPClient) *noveldownloader.Downloader {
	dl := noveldownloader.NewDownloaderWithClient(client)
	if s.Cfg != nil {
		if s.Cfg.DownloadMinDelayMs > 0 {
			dl.MinChapterDelay = time.Duration(s.Cfg.DownloadMinDelayMs) * time.Millisecond
		}
		if s.Cfg.DownloadMaxDelayMs > 0 {
			dl.MaxChapterDelay = time.Duration(s.Cfg.DownloadMaxDelayMs) * time.Millisecond
		}
	}
	return dl
}

```

---

## Worker auth — OAuth2 consent & token management

**Archivo:** `internal/api/router_worker_auth.go`

registerWorkerAuthPublicRoutes (/authorize, /validate) and registerWorkerAuthProtectedRoutes (/approve, /revoke/{id}, /delete/{id}, /tokens). Includes consent page and approval success HTML templates.


```go
package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
)

type pendingAuth struct {
	ExtensionID string
	UserID      string
	State       string
	CreatedAt   time.Time
}

var (
	pendingAuths   = make(map[string]*pendingAuth)
	pendingAuthsMu sync.Mutex
)

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func registerWorkerAuthPublicRoutes(router *pbrouter.Router[*core.RequestEvent], s *Server) {
	router.GET("/api/worker-auth/authorize", func(e *core.RequestEvent) error {
		extensionID := e.Request.URL.Query().Get("extension_id")
		if extensionID == "" {
			return e.BadRequestError("extension_id is required", nil)
		}

		cookie, err := e.Request.Cookie(authCookieName)
		if err != nil || cookie.Value == "" {
			return e.HTML(http.StatusOK, loginRequiredHTML())
		}
		if _, err := e.App.FindAuthRecordByToken(cookie.Value, core.TokenTypeAuth); err != nil {
			return e.HTML(http.StatusOK, loginRequiredHTML())
		}

		state := generateState()
		pendingAuthsMu.Lock()
		pendingAuths[state] = &pendingAuth{
			ExtensionID: extensionID,
			State:       state,
			CreatedAt:   time.Now(),
		}
		pendingAuthsMu.Unlock()

		page := consentPageHTML(extensionID, state)
		e.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		e.Response.WriteHeader(http.StatusOK)
		e.Response.Write([]byte(page))
		return nil
	})

	router.GET("/api/worker-auth/validate", func(e *core.RequestEvent) error {
		token := e.Request.Header.Get("Authorization")
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
		if token == "" {
			token = e.Request.URL.Query().Get("token")
		}
		if token == "" {
			return e.BadRequestError("token required", nil)
		}

		validated, err := s.Store.ValidateWorkerToken(token)
		if err != nil {
			return e.UnauthorizedError("invalid token", err)
		}

		return e.JSON(http.StatusOK, map[string]any{
			"valid":       true,
			"userId":      validated.UserID,
			"extensionId": validated.ExtensionID,
			"label":       validated.Label,
		})
	})

	router.GET("/api/worker-auth/callback", func(e *core.RequestEvent) error {
		token := e.Request.URL.Query().Get("token")
		userID := e.Request.URL.Query().Get("user")
		if token == "" || userID == "" {
			return e.BadRequestError("missing token or user", nil)
		}

		e.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		e.Response.WriteHeader(http.StatusOK)
		e.Response.Write([]byte(callbackSuccessHTML(token, userID)))
		return nil
	})
}

func registerWorkerAuthProtectedRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	authGroup := api.Group("/worker-auth")
	authGroup.Bind(apis.RequireAuth())

	authGroup.POST("/approve", func(e *core.RequestEvent) error {
		state := e.Request.FormValue("state")
		if state == "" {
			return e.BadRequestError("state is required", nil)
		}

		pendingAuthsMu.Lock()
		pending, exists := pendingAuths[state]
		if exists {
			delete(pendingAuths, state)
		}
		pendingAuthsMu.Unlock()

		if !exists || time.Since(pending.CreatedAt) > 10*time.Minute {
			return e.BadRequestError("invalid or expired authorization request", nil)
		}

		if e.Auth == nil {
			return e.BadRequestError("authentication required", nil)
		}

		label := fmt.Sprintf("Chrome Extension (%s)", pending.ExtensionID[:8])
		_, plaintext, err := s.Store.CreateWorkerToken(e.Auth.Id, pending.ExtensionID, label)
		if err != nil {
			return e.InternalServerError("failed to create token", err)
		}

		callbackURL := fmt.Sprintf("/api/worker-auth/callback?token=%s&user=%s", plaintext, e.Auth.Id)
		e.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		e.Response.WriteHeader(http.StatusOK)
		page := approvalSuccessHTML(label, callbackURL)
		e.Response.Write([]byte(page))
		return nil
	})

	authGroup.POST("/revoke/{id}", func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.BadRequestError("authentication required", nil)
		}
		tokenID := e.Request.PathValue("id")
		if err := s.Store.RevokeWorkerToken(e.Auth.Id, tokenID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		CloseWorkerByTokenID(tokenID)
		return e.JSON(http.StatusOK, map[string]any{"ok": true})
	})

	authGroup.POST("/delete/{id}", func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.BadRequestError("authentication required", nil)
		}
		tokenID := e.Request.PathValue("id")
		if err := s.Store.DeleteWorkerToken(e.Auth.Id, tokenID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		CloseWorkerByTokenID(tokenID)
		return e.JSON(http.StatusOK, map[string]any{"ok": true})
	})

	authGroup.GET("/tokens", func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.BadRequestError("authentication required", nil)
		}
		tokens, err := s.Store.ListWorkerTokens(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to list tokens", err)
		}
		return e.JSON(http.StatusOK, map[string]any{
			"tokens": tokens,
			"count":  len(tokens),
		})
	})
}

var consentPageTmpl = template.Must(template.New("consent").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Autorizar Conexión</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #0f0f0f;
            color: #e0e0e0;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .card {
            background: #1a1a1a;
            border: 1px solid #2a2a2a;
            border-radius: 12px;
            padding: 32px;
            max-width: 420px;
            width: 90%;
        }
        h1 {
            font-size: 20px;
            font-weight: 600;
            margin-bottom: 16px;
            color: #fff;
        }
        .info {
            background: #252525;
            border-radius: 8px;
            padding: 16px;
            margin-bottom: 20px;
        }
        .info-row {
            display: flex;
            justify-content: space-between;
            margin-bottom: 8px;
        }
        .info-row:last-child { margin-bottom: 0; }
        .info-label { color: #888; font-size: 13px; }
        .info-value { color: #fff; font-size: 13px; font-family: monospace; }
        .permissions {
            margin-bottom: 24px;
            font-size: 14px;
            color: #aaa;
            line-height: 1.6;
        }
        .permissions ul {
            margin-top: 8px;
            padding-left: 20px;
        }
        .buttons {
            display: flex;
            gap: 12px;
        }
        .btn {
            flex: 1;
            padding: 10px 16px;
            border-radius: 8px;
            border: none;
            font-size: 14px;
            font-weight: 500;
            cursor: pointer;
            transition: background 0.2s;
        }
        .btn-cancel {
            background: #2a2a2a;
            color: #aaa;
        }
        .btn-cancel:hover { background: #333; }
        .btn-approve {
            background: #3b82f6;
            color: #fff;
        }
        .btn-approve:hover { background: #2563eb; }
    </style>
</head>
<body>
    <div class="card">
        <h1>Autorizar Conexión</h1>
        <div class="info">
            <div class="info-row">
                <span class="info-label">Extensión</span>
                <span class="info-value">{{.ExtensionID}}</span>
            </div>
        </div>
        <div class="permissions">
            Esto permitirá que la extensión:
            <ul>
                <li>Descargue páginas web por ti</li>
                <li>Acceda a tu sesión de usuario</li>
            </ul>
        </div>
        <form method="POST" action="/api/worker-auth/approve">
            <input type="hidden" name="state" value="{{.State}}">
            <div class="buttons">
                <button type="button" class="btn btn-cancel" onclick="window.close()">Cancelar</button>
                <button type="submit" class="btn btn-approve">Autorizar</button>
            </div>
        </form>
    </div>
</body>
</html>`))

func consentPageHTML(extensionID, state string) string {
	var buf bytes.Buffer
	consentPageTmpl.Execute(&buf, map[string]string{
		"ExtensionID": extensionID,
		"State":       state,
	})
	return buf.String()
}

var approvalSuccessTmpl = template.Must(template.New("success").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Conexión Autorizada</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #0f0f0f;
            color: #e0e0e0;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .card {
            background: #1a1a1a;
            border: 1px solid #2a2a2a;
            border-radius: 12px;
            padding: 32px;
            max-width: 420px;
            width: 90%;
            text-align: center;
        }
        .icon {
            width: 64px;
            height: 64px;
            background: #166534;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 20px;
        }
        .icon svg {
            width: 32px;
            height: 32px;
            stroke: #4ade80;
        }
        h1 {
            font-size: 20px;
            font-weight: 600;
            margin-bottom: 8px;
            color: #fff;
        }
        p {
            font-size: 14px;
            color: #888;
            margin-bottom: 20px;
        }
        .label {
            background: #252525;
            border-radius: 8px;
            padding: 12px;
            font-size: 13px;
            color: #aaa;
            margin-bottom: 20px;
        }
        .btn {
            display: inline-block;
            padding: 10px 24px;
            background: #3b82f6;
            color: #fff;
            border: none;
            border-radius: 8px;
            font-size: 14px;
            font-weight: 500;
            cursor: pointer;
            text-decoration: none;
        }
        .btn:hover { background: #2563eb; }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon">
            <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="20 6 9 17 4 12"></polyline>
            </svg>
        </div>
        <h1>Conexión Autorizada</h1>
        <p>{{.Label}} está conectada a tu cuenta.</p>
        <div class="label">Puedes cerrar esta pestaña.</div>
    </div>
    <script>
        setTimeout(function() { window.location.href = "{{.CallbackURL}}"; }, 1000);
    </script>
</body>
</html>`))

func approvalSuccessHTML(label, callbackURL string) string {
	var buf bytes.Buffer
	approvalSuccessTmpl.Execute(&buf, map[string]string{
		"Label":       label,
		"CallbackURL": callbackURL,
	})
	return buf.String()
}

func loginRequiredHTML() string {
	return `<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Sesión Requerida</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #0f0f0f;
            color: #e0e0e0;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .card {
            background: #1a1a1a;
            border: 1px solid #2a2a2a;
            border-radius: 12px;
            padding: 32px;
            max-width: 420px;
            width: 90%;
            text-align: center;
        }
        .icon {
            width: 64px;
            height: 64px;
            background: #7c2d12;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 20px;
        }
        .icon svg {
            width: 32px;
            height: 32px;
            stroke: #fb923c;
        }
        h1 {
            font-size: 20px;
            font-weight: 600;
            margin-bottom: 8px;
            color: #fff;
        }
        p {
            font-size: 14px;
            color: #888;
            margin-bottom: 20px;
            line-height: 1.5;
        }
        .btn {
            display: inline-block;
            padding: 10px 24px;
            background: #3b82f6;
            color: #fff;
            border: none;
            border-radius: 8px;
            font-size: 14px;
            font-weight: 500;
            cursor: pointer;
            text-decoration: none;
        }
        .btn:hover { background: #2563eb; }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon">
            <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
                <path d="M7 11V7a5 5 0 0 1 10 0v4"></path>
            </svg>
        </div>
        <h1>Sesión Requerida</h1>
        <p>Debes iniciar sesión en Yara primero para autorizar la extensión del navegador.</p>
        <a href="/#/login" class="btn">Iniciar Sesión</a>
    </div>
</body>
</html>`
}

func callbackSuccessHTML(token, userID string) string {
	return `<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Autenticación Completa</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #0f0f0f;
            color: #e0e0e0;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .card {
            background: #1a1a1a;
            border: 1px solid #2a2a2a;
            border-radius: 12px;
            padding: 32px;
            max-width: 420px;
            width: 90%;
            text-align: center;
        }
        .icon {
            width: 64px;
            height: 64px;
            background: #166534;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 20px;
        }
        .icon svg {
            width: 32px;
            height: 32px;
            stroke: #4ade80;
        }
        h1 {
            font-size: 20px;
            font-weight: 600;
            margin-bottom: 8px;
            color: #fff;
        }
        p {
            font-size: 14px;
            color: #888;
            margin-bottom: 20px;
        }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon">
            <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="20 6 9 17 4 12"></polyline>
            </svg>
        </div>
        <h1>Autenticación Completa</h1>
        <p>La extensión del navegador está conectada. Puedes cerrar esta pestaña.</p>
    </div>
    <script>setTimeout(function() { window.close(); }, 2000);</script>
</body>
</html>`
}

func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			pendingAuthsMu.Lock()
			for state, auth := range pendingAuths {
				if time.Since(auth.CreatedAt) > 10*time.Minute {
					delete(pendingAuths, state)
				}
			}
			pendingAuthsMu.Unlock()
		}
	}()
}

```

---

## Job worker — proxy download in runtime_worker

**Archivo:** `internal/api/runtime_worker.go`

processDownloadJob uses proxy when parser is nil or IsBrowserRequiredSite is true AND a browser worker is connected. Creates a ProxyHTTPClient-backed downloader for proxied chapter fetches.


**⚠️ Problema conocido:**
- `processDownloadJob` intenta `dl.DownloadChapters` (HTTP directo) antes de proxy.
  Para 69shuba, esto siempre falla (Cloudflare), y luego reintenta con proxy.
  La latencia extra es ~30s por chapter en el primer intento fallido.
- El proxy se crea con `IsBrowserRequiredSite` check, pero se intenta HTTP directo
  de todas formas. La variable `proxyDL` existe pero no se prioriza.


```go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"translator-server/internal/noveldownloader"
	"translator-server/internal/store"
)

func (s *Server) startJobWorker() {
	s.downloadQueue = make(chan string, 128)
	s.translateQueue = make(chan string, 128)

	go s.downloadWorkerLoop()
	go s.translateWorkerLoop()

	jobs, err := s.Store.ListRunnableJobs()
	if err != nil {
		slog.Error("list runnable jobs", "error", err)
		return
	}
	for _, job := range jobs {
		s.enqueueJob(job.ID)
	}
}

func (s *Server) enqueueJob(jobID string) {
	if jobID == "" {
		return
	}
	s.queueMu.Lock()
	if s.queuedJobs == nil {
		s.queuedJobs = map[string]struct{}{}
	}
	if _, exists := s.queuedJobs[jobID]; exists {
		s.queueMu.Unlock()
		return
	}
	s.queuedJobs[jobID] = struct{}{}
	s.queueMu.Unlock()

	job, err := s.Store.GetJob(jobID)
	if err != nil {
		slog.Error("enqueue job: get job", "jobId", jobID, "error", err)
		return
	}

	var queue chan string
	switch job.Operation {
	case "download":
		queue = s.downloadQueue
	default:
		queue = s.translateQueue
	}

	select {
	case queue <- jobID:
	default:
		s.queueMu.Lock()
		delete(s.queuedJobs, jobID)
		s.queueMu.Unlock()
		msg := "Server is busy processing other jobs. Please wait a few minutes and try again."
		if ue := s.Store.UpdateJob(jobID, map[string]any{
			"status":       "failed",
			"errorMessage": msg,
		}); ue != nil {
			slog.Error("update job status on queue saturation", "jobId", jobID, "error", ue)
		}
		slog.Warn("job queue full, job rejected",
			"jobId", jobID,
			"queueLen", len(queue),
			"queueCap", cap(queue))
	}
}

func (s *Server) workerLoop(queue chan string) {
	for jobID := range queue {
		s.queueMu.Lock()
		delete(s.queuedJobs, jobID)
		s.queueMu.Unlock()
		if err := s.processJob(jobID); err != nil {
			slog.Error("job failed", "jobId", jobID, "error", err)
		}
	}
}

func (s *Server) downloadWorkerLoop() {
	s.workerLoop(s.downloadQueue)
}

func (s *Server) translateWorkerLoop() {
	s.workerLoop(s.translateQueue)
}

func (s *Server) processJob(jobID string) error {
	job, err := s.Store.GetJob(jobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}
	if job.Status == "cancelled" || job.Status == "done" || job.Status == "failed" {
		return nil
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.registerJobCancel(jobID, cancel)
	defer func() {
		cancel()
		s.unregisterJobCancel(jobID)
	}()

	if job.Operation == "download" {
		return s.processDownloadJob(runCtx, job)
	}

	jc, err := s.buildJobContext(runCtx, job)
	if err != nil {
		if ue := s.Store.UpdateJob(jobID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on build context failure", "jobId", jobID, "error", ue)
		}
		return fmt.Errorf("load job context: %w", err)
	}
	if len(jc.chapters) == 0 {
		err := fmt.Errorf("job %s has no chapters to process", jobID)
		if ue := s.Store.UpdateJob(jobID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on empty chapters", "jobId", jobID, "error", ue)
		}
		return err
	}

	if err := s.Store.UpdateJob(jobID, map[string]interface{}{
		"status":                  "running",
		"operation":               job.Operation,
		"provider":                jc.cfg.AI.Provider,
		"model":                   effectiveModel(jc.cfg.AI),
		"errorMessage":            "",
		"totalChapters":           len(jc.chapters),
		"autoSegmentEnabled":      jc.cfg.Translation.AutoSegment,
		"autoSegmentActive":       false,
		"autoSegmentCount":        0,
		"autoSegmentChapterId":    "",
		"autoSegmentChapterTitle": "",
	}); err != nil {
		return fmt.Errorf("set job running: %w", err)
	}

	var wasCancelled bool
	for idx := range jc.chapters {
		if runCtx.Err() != nil {
			wasCancelled = true
			break
		}
		chapter := jc.chapters[idx]
		jc.resetSegmentProgress()

		var chapterErr error
		switch job.Operation {
		case "refine":
			chapterErr = s.runRefineChapter(jc, idx, &chapter)
		default:
			segmentation := previewChapterSegmentation(jc.cfg, chapter)
			jc.recordSegProgress(0, 0, segmentation.SegmentCount, chapter.ID, chapter.Title, segmentation.Applied)
			jc.flushProgress(s)
			var segErr error
			_, segErr = s.runTranslateChapterDetailed(jc, idx, &chapter)
			chapterErr = segErr
		}

		if runCtx.Err() != nil {
			wasCancelled = true
		}

		jc.recordChapterResult(chapterErr)
		if chapterErr != nil {
			if wasCancelled {
				if err := s.Store.UpdateChapterStatusFast(chapter.ID, "pending", ""); err != nil {
					slog.Warn("reset chapter status on cancel", "chapterId", chapter.ID, "error", err)
				}
			} else {
				if err := s.Store.UpdateChapterStatusFast(chapter.ID, "failed", chapterErr.Error()); err != nil {
					slog.Warn("update chapter status on failure", "chapterId", chapter.ID, "error", err)
				}
			}
		}
		jc.resetSegmentProgress()
		jc.flushProgress(s)
	}

	if jc.statsDirty {
		if err := s.Store.RecalculateNovelStats(jc.novel.ID); err != nil {
			slog.Error("recalculate novel stats at job end", "jobId", jobID, "error", err)
		}
	}

	finalStatus := "done"
	finalError := ""
	if wasCancelled {
		finalStatus = "cancelled"
	} else if jc.failed > 0 {
		finalStatus = "failed"
		finalError = jc.lastError
	}

	return s.Store.UpdateJob(jobID, map[string]interface{}{
		"status":                    finalStatus,
		"completedChapters":         jc.completed,
		"failedChapters":            jc.failed,
		"errorMessage":              finalError,
		"autoSegmentActive":         false,
		"autoSegmentCurrentIndex":   0,
		"autoSegmentCompletedCount": 0,
		"autoSegmentChapterId":      "",
		"autoSegmentChapterTitle":   "",
	})
}

type downloadJobOptions struct {
	URL            string                      `json:"url"`
	Chapters       []store.DownloadChapterInfo `json:"chapters"`
	StartOrder     int                         `json:"startOrder"`
	SourceLanguage string                      `json:"sourceLanguage"`
	TargetLanguage string                      `json:"targetLanguage"`
}

func (s *Server) processDownloadJob(ctx context.Context, job *store.Job) error {
	var opts downloadJobOptions
	if err := json.Unmarshal([]byte(job.OptionsJSON), &opts); err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": fmt.Sprintf("invalid job options: %v", err)}); ue != nil {
			slog.Error("update job status on invalid options", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("parse download options: %w", err)
	}
	dl := s.DownloaderFactory()
	if len(opts.Chapters) == 0 {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "done"}); ue != nil {
			slog.Error("update job status on no chapters", "jobId", job.ID, "error", ue)
		}
		if ctx.Err() == nil {
			if err := dl.SleepBetweenChapters(ctx); err != nil {
				return err
			}
		}
		return nil
	}
	if err := s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status":        "running",
		"totalChapters": len(opts.Chapters),
		"errorMessage":  "",
	}); err != nil {
		return fmt.Errorf("set job running: %w", err)
	}

	parser := dl.FindParser(opts.URL)
	hasWorker := s.HasBrowserWorker()

	if parser == nil && !hasWorker {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": "unsupported URL"}); ue != nil {
			slog.Error("update job status on unsupported URL", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("unsupported URL: %s", opts.URL)
	}

	// Set up proxy downloader if a browser worker is available
	var proxyDL *noveldownloader.Downloader
	if hasWorker && (parser == nil || noveldownloader.IsBrowserRequiredSite(opts.URL)) {
		proxyDL = s.DownloaderFactoryWithClient(NewProxyHTTPClient(s))
	}

	completed := 0
	failed := 0
	for idx, chInfo := range opts.Chapters {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if idx > 0 {
			if err := dl.SleepBetweenChapters(ctx); err != nil {
				return err
			}
		}

		var ch *noveldownloader.Chapter
		var downloadErr error
		chURLs := []noveldownloader.ChapterURL{{URL: chInfo.URL, Title: chInfo.Title}}

		if parser != nil {
			downloaded, err := dl.DownloadChapters(ctx, chURLs, 1, 1)
			if err != nil && proxyDL != nil {
				slog.Info("direct HTTP chapter download failed, retrying via browser proxy", "error", err)
				downloaded, err = proxyDL.DownloadChapters(ctx, chURLs, 1, 1)
			}
			if err != nil {
				downloadErr = err
			} else if len(downloaded) > 0 {
				ch = &downloaded[0]
			}
		} else if proxyDL != nil {
			downloaded, err := proxyDL.DownloadChapters(ctx, chURLs, 1, 1)
			if err != nil {
				downloadErr = err
			} else if len(downloaded) > 0 {
				ch = &downloaded[0]
			}
		} else {
			downloadErr = fmt.Errorf("no download method available")
		}

		if downloadErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil
			}
			failed++
			slog.Error("failed to download chapter", "jobId", job.ID, "chapter", chInfo.Title, "error", downloadErr)
		} else if ch != nil {
			chOrder := chInfo.Order
			if chOrder <= 0 {
				chOrder = opts.StartOrder + idx
			}
			chTitle := ch.Title
			if chTitle == "" {
				chTitle = chInfo.Title
			}
			if chTitle == "" {
				chTitle = fmt.Sprintf("Capítulo %d", chOrder)
			}
			if _, err := s.Store.UpsertChapterWithoutStats(job.OwnerID, job.NovelID, &store.Chapter{
				ChapterOrder:    chOrder,
				Title:           chTitle,
				OriginalContent: ch.Markdown,
				Status:          "pending",
			}); err != nil {
				failed++
				slog.Error("failed to save chapter", "jobId", job.ID, "chapter", chTitle, "error", err)
			} else {
				completed++
			}
		} else {
			failed++
			slog.Warn("empty download result", "jobId", job.ID, "chapter", chInfo.Title)
		}
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{
			"completedChapters": completed,
			"failedChapters":    failed,
		}); ue != nil {
			slog.Warn("update job progress", "jobId", job.ID, "error", ue)
		}
	}
	if err := s.Store.RecalculateNovelStats(job.NovelID); err != nil {
		slog.Error("failed to recalculate novel stats after download", "jobId", job.ID, "error", err)
	}
	status := "done"
	if failed > 0 {
		status = "failed"
	}
	if ctx.Err() == nil {
		if err := dl.SleepBetweenChapters(ctx); err != nil {
			return err
		}
	}
	return s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status":            status,
		"completedChapters": completed,
		"failedChapters":    failed,
	})
}

```

---

## Import handler — proxy download in router_import

**Archivo:** `internal/api/router_import.go`

preview-from-url and import-from-url handlers use getNovelInfoWithFallback. The import-from-url flow also uses proxy download for the first chapter fetch.


```go
package api

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/epubimport"
	"translator-server/internal/noveldownloader"
	"translator-server/internal/store"
)

var chapterOrderRegex = regexp.MustCompile(`(\d+)`)

const previewCacheTTL = 15 * time.Minute

type previewCacheEntry struct {
	chapters  []noveldownloader.ChapterURL
	createdAt time.Time
}

func registerImportRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/db/novels/import-epub", func(e *core.RequestEvent) error {
		if err := e.Request.ParseMultipartForm(64 << 20); err != nil {
			return e.BadRequestError("invalid multipart", err)
		}
		file, header, err := e.Request.FormFile("file")
		if err != nil {
			return e.BadRequestError("file required", err)
		}
		defer file.Close()
		blob, err := io.ReadAll(file)
		if err != nil {
			return e.InternalServerError("failed to read file", err)
		}
		parsed, err := epubimport.Parse(blob, header.Filename)
		if err != nil {
			return e.BadRequestError("parse error", err)
		}
		sourceLang := strings.TrimSpace(e.Request.FormValue("sourceLanguage"))
		if sourceLang == "" {
			sourceLang = parsed.Language
		}
		targetLang := strings.TrimSpace(e.Request.FormValue("targetLanguage"))
		if sourceLang == "" || targetLang == "" {
			return e.BadRequestError("sourceLanguage and targetLanguage are required", nil)
		}
		chapters := make([]store.ImportedEpubChapter, len(parsed.Chapters))
		for i, ch := range parsed.Chapters {
			chapters[i] = store.ImportedEpubChapter{Title: ch.Title, Content: ch.Content}
		}
		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = mime.TypeByExtension(".epub")
			if mimeType == "" {
				mimeType = "application/epub+zip"
			}
		}
		result, err := s.Store.ImportEpubNovel(&store.ImportEpubNovelInput{OwnerID: e.Auth.Id, FileName: header.Filename, FileBlob: blob, MimeType: mimeType, SourceTitle: parsed.Title, SourceAuthor: parsed.Author, SourceDescription: parsed.Description, SourceLanguage: sourceLang, TargetLanguage: targetLang, SourceSeries: parsed.Series, SourceNumber: parsed.Number, CoverBlob: parsed.CoverBlob, CoverMime: parsed.CoverMime, Chapters: chapters})
		if err != nil {
			return e.InternalServerError("failed to import epub", err)
		}
		return e.JSON(http.StatusCreated, map[string]any{"novel": parseJSONFields(&result.Novel), "epub": epubRecord(result.Epub), "chaptersImported": result.ChaptersImported})
	})
	api.POST("/db/novels/import-from-zip", func(e *core.RequestEvent) error {
		if err := e.Request.ParseMultipartForm(256 << 20); err != nil {
			return e.BadRequestError("invalid multipart", err)
		}
		file, header, err := e.Request.FormFile("file")
		if err != nil {
			return e.BadRequestError("file required", err)
		}
		defer file.Close()
		blob, err := io.ReadAll(file)
		if err != nil {
			return e.InternalServerError("failed to read file", err)
		}
		reader, err := zip.NewReader(strings.NewReader(string(blob)), int64(len(blob)))
		if err != nil {
			return e.BadRequestError("invalid zip file", err)
		}
		rawEntries := make([]struct {
			name    string
			content []byte
		}, 0)
		for _, f := range reader.File {
			if f.FileInfo().IsDir() {
				continue
			}
			rc, openErr := f.Open()
			if openErr != nil {
				return e.InternalServerError("failed to read zip entry", openErr)
			}
			data, readErr := io.ReadAll(rc)
			rc.Close()
			if readErr != nil {
				return e.InternalServerError("failed to read zip entry", readErr)
			}
			name := strings.TrimLeft(filepath.ToSlash(f.Name), "./")
			rawEntries = append(rawEntries, struct {
				name    string
				content []byte
			}{name, data})
		}
		prefix := detectZipRoot(rawEntries)
		var metadataJSON string
		var coverBlob []byte
		var coverMime string
		type zipFile struct {
			name    string
			content string
		}
		originals := map[string]zipFile{}
		translated := map[string]zipFile{}
		for _, e := range rawEntries {
			normalized := strings.TrimPrefix(e.name, prefix)
			slog.Debug("zip entry", "raw", e.name, "normalized", normalized)
			lower := strings.ToLower(normalized)
			switch {
			case lower == "metadata.json":
				metadataJSON = string(e.content)
			case strings.HasPrefix(lower, "cover."):
				coverBlob = e.content
				ext := strings.ToLower(filepath.Ext(normalized))
				switch ext {
				case ".jpg", ".jpeg":
					coverMime = "image/jpeg"
				case ".png":
					coverMime = "image/png"
				case ".gif":
					coverMime = "image/gif"
				case ".webp":
					coverMime = "image/webp"
				default:
					coverMime = "image/jpeg"
				}
			case strings.HasPrefix(lower, "originals/"):
				name := normalized[len("originals/"):]
				if name != "" {
					originals[name] = zipFile{name: name, content: string(e.content)}
				}
			case strings.HasPrefix(lower, "translated/"):
				name := normalized[len("translated/"):]
				if name != "" {
					translated[name] = zipFile{name: name, content: string(e.content)}
				}
			}
		}
		if metadataJSON == "" {
			return e.BadRequestError("metadata.json is required in the zip", nil)
		}
		if len(originals) == 0 {
			return e.BadRequestError("originals/ directory is required in the zip", nil)
		}
		type namedFile struct {
			name    string
			content string
			number  int
		}
		sorted := make([]namedFile, 0, len(originals))
		for name, f := range originals {
			sorted = append(sorted, namedFile{name: name, content: f.content, number: extractChapterOrder(name)})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].number < sorted[j].number
		})
		chapters := make([]store.ImportedZipChapter, 0, len(sorted))
		for idx, entry := range sorted {
			title := extractChapterTitle(entry.content, entry.name)
			origContent := contentAfterTitle(entry.content)
			transContent := ""
			transTitle := ""
			if t, ok := translated[entry.name]; ok {
				transContent = contentAfterTitle(t.content)
				transTitle = extractChapterTitle(t.content, entry.name)
			}
			chapters = append(chapters, store.ImportedZipChapter{
				Order:             idx + 1,
				Title:             title,
				TranslatedTitle:   transTitle,
				OriginalContent:   origContent,
				TranslatedContent: transContent,
			})
		}
		result, err := s.Store.ImportZipNovel(&store.ImportZipNovelInput{
			OwnerID:      e.Auth.Id,
			FileName:     header.Filename,
			FileBlob:     blob,
			MetadataJSON: metadataJSON,
			CoverBlob:    coverBlob,
			CoverMime:    coverMime,
			Chapters:     chapters,
		})
		if err != nil {
			return e.InternalServerError("failed to import zip novel", err)
		}
		return e.JSON(http.StatusCreated, map[string]any{"novel": parseJSONFields(&result.Novel), "chaptersImported": result.ChaptersImported})
	})
	api.POST("/db/novels/preview-from-url", func(e *core.RequestEvent) error {
		body := struct {
			URL string `json:"url"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if strings.TrimSpace(body.URL) == "" {
			return e.BadRequestError("url is required", nil)
		}
		info, err := s.getNovelInfoWithFallback(e.Request.Context(), body.URL)
		if err != nil {
			return e.BadRequestError(err.Error(), nil)
		}
		return e.JSON(http.StatusOK, map[string]any{
			"title":         info.Title,
			"author":        info.Author,
			"description":   info.Description,
			"coverURL":      info.CoverURL,
			"totalChapters": len(info.Chapters),
			"sourceURL":     info.SourceURL,
		})
	})
	api.POST("/db/novels/import-from-url", func(e *core.RequestEvent) error {
		body := struct {
			URL            string `json:"url"`
			SourceLanguage string `json:"sourceLanguage"`
			TargetLanguage string `json:"targetLanguage"`
			StartChapter   int    `json:"startChapter"`
			EndChapter     int    `json:"endChapter"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if strings.TrimSpace(body.URL) == "" {
			return e.BadRequestError("url is required", nil)
		}
		sourceLang := strings.TrimSpace(body.SourceLanguage)
		if sourceLang == "" {
			sourceLang = "en"
		}
		targetLang := strings.TrimSpace(body.TargetLanguage)
		if targetLang == "" {
			targetLang = "es"
		}
		info, err := s.getNovelInfoWithFallback(e.Request.Context(), body.URL)
		if err != nil {
			return e.BadRequestError(err.Error(), nil)
		}
		startCh := body.StartChapter
		if startCh < 1 {
			startCh = 1
		}
		endCh := body.EndChapter
		if endCh < startCh || endCh > len(info.Chapters) {
			endCh = len(info.Chapters)
		}

		var firstChapter []noveldownloader.Chapter
		dl := s.DownloaderFactory()
		parser := dl.FindParser(body.URL)

		if parser != nil {
			firstChapter, err = dl.DownloadChapters(e.Request.Context(), info.Chapters, startCh, startCh)
			if err != nil && s.HasBrowserWorker() {
				slog.Info("direct HTTP chapter download failed, retrying via browser proxy", "error", err)
				proxyDL := s.DownloaderFactoryWithClient(NewProxyHTTPClient(s))
				firstChapter, err = proxyDL.DownloadChapters(e.Request.Context(), info.Chapters, startCh, startCh)
			}
		} else if s.HasBrowserWorker() {
			proxyDL := s.DownloaderFactoryWithClient(NewProxyHTTPClient(s))
			firstChapter, err = proxyDL.DownloadChapters(e.Request.Context(), info.Chapters, startCh, startCh)
		} else {
			return e.InternalServerError("failed to download first chapter", fmt.Errorf("no download method available"))
		}
		if err != nil {
			return e.InternalServerError("failed to download first chapter", err)
		}
		if len(firstChapter) == 0 {
			return e.InternalServerError("failed to download first chapter", fmt.Errorf("no content returned"))
		}
		result, err := s.Store.ImportUrlNovel(&store.ImportUrlNovelInput{
			OwnerID:           e.Auth.Id,
			URL:               body.URL,
			SourceLanguage:    sourceLang,
			TargetLanguage:    targetLang,
			SourceTitle:       info.Title,
			SourceAuthor:      info.Author,
			SourceDescription: info.Description,
			StartChapter:      startCh,
			EndChapter:        endCh,
		})
		if err != nil {
			return e.InternalServerError("failed to create novel", err)
		}
		ch := firstChapter[0]
		chTitle := ch.Title
		if chTitle == "" {
			chTitle = fmt.Sprintf("Capítulo %d", startCh)
		}
		if _, err := s.Store.UpsertChapterWithoutStats(e.Auth.Id, result.Novel.ID, &store.Chapter{
			ChapterOrder:    startCh,
			Title:           chTitle,
			OriginalContent: ch.Markdown,
			Status:          "pending",
		}); err != nil {
			return e.InternalServerError("failed to save chapter", err)
		}

		if info.CoverURL != "" {
			coverBlob, coverMime, coverErr := dl.DownloadCover(e.Request.Context(), info.CoverURL)
			if coverErr != nil {
				slog.Warn("failed to download cover", "novel", result.Novel.ID, "error", coverErr)
			} else if err := s.Store.AttachCoverBlob(result.Novel.ID, coverBlob, coverMime); err != nil {
				slog.Warn("failed to attach cover", "novel", result.Novel.ID, "error", err)
			}
		}
		if err := s.Store.RecalculateNovelStats(result.Novel.ID); err != nil {
			slog.Warn("failed to recalculate novel stats", "novel", result.Novel.ID, "error", err)
		}
		remainingChapters := make([]store.DownloadChapterInfo, 0)
		for i := startCh; i < endCh; i++ {
			chURL := info.Chapters[i]
			chTitle := chURL.Title
			if chTitle == "" {
				chTitle = fmt.Sprintf("Capítulo %d", i+1)
			}
			remainingChapters = append(remainingChapters, store.DownloadChapterInfo{
				URL:   chURL.URL,
				Title: chTitle,
			})
		}
		var downloadJobID string
		if len(remainingChapters) > 0 {
			optionsJSON, _ := json.Marshal(map[string]any{
				"url":            body.URL,
				"chapters":       remainingChapters,
				"startOrder":     startCh + 1,
				"sourceLanguage": sourceLang,
				"targetLanguage": targetLang,
			})
			job := &store.Job{
				NovelID:       result.Novel.ID,
				Status:        "pending",
				Operation:     "download",
				ChapterIDs:    "[]",
				OptionsJSON:   string(optionsJSON),
				TotalChapters: len(remainingChapters),
			}
			if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
				slog.Error("failed to create download job", "novel", result.Novel.ID, "error", err)
			} else {
				s.enqueueJob(job.ID)
				downloadJobID = job.ID
			}
		}
		novel, err := s.Store.GetOwnedNovel(e.Auth.Id, result.Novel.ID)
		if err != nil {
			return e.InternalServerError("failed to reload novel", err)
		}
		resp := map[string]any{
			"novel":            parseJSONFields(novel),
			"chaptersImported": 1,
			"totalChapters":    len(info.Chapters),
		}
		if downloadJobID != "" {
			resp["downloadJob"] = map[string]any{
				"id":            downloadJobID,
				"totalChapters": len(remainingChapters),
			}
		}
		return e.JSON(http.StatusCreated, resp)
	})
	api.GET("/db/novels/{id}/update-preview", func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("id")
		novel, err := s.Store.GetOwnedNovel(e.Auth.Id, novelID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if strings.TrimSpace(novel.URL) == "" {
			return e.BadRequestError("novel has no source URL", nil)
		}
		dl := s.DownloaderFactory()
		info, err := dl.GetNovelInfo(e.Request.Context(), novel.URL)
		if err != nil {
			return e.InternalServerError("failed to fetch novel info", err)
		}
		cacheKey := e.Auth.Id + ":" + novelID
		s.previewCacheMu.Lock()
		s.previewCache[cacheKey] = previewCacheEntry{
			chapters:  info.Chapters,
			createdAt: time.Now(),
		}
		s.previewCacheMu.Unlock()
		time.AfterFunc(previewCacheTTL, func() {
			s.previewCacheMu.Lock()
			defer s.previewCacheMu.Unlock()
			if entry, exists := s.previewCache[cacheKey]; exists {
				if time.Since(entry.createdAt) >= previewCacheTTL {
					delete(s.previewCache, cacheKey)
				}
			}
		})
		existingOrders, err := s.Store.GetExistingChapterOrders(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to get existing chapter orders", err)
		}
		existingTitles, err := s.Store.GetExistingChapterURLs(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to check existing chapters", err)
		}
		newAvailable := 0
		firstNew := 0
		lastNew := 0
		for _, ch := range info.Chapters {
			chNum := extractChapterOrder(ch.Title)
			if chNum > 0 && existingOrders[chNum] {
				continue
			}
			if existingTitles[ch.Title] {
				continue
			}
			newAvailable++
			if chNum > 0 {
				if firstNew == 0 || chNum < firstNew {
					firstNew = chNum
				}
				if chNum > lastNew {
					lastNew = chNum
				}
			}
		}
		return e.JSON(http.StatusOK, map[string]any{
			"title":           info.Title,
			"author":          info.Author,
			"description":     info.Description,
			"coverURL":        info.CoverURL,
			"sourceURL":       info.SourceURL,
			"currentChapters": len(existingTitles),
			"totalChapters":   len(info.Chapters),
			"newChapters":     newAvailable,
			"firstNewChapter": firstNew,
			"lastNewChapter":  lastNew,
		})
	})
	api.POST("/db/novels/{id}/update-from-url", func(e *core.RequestEvent) error {
		body := struct {
			StartChapter int `json:"startChapter"`
			EndChapter   int `json:"endChapter"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		novelID := e.Request.PathValue("id")
		novel, err := s.Store.GetOwnedNovel(e.Auth.Id, novelID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if strings.TrimSpace(novel.URL) == "" {
			return e.BadRequestError("novel has no source URL", nil)
		}
		cacheKey := e.Auth.Id + ":" + novelID
		s.previewCacheMu.RLock()
		cached, found := s.previewCache[cacheKey]
		s.previewCacheMu.RUnlock()

		var chapters []noveldownloader.ChapterURL
		if found {
			chapters = cached.chapters
			s.previewCacheMu.Lock()
			delete(s.previewCache, cacheKey)
			s.previewCacheMu.Unlock()
		} else {
			dl := s.DownloaderFactory()
			info, err := dl.GetNovelInfo(e.Request.Context(), novel.URL)
			if err != nil {
				return e.InternalServerError("failed to fetch novel info", err)
			}
			chapters = info.Chapters
		}
		existingOrders, err := s.Store.GetExistingChapterOrders(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to get existing chapter orders", err)
		}
		existingTitles, err := s.Store.GetExistingChapterURLs(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to check existing chapters", err)
		}
		sourceToDownload := make([]int, 0)
		for i, ch := range chapters {
			chNum := extractChapterOrder(ch.Title)
			if chNum > 0 && existingOrders[chNum] {
				continue
			}
			if existingTitles[ch.Title] {
				continue
			}
			pos := chNum
			if pos <= 0 {
				pos = i + 1
			}
			if body.StartChapter > 0 && pos < body.StartChapter {
				continue
			}
			if body.EndChapter > 0 && pos > body.EndChapter {
				continue
			}
			sourceToDownload = append(sourceToDownload, i)
		}
		if len(sourceToDownload) == 0 {
			return e.JSON(http.StatusOK, map[string]any{"chaptersAdded": 0, "chapters": []map[string]any{}, "totalChapters": len(chapters), "message": "No hay capítulos nuevos. La novela ya está al día."})
		}
		downloadChapters := make([]store.DownloadChapterInfo, 0, len(sourceToDownload))
		for _, srcIdx := range sourceToDownload {
			ch := chapters[srcIdx]
			chTitle := ch.Title
			if chTitle == "" {
				chTitle = fmt.Sprintf("Capítulo %d", srcIdx+1)
			}
			chOrder := extractChapterOrder(ch.Title)
			if chOrder <= 0 {
				chOrder = srcIdx + 1
			}
			downloadChapters = append(downloadChapters, store.DownloadChapterInfo{
				URL:   ch.URL,
				Title: chTitle,
				Order: chOrder,
			})
		}
		firstNewOrder := extractChapterOrder(chapters[sourceToDownload[0]].Title)
		if firstNewOrder <= 0 {
			firstNewOrder = sourceToDownload[0] + 1
		}
		optionsJSON, _ := json.Marshal(map[string]any{
			"url":            novel.URL,
			"chapters":       downloadChapters,
			"startOrder":     firstNewOrder,
			"sourceLanguage": novel.SourceLanguage,
			"targetLanguage": novel.TargetLanguage,
		})
		job := &store.Job{
			NovelID:       novelID,
			Status:        "pending",
			Operation:     "download",
			ChapterIDs:    "[]",
			OptionsJSON:   string(optionsJSON),
			TotalChapters: len(downloadChapters),
		}
		if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
			return e.InternalServerError("failed to create download job", err)
		}
		s.enqueueJob(job.ID)
		return e.JSON(http.StatusOK, map[string]any{
			"chaptersAdded":   0,
			"chapters":        []map[string]any{},
			"totalChapters":   len(chapters),
			"pendingChapters": len(downloadChapters),
			"downloadJobId":   job.ID,
			"message":         fmt.Sprintf("Descarga iniciada. %d capítulos se están descargando en segundo plano.", len(downloadChapters)),
		})
	})
	api.GET("/db/novels/check-batch-updates", func(e *core.RequestEvent) error {
		novels, err := s.Store.ListOwnedNovelsWithURL(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to list novels", err)
		}
		if len(novels) == 0 {
			return e.JSON(http.StatusOK, store.BatchCheckResponse{
				Results: []store.BatchCheckNovelResult{},
				Checked: 0, WithUpdates: 0, Errors: 0,
			})
		}
		dl := s.DownloaderFactory()
		supported := make([]store.Novel, 0, len(novels))
		for _, n := range novels {
			if dl.IsSupportedURL(n.URL) {
				supported = append(supported, n)
			}
		}
		if len(supported) == 0 {
			return e.JSON(http.StatusOK, store.BatchCheckResponse{
				Results: []store.BatchCheckNovelResult{},
				Checked: 0, WithUpdates: 0, Errors: 0,
			})
		}
		results := make([]store.BatchCheckNovelResult, 0, len(supported))
		checked := 0
		withUpdates := 0
		errCount := 0
		for i, novel := range supported {
			if i > 0 {
				if err := dl.SleepBetweenChapters(e.Request.Context()); err != nil {
					break
				}
			}
			info, err := dl.GetNovelInfo(e.Request.Context(), novel.URL)
			if err != nil {
				errCount++
				results = append(results, store.BatchCheckNovelResult{
					NovelID: novel.ID, SourceTitle: novel.SourceTitle,
					Error: err.Error(),
				})
				continue
			}
			existingOrders, err := s.Store.GetExistingChapterOrders(e.Auth.Id, novel.ID)
			if err != nil {
				errCount++
				results = append(results, store.BatchCheckNovelResult{
					NovelID: novel.ID, SourceTitle: novel.SourceTitle,
					Error: err.Error(),
				})
				continue
			}
			existingTitles, err := s.Store.GetExistingChapterURLs(e.Auth.Id, novel.ID)
			if err != nil {
				errCount++
				results = append(results, store.BatchCheckNovelResult{
					NovelID: novel.ID, SourceTitle: novel.SourceTitle,
					Error: err.Error(),
				})
				continue
			}
			newCh := make([]store.DownloadChapterInfo, 0)
			newAvailable := 0
			firstNew := 0
			lastNew := 0
			startOrder := 0
			for srcIdx, ch := range info.Chapters {
				chNum := extractChapterOrder(ch.Title)
				if chNum > 0 && existingOrders[chNum] {
					continue
				}
				if existingTitles[ch.Title] {
					continue
				}
				newAvailable++
				pos := chNum
				if pos <= 0 {
					pos = srcIdx + 1
				}
				if startOrder == 0 {
					startOrder = pos
				}
				if chNum > 0 {
					if firstNew == 0 || chNum < firstNew {
						firstNew = chNum
					}
					if chNum > lastNew {
						lastNew = chNum
					}
				}
				chTitle := ch.Title
				if chTitle == "" {
					chTitle = fmt.Sprintf("Capítulo %d", pos)
				}
				chOrder := extractChapterOrder(ch.Title)
				if chOrder <= 0 {
					chOrder = pos
				}
				newCh = append(newCh, store.DownloadChapterInfo{
					URL:   ch.URL,
					Title: chTitle,
					Order: chOrder,
				})
			}
			checked++
			if newAvailable > 0 {
				withUpdates++
			}
			if newAvailable == 0 {
				continue
			}
			results = append(results, store.BatchCheckNovelResult{
				NovelID:         novel.ID,
				SourceTitle:     novel.SourceTitle,
				SourceAuthor:    novel.SourceAuthor,
				CoverURL:        info.CoverURL,
				NewChapters:     newAvailable,
				FirstNewChapter: firstNew,
				LastNewChapter:  lastNew,
				StartOrder:      startOrder,
				CurrentChapters: len(existingTitles),
				TotalChapters:   len(info.Chapters),
				NewChapterInfo:  newCh,
			})
		}
		return e.JSON(http.StatusOK, store.BatchCheckResponse{
			Results: results, Checked: checked,
			WithUpdates: withUpdates, Errors: errCount,
		})
	})
	api.POST("/db/novels/batch-update-from-url", func(e *core.RequestEvent) error {
		body := struct {
			Selections []store.BatchUpdateSelection `json:"selections"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if len(body.Selections) == 0 {
			return e.BadRequestError("selections required", nil)
		}
		jobs := make([]store.BatchUpdateJobResult, 0, len(body.Selections))
		totalPending := 0
		for _, sel := range body.Selections {
			novel, err := s.Store.GetOwnedNovel(e.Auth.Id, sel.NovelID)
			if err != nil {
				continue
			}
			chaptersToDownload := sel.NewChapterInfo
			if sel.StartChapter > 0 || sel.EndChapter > 0 {
				filtered := make([]store.DownloadChapterInfo, 0)
				for _, ch := range sel.NewChapterInfo {
					order := extractChapterOrder(ch.Title)
					if order <= 0 {
						order = sel.StartOrder + len(filtered)
					}
					if sel.StartChapter > 0 && order < sel.StartChapter {
						continue
					}
					if sel.EndChapter > 0 && order > sel.EndChapter {
						continue
					}
					filtered = append(filtered, ch)
				}
				chaptersToDownload = filtered
			}
			if len(chaptersToDownload) == 0 {
				continue
			}
			firstOrder := extractChapterOrder(chaptersToDownload[0].Title)
			if firstOrder <= 0 {
				firstOrder = sel.StartOrder
			}
			optionsJSON, _ := json.Marshal(map[string]any{
				"url":            novel.URL,
				"chapters":       chaptersToDownload,
				"startOrder":     firstOrder,
				"sourceLanguage": novel.SourceLanguage,
				"targetLanguage": novel.TargetLanguage,
			})
			job := &store.Job{
				NovelID:       sel.NovelID,
				Status:        "pending",
				Operation:     "download",
				ChapterIDs:    "[]",
				OptionsJSON:   string(optionsJSON),
				TotalChapters: len(chaptersToDownload),
			}
			if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
				continue
			}
			s.enqueueJob(job.ID)
			jobs = append(jobs, store.BatchUpdateJobResult{
				NovelID:         sel.NovelID,
				JobID:           job.ID,
				PendingChapters: len(chaptersToDownload),
			})
			totalPending += len(chaptersToDownload)
		}
		return e.JSON(http.StatusOK, store.BatchUpdateResponse{
			Jobs: jobs, TotalPending: totalPending,
		})
	})
	api.GET("/db/novels/batch-translate-preview", func(e *core.RequestEvent) error {
		novels, err := s.Store.ListOwnedNovelsWithTranslationStats(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to list novels", err)
		}
		results := make([]store.BatchTranslateNovelResult, 0, len(novels))
		withPending := 0
		for _, novel := range novels {
			pendingChapters := novel.ChapterCount - novel.TranslatedCount
			if pendingChapters < 0 {
				pendingChapters = 0
			}
			hasOriginal := novel.OriginalCharCount > 0
			result := store.BatchTranslateNovelResult{
				NovelID:            novel.ID,
				SourceTitle:        novel.SourceTitle,
				SourceAuthor:       novel.SourceAuthor,
				CoverURL:           novel.CoverPath,
				PendingChapters:    pendingChapters,
				TotalChapters:      novel.ChapterCount,
				TranslatedCount:    novel.TranslatedCount,
				CompletedCount:     novel.CompletedCount,
				HasOriginalContent: hasOriginal,
			}
			if pendingChapters > 0 {
				withPending++
				results = append(results, result)
			}
		}
		return e.JSON(http.StatusOK, store.BatchTranslateResponse{
			Results:     results,
			TotalNovels: len(results),
			WithPending: withPending,
		})
	})
	api.POST("/db/novels/batch-translate", func(e *core.RequestEvent) error {
		body := struct {
			Selections []store.BatchTranslateSelection `json:"selections"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if len(body.Selections) == 0 {
			return e.BadRequestError("selections required", nil)
		}
		jobs := make([]store.BatchTranslateJobResult, 0, len(body.Selections))
		totalPending := 0
		for _, sel := range body.Selections {
			novel, err := s.Store.GetOwnedNovel(e.Auth.Id, sel.NovelID)
			if err != nil {
				continue
			}
			var chapterIDs []string
			if len(sel.ChapterIDs) > 0 {
				chapterIDs = sel.ChapterIDs
			} else {
				pending, err := s.Store.GetOwnedNovelChapterIDsByStatus(e.Auth.Id, sel.NovelID)
				if err != nil || len(pending) == 0 {
					continue
				}
				chapterIDs = pending
			}
			idsJSON, _ := json.Marshal(chapterIDs)
			job := &store.Job{
				NovelID:       sel.NovelID,
				Status:        "pending",
				Operation:     "translate",
				ChapterIDs:    string(idsJSON),
				TotalChapters: len(chapterIDs),
			}
			if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
				continue
			}
			if chapters, _, err := s.Store.LoadJobChapters(job); err == nil {
				chIDs := make([]string, 0, len(chapters))
				for _, chapter := range chapters {
					chIDs = append(chIDs, chapter.ID)
				}
				_ = s.Store.UpdateChaptersStatusFast(chIDs, "processing", "")
			}
			s.enqueueJob(job.ID)
			jobs = append(jobs, store.BatchTranslateJobResult{
				NovelID:         sel.NovelID,
				JobID:           job.ID,
				PendingChapters: len(chapterIDs),
			})
			totalPending += len(chapterIDs)
			_ = novel
		}
		return e.JSON(http.StatusOK, store.BatchTranslateStartResponse{
			Jobs: jobs, TotalPending: totalPending,
		})
	})
}

func detectZipRoot(entries []struct {
	name    string
	content []byte
}) string {
	if len(entries) == 0 {
		return ""
	}
	candidate := entries[0].name
	for {
		idx := strings.IndexByte(candidate, '/')
		if idx < 0 {
			return ""
		}
		prefix := candidate[:idx+1]
		allMatch := true
		for _, e := range entries {
			if !strings.HasPrefix(e.name, prefix) {
				allMatch = false
				break
			}
		}
		if allMatch {
			if hasFileAtRoot(strings.TrimSuffix(prefix, "/"), entries) {
				return prefix
			}
			candidate = entries[0].name[idx+1:]
			continue
		}
		return ""
	}
}

func hasFileAtRoot(dir string, entries []struct {
	name    string
	content []byte
}) bool {
	for _, e := range entries {
		rest := strings.TrimPrefix(e.name, dir+"/")
		if rest != "" && strings.IndexByte(rest, '/') < 0 {
			if base := strings.ToLower(filepath.Base(rest)); base == "metadata.json" || strings.HasPrefix(base, "originals") || strings.HasPrefix(base, "translated") {
				return true
			}
		}
	}
	return false
}

func extractChapterOrder(filename string) int {
	matches := chapterOrderRegex.FindStringSubmatch(filename)
	if len(matches) >= 2 {
		if n, err := strconv.Atoi(matches[1]); err == nil {
			return n
		}
	}
	return 0
}

func extractChapterTitle(content, filename string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	first = strings.TrimSpace(first)
	first = strings.TrimLeft(first, "# ")
	first = stripMarkdown(first)
	first = strings.TrimSpace(first)
	if first != "" {
		return first
	}
	return filename
}

func stripMarkdown(s string) string {
	s = strings.ReplaceAll(s, "***", "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "~~", "")
	s = strings.ReplaceAll(s, "`", "")
	return s
}

func contentAfterTitle(content string) string {
	_, rest, found := strings.Cut(strings.TrimSpace(content), "\n")
	if !found || rest == "" {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(rest)
}

```

---

## BrowserRequired sites — site-level proxy flag

**Archivo:** `internal/noveldownloader/browser_required.go`

IsBrowserRequiredSite checks if a URL's host is in the BrowserRequiredSites map. Currently only 69shuba.com is registered.


```go
package noveldownloader

import (
	"net/url"
	"strings"
)

var BrowserRequiredSites = map[string]bool{
	// 69shuba.com — Cloudflare-protected chapter pages, catalog requires login
	"69shuba.com": true,
}

func IsBrowserRequiredSite(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	return BrowserRequiredSites[host]
}

```

---

## BrowserWorkerProvider — full-content-provider via extension

**Archivo:** `internal/noveldownloader/browser_worker_provider.go`

Implements a ContentProvider that sends get_novel_info, get_chapters, get_chapter jobs to the browser worker extension for full JS-rendered parsing. Converts HTML to markdown server-side.


```go
package noveldownloader

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown/v2"
)

type BrowserWorkerClient interface {
	SendJob(operation, url string, params map[string]interface{}) (*BrowserWorkerResult, error)
	IsConnected() bool
}

type BrowserWorkerResult struct {
	JobID  string                 `json:"jobId"`
	Status string                 `json:"status"`
	Data   map[string]interface{} `json:"data"`
}

type BrowserWorkerProvider struct {
	client BrowserWorkerClient
}

func NewBrowserWorkerProvider(client BrowserWorkerClient) *BrowserWorkerProvider {
	return &BrowserWorkerProvider{client: client}
}

func (p *BrowserWorkerProvider) Name() string {
	return "browser-worker"
}

func (p *BrowserWorkerProvider) CanHandle(url string) bool {
	return p.client != nil && p.client.IsConnected()
}

func (p *BrowserWorkerProvider) GetNovelInfo(ctx context.Context, url string) (*NovelInfo, error) {
	if !p.client.IsConnected() {
		return nil, fmt.Errorf("browser worker not connected")
	}

	result, err := p.client.SendJob("get_novel_info", url, nil)
	if err != nil {
		return nil, fmt.Errorf("browser worker job failed: %w", err)
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("browser worker returned status: %s", result.Status)
	}

	info := &NovelInfo{
		SourceURL: url,
	}

	if title, ok := result.Data["title"].(string); ok {
		info.Title = title
	}
	if author, ok := result.Data["author"].(string); ok {
		info.Author = author
	}
	if desc, ok := result.Data["description"].(string); ok {
		info.Description = desc
	}
	if cover, ok := result.Data["coverURL"].(string); ok {
		info.CoverURL = cover
	}
	if sourceURL, ok := result.Data["sourceURL"].(string); ok {
		info.SourceURL = sourceURL
	}

	if chaptersRaw, ok := result.Data["chapters"].([]interface{}); ok {
		for _, ch := range chaptersRaw {
			chMap, ok := ch.(map[string]interface{})
			if !ok {
				continue
			}
			chURL := ChapterURL{}
			if u, ok := chMap["url"].(string); ok {
				chURL.URL = u
			}
			if t, ok := chMap["title"].(string); ok {
				chURL.Title = t
			}
			info.Chapters = append(info.Chapters, chURL)
		}
	}

	slog.Info("browser worker got novel info",
		"title", info.Title,
		"chapters", len(info.Chapters))

	return info, nil
}

func (p *BrowserWorkerProvider) GetChapterURLs(ctx context.Context, url string) ([]ChapterURL, error) {
	if !p.client.IsConnected() {
		return nil, fmt.Errorf("browser worker not connected")
	}

	result, err := p.client.SendJob("get_chapters", url, nil)
	if err != nil {
		return nil, fmt.Errorf("browser worker job failed: %w", err)
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("browser worker returned status: %s", result.Status)
	}

	var chapters []ChapterURL
	if chaptersRaw, ok := result.Data["chapters"].([]interface{}); ok {
		for _, ch := range chaptersRaw {
			chMap, ok := ch.(map[string]interface{})
			if !ok {
				continue
			}
			chURL := ChapterURL{}
			if u, ok := chMap["url"].(string); ok {
				chURL.URL = u
			}
			if t, ok := chMap["title"].(string); ok {
				chURL.Title = t
			}
			chapters = append(chapters, chURL)
		}
	}

	return chapters, nil
}

func (p *BrowserWorkerProvider) ParseChapter(ctx context.Context, url string) (*Chapter, error) {
	if !p.client.IsConnected() {
		return nil, fmt.Errorf("browser worker not connected")
	}

	result, err := p.client.SendJob("get_chapter", url, nil)
	if err != nil {
		return nil, fmt.Errorf("browser worker job failed: %w", err)
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("browser worker returned status: %s", result.Status)
	}

	chapter := &Chapter{
		SourceURL: url,
	}

	if title, ok := result.Data["title"].(string); ok {
		chapter.Title = title
	}
	if content, ok := result.Data["content"].(string); ok {
		chapter.Content = content
	}
	if markdown, ok := result.Data["markdown"].(string); ok {
		chapter.Markdown = markdown
	}
	if sourceURL, ok := result.Data["sourceURL"].(string); ok {
		chapter.SourceURL = sourceURL
	}

	if chapter.Content != "" && chapter.Markdown == "" {
		markdown, err := md.ConvertString(chapter.Content)
		if err != nil {
			slog.Warn("browser worker failed to convert to markdown", "error", err)
		} else {
			chapter.Markdown = strings.TrimSpace(markdown)
		}
	}

	return chapter, nil
}

```

---

## Downloader — parser registry (includes 69shuba)

**Archivo:** `internal/noveldownloader/downloader.go`

NewDownloader and NewDownloaderWithClient register all parsers including New69ShubaParser. IsSupportedURL, FindParser, GetNovelInfo, DownloadChapter, DownloadChapters.


```go
package noveldownloader

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown/v2"
)

const (
	// DefaultMinChapterDelay is the default minimum wait between two
	// consecutive chapter fetches. Used to stay below the rate limits of
	// upstream sites like novelfire.net and novelbin.com.
	DefaultMinChapterDelay = 5 * time.Second
	// DefaultMaxChapterDelay is the default maximum wait between two
	// consecutive chapter fetches. A new random value in
	// [min, max] is picked for each gap so the request pattern is less
	// recognizable by upstream defences.
	DefaultMaxChapterDelay = 10 * time.Second
)

type Downloader struct {
	parsers []Parser
	client  HTTPClient
	// MinChapterDelay is the lower bound (inclusive) of the random
	// sleep applied between two consecutive chapter fetches.
	MinChapterDelay time.Duration
	// MaxChapterDelay is the upper bound (inclusive) of the random
	// sleep applied between two consecutive chapter fetches.
	MaxChapterDelay time.Duration
}

func NewDownloader() *Downloader {
	return &Downloader{
		parsers: []Parser{
			NewNovelfireParser(),
			NewNovelbinParser(),
			NewFenrirRealmParser(),
			New69ShubaParser(),
		},
		client:          NewHTTPClient(),
		MinChapterDelay: DefaultMinChapterDelay,
		MaxChapterDelay: DefaultMaxChapterDelay,
	}
}

// NewDownloaderWithClient returns a Downloader that uses the provided
// HTTPClient. Primarily intended for tests that need to redirect remote
// hosts to local fixtures; the inter-chapter delay is disabled so test
// runs stay fast.
func NewDownloaderWithClient(client HTTPClient) *Downloader {
	return &Downloader{
		parsers: []Parser{
			NewNovelfireParser(),
			NewNovelbinParser(),
			NewFenrirRealmParser(),
			New69ShubaParser(),
		},
		client: client,
	}
}

func (d *Downloader) IsSupportedURL(url string) bool {
	return d.FindParser(url) != nil
}

func (d *Downloader) FindParser(url string) Parser {
	for _, p := range d.parsers {
		if p.CanHandle(url) {
			return p
		}
	}
	return nil
}

func (d *Downloader) GetNovelInfo(ctx context.Context, url string) (*NovelInfo, error) {
	parser := d.FindParser(url)
	if parser == nil {
		return nil, fmt.Errorf("unsupported URL: %s", url)
	}
	return parser.GetNovelInfo(ctx, d.client, url)
}

func (d *Downloader) DownloadChapter(ctx context.Context, chapterURL string) (*Chapter, error) {
	parser := d.FindParser(chapterURL)
	if parser == nil {
		return nil, fmt.Errorf("unsupported URL: %s", chapterURL)
	}
	chapter, err := parser.ParseChapter(ctx, d.client, chapterURL)
	if err != nil {
		return nil, err
	}
	if chapter.Content != "" {
		markdown, err := md.ConvertString(chapter.Content)
		if err != nil {
			return nil, fmt.Errorf("converting to markdown: %w", err)
		}
		chapter.Markdown = stripLeadingTitle(cleanMarkdown(markdown))
	}
	return chapter, nil
}

func (d *Downloader) DownloadChapters(ctx context.Context, chapters []ChapterURL, start, end int) ([]Chapter, error) {
	if start < 1 {
		start = 1
	}
	if end > len(chapters) || end < start {
		end = len(chapters)
	}

	var selected []ChapterURL
	for i, ch := range chapters {
		idx := i + 1
		if idx >= start && idx <= end {
			selected = append(selected, ch)
		}
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no chapters in range %d-%d (total: %d)", start, end, len(chapters))
	}

	result := make([]Chapter, 0, len(selected))
	for i, ch := range selected {
		if i > 0 {
			if err := d.SleepBetweenChapters(ctx); err != nil {
				return nil, err
			}
		}
		chapter, err := d.DownloadChapter(ctx, ch.URL)
		if err != nil {
			return nil, fmt.Errorf("downloading chapter %q: %w", ch.Title, err)
		}
		result = append(result, *chapter)
	}
	return result, nil
}

// SleepBetweenChapters waits a random duration within
// [MinChapterDelay, MaxChapterDelay] before fetching the next chapter.
// The wait is bounded by the request context so cancellation propagates.
func (d *Downloader) SleepBetweenChapters(ctx context.Context) error {
	min := d.MinChapterDelay
	max := d.MaxChapterDelay
	if min <= 0 && max <= 0 {
		return nil
	}
	if max < min {
		max = min
	}
	var delay time.Duration
	if max == min {
		delay = max
	} else {
		delay = min + time.Duration(rand.Int63n(int64(max-min)))
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// stripLeadingTitle removes a markdown heading from the first line of
// content when it looks like a duplicate of the chapter title.
// Some sites (novelfire, novelbin) inject the chapter title as a heading
// inside the content body, causing a duplicate title in the stored content.
func stripLeadingTitle(content string) string {
	trimmed := strings.TrimLeft(content, "\n\t ")
	if trimmed == "" {
		return ""
	}
	// Re-split from the trimmed content so leading whitespace does not
	// cause us to miss a heading on the first non-empty line.
	lines := strings.SplitN(trimmed, "\n", 2)
	first := strings.TrimSpace(lines[0])
	if strings.HasPrefix(first, "# ") ||
		strings.HasPrefix(first, "## ") ||
		strings.HasPrefix(first, "### ") ||
		strings.HasPrefix(first, "#### ") {
		if len(lines) > 1 {
			return strings.TrimSpace(lines[1])
		}
		return ""
	}
	return content
}

func cleanMarkdown(markdown string) string {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	markdown = strings.ReplaceAll(markdown, "\r", "\n")
	markdown = strings.TrimSpace(markdown)
	for strings.Contains(markdown, "\n\n\n") {
		markdown = strings.ReplaceAll(markdown, "\n\n\n", "\n\n")
	}
	return markdown
}

// DownloadCover fetches a remote image and returns its bytes together with
// the content type reported by the server. The downloader routes the
// request through its configured HTTPClient so that test transports and
// host rewrites apply consistently.
func (d *Downloader) DownloadCover(ctx context.Context, coverURL string) ([]byte, string, error) {
	if strings.TrimSpace(coverURL) == "" {
		return nil, "", fmt.Errorf("empty cover url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coverURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating cover request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching cover: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d fetching cover", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading cover body: %w", err)
	}
	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return body, mimeType, nil
}

```

---

## Parser interface

**Archivo:** `internal/noveldownloader/parser.go`

Parser interface definition — Name, CanHandle, GetNovelInfo, GetChapterURLs, ParseChapter.


```go
package noveldownloader

import (
	"context"

	"github.com/PuerkitoBio/goquery"
)

type Parser interface {
	Name() string
	CanHandle(url string) bool
	GetNovelInfo(ctx context.Context, client HTTPClient, url string) (*NovelInfo, error)
	GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, url string) ([]ChapterURL, error)
	ParseChapter(ctx context.Context, client HTTPClient, url string) (*Chapter, error)
}

```

---

## HTTPClient interface & Chinese charset decoding

**Archivo:** `internal/noveldownloader/client.go`

HTTPClient interface (Fetch, FetchDocument, Do). Default httpClient with 30s timeout. GBK/GB2312/GB18030 auto-detection and decoding to UTF-8, used by the 69shuba parser.


```go
package noveldownloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type HTTPClient interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
	FetchDocument(ctx context.Context, url string) (*goquery.Document, error)
	Do(req *http.Request) (*http.Response, error)
}

type httpClient struct {
	client *http.Client
}

func NewHTTPClient() HTTPClient {
	return &httpClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewHTTPClientWithTransport returns an HTTPClient backed by an http.Client
// using the given transport. Intended primarily for tests that need to
// rewrite hosts (e.g. map novelbin.com to a local httptest server).
func NewHTTPClientWithTransport(transport http.RoundTripper) HTTPClient {
	return &httpClient{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func (c *httpClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Auto-detect and decode Chinese encodings (GBK, GB2312, GB18030)
	body = decodeChineseCharset(body)

	return body, nil
}

// decodeChineseCharset detects GBK/GB2312/GB18030 encoding from HTML <meta>
// charset declarations and decodes to UTF-8. If no Chinese charset is found
// it returns the input unchanged.
func decodeChineseCharset(raw []byte) []byte {
	// If content is already valid UTF-8, skip charset detection entirely.
	// This handles content from browser proxy which decodes GBK to UTF-8
	// before returning it, but the HTML may still contain <meta charset="gbk">.
	if utf8.Valid(raw) {
		return raw
	}

	// Peek at the first 4096 bytes for <meta charset="..."> or <meta ... charset="...">
	peekLen := 4096
	if len(raw) < peekLen {
		peekLen = len(raw)
	}
	peek := string(raw[:peekLen])

	isGBK := false

	// Check <meta charset="gbk">, <meta charset="gb2312">, <meta charset="gb18030">
	lower := strings.ToLower(peek)
	for _, cs := range []string{"gbk", "gb2312", "gb18030"} {
		if strings.Contains(lower, `charset="`+cs+`"`) ||
			strings.Contains(lower, `content="text/html; charset=`+cs+`"`) ||
			strings.Contains(lower, `charset=`+cs) {
			isGBK = true
			break
		}
	}
	if !isGBK {
		return raw
	}

	// Try GBK first, fall back to GB18030
	for _, decoder := range []transform.Transformer{
		simplifiedchinese.GBK.NewDecoder(),
		simplifiedchinese.GB18030.NewDecoder(),
	} {
		decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(raw), decoder))
		if err == nil && isLikelyUTF8(decoded) {
			return decoded
		}
	}

	// If decoding failed, return original
	return raw
}

// isLikelyUTF8 does a quick check to see if the bytes look like valid UTF-8.
func isLikelyUTF8(b []byte) bool {
	// Check a sample: if we see common UTF-8 continuation bytes patterns it's likely OK
	// A simple heuristic: if more than 5% of non-ASCII bytes are valid UTF-8 sequences
	nonASCII := 0
	valid := 0
	i := 0
	sample := len(b)
	if sample > 5000 {
		sample = 5000
	}
	for i < sample {
		if b[i] < 0x80 {
			i++
			continue
		}
		nonASCII++
		// Check valid UTF-8 multi-byte sequences
		if b[i] >= 0xC0 && b[i] <= 0xDF && i+1 < sample && b[i+1]&0xC0 == 0x80 {
			valid++
			i += 2
		} else if b[i] >= 0xE0 && b[i] <= 0xEF && i+2 < sample && b[i+1]&0xC0 == 0x80 && b[i+2]&0xC0 == 0x80 {
			valid++
			i += 3
		} else if b[i] >= 0xF0 && b[i] <= 0xF4 && i+3 < sample && b[i+1]&0xC0 == 0x80 && b[i+2]&0xC0 == 0x80 && b[i+3]&0xC0 == 0x80 {
			valid++
			i += 4
		} else {
			i++
		}
	}
	if nonASCII == 0 {
		return true
	}
	return float64(valid)/float64(nonASCII) > 0.8
}

// DecodeHTMLBody decodes raw HTML bytes using charset detection and returns
// the decoded bytes. Useful for parsers that need to handle non-UTF-8 content
// when using a custom FetchDocument path.
func DecodeHTMLBody(raw []byte) []byte {
	return decodeChineseCharset(raw)
}

func (c *httpClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}
	return doc, nil
}

func (c *httpClient) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

```

---

## 69shuba — entry point & URL matching

**Archivo:** `internal/noveldownloader/69shuba.go`

sixtyNineShuba struct, regex patterns for info/chapter/catalog URLs, CanHandle, GetNovelInfo, GetChapterURLs, ParseChapter delegating to sub-files.


**⚠️ Problemas conocidos:**

- Known issue: regex patterns may not match current 69shuba URL structure


```go
package noveldownloader

import (
	"context"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	sixtyNineShubaInfoRe    = regexp.MustCompile(`69shuba\.com/book/(\d+)\.htm`)
	sixtyNineShubaChapsRe   = regexp.MustCompile(`69shuba\.com/book/(\d+)/?$`)
	sixtyNineShubaChapterRe = regexp.MustCompile(`69shuba\.com/txt/(\d+)/(\d+)`)
	sixtyNineShubaBaseURL   = "https://www.69shuba.com"
)

type sixtyNineShuba struct{}

func New69ShubaParser() *sixtyNineShuba {
	return &sixtyNineShuba{}
}

func (s *sixtyNineShuba) Name() string { return "69shuba" }

func (s *sixtyNineShuba) CanHandle(u string) bool {
	return strings.Contains(u, "69shuba.com")
}

func (s *sixtyNineShuba) GetNovelInfo(ctx context.Context, client HTTPClient, u string) (*NovelInfo, error) {
	if sixtyNineShubaChapterRe.MatchString(u) {
		return s.getInfoFromChapter(ctx, client, u)
	}
	return s.getInfoFromInfoPage(ctx, client, u)
}

func (s *sixtyNineShuba) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, u string) ([]ChapterURL, error) {
	return s.fetchChapterList(ctx, client, u)
}

func (s *sixtyNineShuba) ParseChapter(ctx context.Context, client HTTPClient, url string) (*Chapter, error) {
	return s.getChapterContent(ctx, client, url)
}

```

---

## 69shuba — metadata & chapter list extraction

**Archivo:** `internal/noveldownloader/69shuba_metadata.go`

getInfoFromInfoPage, getInfoFromChapter, fetchChapterList, extractChaptersFromInfoPage. Uses minimumChapters (20) threshold — if fewer chapters via direct HTTP, returns error to trigger browser proxy fallback. Reverses chapter list (69shuba shows newest first).


**⚠️ Problemas conocidos:**

- Known issues:

- - Catalog page /book/{id}/ requires login (returns login page → 0 chapters)

- - CSS selectors in fetchChapterList may be outdated

- - extractChaptersFromInfoPage selector .qustime may not exist

- - ensureChapterExtension assumes /txt/ URL pattern

- - minimumChapters=20 threshold triggers proxy fallback even when partial success


```go
package noveldownloader

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// minimumChapters is the minimum number of chapters we need from the catalog
// before we consider a direct-HTTP fetch successful. If we get fewer than
// this (e.g. only the 5 recent chapters shown on the info page), we return
// an error so the caller can fall back to the browser proxy (which has login
// cookies and can access the full catalog at /book/{id}/).
const minimumChapters = 20

var (
	sixtyNineShubaTitleRe        = regexp.MustCompile(`<title>([^<]+)</title>`)
	sixtyNineShubaBookInfoJSRe   = regexp.MustCompile(`articlename:\s*'([^']+)'`)
	sixtyNineShubaAuthorJSRe     = regexp.MustCompile(`author:\s*'([^']+)'`)
	sixtyNineShubaWordCountRe    = regexp.MustCompile(`(\d+\.?\d*)万字`)
	sixtyNineShubaDescriptionRe  = regexp.MustCompile(`og:description.*?content="([^"]+)"`)
	sixtyNineShubaArticleIDRe    = regexp.MustCompile(`articleid:\s*'(\d+)'`)
)

func (s *sixtyNineShuba) getInfoFromInfoPage(ctx context.Context, client HTTPClient, u string) (*NovelInfo, error) {
	raw, err := client.Fetch(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("69shuba fetch: %w", err)
	}

	// Decode GBK to UTF-8 (the site serves <meta charset="gbk">)
	raw = DecodeHTMLBody(raw)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("69shuba parse: %w", err)
	}

	info := &NovelInfo{
		SourceURL: u,
	}

	htmlStr := string(raw)

	// Extract metadata — after GBK→UTF-8 decode these will be readable
	if m := sixtyNineShubaBookInfoJSRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		info.Title = m[1]
	}
	if m := sixtyNineShubaAuthorJSRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		info.Author = m[1]
	}

	if info.Title == "" {
		if content, exists := doc.Find("meta[property='og:novel:book_name']").Attr("content"); exists {
			info.Title = content
		}
	}
	if info.Title == "" {
		info.Title = strings.TrimSpace(doc.Find(".booknav2 h1").Text())
	}
	if info.Author == "" {
		if content, exists := doc.Find("meta[property='og:novel:author']").Attr("content"); exists {
			info.Author = content
		}
	}
	if info.Author == "" {
		authorText := strings.TrimSpace(doc.Find(".booknav2 p").First().Text())
		info.Author = strings.TrimPrefix(authorText, "作者：")
	}

	if content, exists := doc.Find("meta[property='og:description']").Attr("content"); exists {
		info.Description = content
	}
	if info.Description == "" {
		info.Description = strings.TrimSpace(doc.Find("meta[name='description']").AttrOr("content", ""))
	}

	if content, exists := doc.Find("meta[property='og:image']").Attr("content"); exists {
		info.CoverURL = content
	}

	// Build catalog URL directly from the book ID — the catalog page at
	// /book/{id}/ contains the full chapter list but requires a logged-in
	// session (or browser-proxy access to bypass Cloudflare).
	bookID := extract69ShubaBookID(u)
	if bookID == "" {
		if m := sixtyNineShubaArticleIDRe.FindStringSubmatch(htmlStr); len(m) > 1 {
			bookID = m[1]
		}
	}

	if bookID != "" {
		catalogURL := fmt.Sprintf("%s/book/%s/", sixtyNineShubaBaseURL, bookID)
		slog.Info("69shuba: trying catalog", "url", catalogURL)

		chapters, err := s.fetchChapterList(ctx, client, catalogURL)
		if err == nil && len(chapters) >= minimumChapters {
			info.Chapters = chapters
			slog.Info("69shuba: catalog fetch succeeded", "chapters", len(chapters))
			return info, nil
		}
		if err != nil {
			slog.Info("69shuba: catalog fetch failed (will try fallback)", "error", err)
		} else {
			slog.Info("69shuba: catalog returned too few chapters", "got", len(chapters), "need", minimumChapters)
		}
	}

	// Fallback: extract chapters from the info page (only shows ~5 recent ones)
	info.Chapters = s.extractChaptersFromInfoPage(doc)

	// If the fallback also gave us too few, return an error so the caller
	// can switch to the browser proxy (which has login cookies).
	if len(info.Chapters) < minimumChapters {
		return nil, fmt.Errorf("69shuba: only got %d/%d chapters via direct HTTP (needs browser proxy for full catalog)",
			len(info.Chapters), minimumChapters)
	}

	return info, nil
}

func (s *sixtyNineShuba) getInfoFromChapter(ctx context.Context, client HTTPClient, u string) (*NovelInfo, error) {
	raw, err := client.Fetch(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("69shuba fetch: %w", err)
	}

	raw = DecodeHTMLBody(raw)
	htmlStr := string(raw)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return nil, fmt.Errorf("69shuba parse: %w", err)
	}

	info := &NovelInfo{
		SourceURL: u,
	}

	if m := sixtyNineShubaBookInfoJSRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		info.Title = m[1]
	}
	if m := sixtyNineShubaAuthorJSRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		info.Author = m[1]
	}

	bookID := extract69ShubaBookID(u)
	if bookID != "" {
		infoURL := fmt.Sprintf("%s/book/%s/", sixtyNineShubaBaseURL, bookID)
		chapters, err := s.fetchChapterList(ctx, client, infoURL)
		if err == nil && len(chapters) >= minimumChapters {
			info.Chapters = chapters
			return info, nil
		}
	}

	// Fallback from the chapter page itself
	info.Chapters = s.extractChaptersFromInfoPage(doc)

	if len(info.Chapters) < minimumChapters {
		return nil, fmt.Errorf("69shuba: only got %d/%d chapters via direct HTTP (needs browser proxy)",
			len(info.Chapters), minimumChapters)
	}

	return info, nil
}

func (s *sixtyNineShuba) extractChaptersFromInfoPage(doc *goquery.Document) []ChapterURL {
	var chapters []ChapterURL

	doc.Find(".qustime ul li a").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists {
			return
		}

		title := strings.TrimSpace(sel.Find("span").Text())
		if title == "" {
			title = strings.TrimSpace(sel.Text())
		}

		if date := sel.Find("small").Text(); date != "" {
			title = strings.Replace(title, date, "", 1)
			title = strings.TrimSpace(title)
		}

		if !strings.HasPrefix(href, "http") {
			href = sixtyNineShubaBaseURL + href
		}

		href = ensureChapterExtension(href)

		chapters = append(chapters, ChapterURL{
			Title: title,
			URL:   href,
		})
	})

	return chapters
}

func (s *sixtyNineShuba) fetchChapterList(ctx context.Context, client HTTPClient, infoURL string) ([]ChapterURL, error) {
	raw, err := client.Fetch(ctx, infoURL)
	if err != nil {
		slog.Info("69shuba: fetchChapterList fetch failed", "url", infoURL, "error", err)
		return nil, err
	}

	raw = DecodeHTMLBody(raw)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("69shuba parse catalog: %w", err)
	}

	slog.Info("69shuba: fetchChapterList got document", "url", infoURL)

	// Check for 404-like page (site returns 200 but shows "页面目不存在或删除" title)
	pageText := strings.TrimSpace(doc.Find("title").Text())
	if strings.Contains(pageText, "404") || strings.Contains(pageText, "页面目不存在") || strings.Contains(pageText, "页面不存在") {
		return nil, fmt.Errorf("69shuba: catalog page not found (may need login)")
	}

	var chapters []ChapterURL

	// Try multiple selectors for chapter list
	selectors := []string{
		"#catalog ul li a",
		"div.catalog ul li a",
		"ul.chapter-list li a",
		".listmain li a",
		"#list li a",
		".booklist li a",
		".volume li a",
		".qustime li a",
	}

	for _, sel := range selectors {
		doc.Find(sel).Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if !exists {
				return
			}

			title := strings.TrimSpace(s.Text())
			if !strings.HasPrefix(href, "http") {
				href = sixtyNineShubaBaseURL + href
			}

			href = ensureChapterExtension(href)

			chapters = append(chapters, ChapterURL{
				Title: title,
				URL:   href,
			})
		})
		if len(chapters) > 0 {
			slog.Info("69shuba: found chapters with selector", "selector", sel, "count", len(chapters))
			break
		}
	}

	// Fallback: find all links that look like chapter URLs
	if len(chapters) == 0 {
		slog.Info("69shuba: no chapters found with selectors, trying fallback")
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if !exists {
				return
			}
			title := strings.TrimSpace(s.Text())
			if title == "" {
				return
			}
			if strings.Contains(href, "/txt/") || strings.Contains(href, "/chapter/") || strings.Contains(href, "/read/") {
				if !strings.HasPrefix(href, "http") {
					href = sixtyNineShubaBaseURL + href
				}
				href = ensureChapterExtension(href)
				chapters = append(chapters, ChapterURL{
					Title: title,
					URL:   href,
				})
			}
		})
		if len(chapters) > 0 {
			slog.Info("69shuba: found chapters with fallback", "count", len(chapters))
		}
	}

	// 69shuba lists chapters in reverse order (newest first).
	// Reverse to chronological order (oldest first).
	slices.Reverse(chapters)

	return chapters, nil
}

func extract69ShubaBookID(u string) string {
	if m := sixtyNineShubaChapsRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	if m := sixtyNineShubaInfoRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	return ""
}

func (s *sixtyNineShuba) getWordCount(html string) int {
	if m := sixtyNineShubaWordCountRe.FindStringSubmatch(html); len(m) > 1 {
		if wc, err := strconv.ParseFloat(m[1], 64); err == nil {
			return int(wc * 10000)
		}
	}
	return 0
}

func ensureChapterExtension(url string) string {
	if strings.Contains(url, "/txt/") && !strings.HasSuffix(url, ".html") {
		return url + ".html"
	}
	return url
}

```

---

## 69shuba — chapter content extraction

**Archivo:** `internal/noveldownloader/69shuba_chapters.go`

getChapterContent — fetches, decodes GBK, finds .txtnav or #content div, removes ads/scripts, cleans HTML artifacts.


**⚠️ Problemas conocidos:**

- Known issues:

- - .txtnav / #content selectors may not match current 69shuba DOM

- - Title extraction assumes <h1> inside .txtnav

- - HTML content may not convert cleanly to markdown (em-spaces, ad remnants)


```go
package noveldownloader

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// GetChapters returns chapter URLs by fetching the catalog page at /book/{id}/.
func (s *sixtyNineShuba) GetChapters(ctx context.Context, client HTTPClient, novelURL string) ([]ChapterURL, error) {
	bookID := extract69ShubaBookID(novelURL)
	if bookID == "" {
		return nil, fmt.Errorf("69shuba: cannot extract book ID from %s", novelURL)
	}

	infoURL := fmt.Sprintf("%s/book/%s/", sixtyNineShubaBaseURL, bookID)
	return s.fetchChapterList(ctx, client, infoURL)
}

func (s *sixtyNineShuba) getChapterContent(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	raw, err := client.Fetch(ctx, chapterURL)
	if err != nil {
		return nil, fmt.Errorf("69shuba fetch: %w", err)
	}

	// Decode GBK to UTF-8 (the site serves <meta charset="gbk">)
	raw = DecodeHTMLBody(raw)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("69shuba parse: %w", err)
	}

	// Find the main content container (.txtnav)
	txtNav := doc.Find(".txtnav")
	if txtNav.Length() == 0 {
		txtNav = doc.Find("#content")
	}
	if txtNav.Length() == 0 {
		return nil, fmt.Errorf("69shuba: no content found at %s", chapterURL)
	}

	// Extract chapter title from <h1> inside .txtnav
	title := strings.TrimSpace(txtNav.Find("h1").First().Text())

	// Remove non-content elements that live inside .txtnav
	txtNav.Find("h1").Remove()
	txtNav.Find("div.txtinfo").Remove()
	txtNav.Find("#txtright").Remove()
	txtNav.Find("div.txtright").Remove()

	// Remove scripts, styles, iframes, and ad containers
	txtNav.Find("script, style, noscript, iframe, ins, .ad, .ads, .advert").Remove()

	// Remove elements with display:none
	txtNav.Find("*").Each(func(_ int, s *goquery.Selection) {
		if style, exists := s.Attr("style"); exists {
			if strings.Contains(strings.ToLower(style), "display:none") || strings.Contains(strings.ToLower(style), "display: none") {
				s.Remove()
			}
		}
	})

	// Get the cleaned HTML — let the markdown converter handle paragraph formatting
	html, err := txtNav.Html()
	if err != nil {
		return nil, fmt.Errorf("69shuba: failed to get HTML: %w", err)
	}

	// Clean up artifacts: em-space indentation (U+2003) used by 69shuba for paragraph indents
	html = strings.ReplaceAll(html, "\u2003", " ")

	content := strings.TrimSpace(html)
	if content == "" {
		return nil, fmt.Errorf("69shuba: empty content at %s", chapterURL)
	}

	return &Chapter{
		Title:     title,
		SourceURL: chapterURL,
		Content:   content,
	}, nil
}

```

---

## Store — WorkerToken CRUD

**Archivo:** `internal/store/store_worker_tokens.go`

WorkerToken struct, generateToken (SHA-256 hashed), CreateWorkerToken, ValidateWorkerToken, ListWorkerTokens, RevokeWorkerToken, DeleteWorkerToken. Tokens are stored as hashes (never plaintext).


```go
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type WorkerToken struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	ExtensionID string `json:"extensionId"`
	TokenHash   string `json:"-"`
	Label       string `json:"label"`
	LastUsedAt  string `json:"lastUsedAt,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	Revoked     bool   `json:"revoked"`
}

func generateToken() (plaintext string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	plaintext = hex.EncodeToString(b)
	h := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(h[:])
	return plaintext, hash, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Store) CreateWorkerToken(userID, extensionID, label string) (*WorkerToken, string, error) {
	plaintext, hash, err := generateToken()
	if err != nil {
		return nil, "", err
	}

	collection, err := s.App.FindCollectionByNameOrId(WorkerTokensCollection)
	if err != nil {
		return nil, "", err
	}

	record := core.NewRecord(collection)
	record.Set("owner", userID)
	record.Set("extension_id", extensionID)
	record.Set("token_hash", hash)
	record.Set("label", label)
	record.Set("revoked", false)

	if err := s.App.Save(record); err != nil {
		return nil, "", fmt.Errorf("save worker token: %w", err)
	}

	token := &WorkerToken{
		ID:          record.Id,
		UserID:      userID,
		ExtensionID: extensionID,
		TokenHash:   hash,
		Label:       label,
		CreatedAt:   record.GetString("created"),
		Revoked:     false,
	}

	return token, plaintext, nil
}

func (s *Store) ValidateWorkerToken(token string) (*WorkerToken, error) {
	hash := hashToken(token)

	records, err := s.App.FindRecordsByFilter(
		WorkerTokensCollection,
		"token_hash = {:hash} && revoked = false",
		"",
		1, 0,
		dbx.Params{"hash": hash},
	)
	if err != nil || len(records) == 0 {
		return nil, fmt.Errorf("invalid or revoked token")
	}

	record := records[0]
	ownerID := record.GetString("owner")

	record.Set("last_used_at", time.Now().Format(time.RFC3339))
	if err := s.App.Save(record); err != nil {
		return nil, fmt.Errorf("update last used: %w", err)
	}

	return &WorkerToken{
		ID:          record.Id,
		UserID:      ownerID,
		ExtensionID: record.GetString("extension_id"),
		TokenHash:   hash,
		Label:       record.GetString("label"),
		LastUsedAt:  time.Now().Format(time.RFC3339),
		CreatedAt:   record.GetString("created"),
		Revoked:     false,
	}, nil
}

func (s *Store) ListWorkerTokens(userID string) ([]WorkerToken, error) {
	records, err := s.App.FindRecordsByFilter(
		WorkerTokensCollection,
		"owner = {:owner}",
		"-created",
		100, 0,
		dbx.Params{"owner": userID},
	)
	if err != nil {
		return nil, err
	}

	tokens := make([]WorkerToken, 0, len(records))
	for _, record := range records {
		tokens = append(tokens, WorkerToken{
			ID:          record.Id,
			UserID:      userID,
			ExtensionID: record.GetString("extension_id"),
			Label:       record.GetString("label"),
			LastUsedAt:  record.GetString("last_used_at"),
			CreatedAt:   record.GetString("created"),
			Revoked:     record.GetBool("revoked"),
		})
	}
	return tokens, nil
}

func (s *Store) RevokeWorkerToken(userID, tokenID string) error {
	records, err := s.App.FindRecordsByFilter(
		WorkerTokensCollection,
		"id = {:id}",
		"",
		1, 0,
		dbx.Params{"id": tokenID},
	)
	if err != nil || len(records) == 0 {
		return ErrNotFound
	}

	record := records[0]
	if record.GetString("owner") != userID {
		return ErrForbidden
	}

	record.Set("revoked", true)
	if err := s.App.Save(record); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

func (s *Store) DeleteWorkerToken(userID, tokenID string) error {
	records, err := s.App.FindRecordsByFilter(
		WorkerTokensCollection,
		"id = {:id}",
		"",
		1, 0,
		dbx.Params{"id": tokenID},
	)
	if err != nil || len(records) == 0 {
		return ErrNotFound
	}

	record := records[0]
	if record.GetString("owner") != userID {
		return ErrForbidden
	}

	if err := s.App.Delete(record); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	return nil
}

```

---

## Store — schema: worker_tokens collection

**Archivo:** `internal/store/store_schema.go`

ensureWorkerTokensCollection creates the worker_tokens PocketBase collection with fields: owner (relation→users), extension_id, token_hash, label, last_used_at, revoked.


```go
package store

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"translator-server/internal/ai"
)

func addSystemDateFields(c *core.Collection) {
	if c.Fields.GetByName("created") == nil {
		c.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	}
	if c.Fields.GetByName("updated") == nil {
		c.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
	}
}

func (s *Store) migrateSystemDateFields(c *core.Collection) (*core.Collection, error) {
	if c.Fields.GetByName("created") != nil && c.Fields.GetByName("updated") != nil {
		return c, nil
	}
	addSystemDateFields(c)
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func enableNovelCascadeDelete(c *core.Collection) (bool, error) {
	rel, ok := c.Fields.GetByName("novel").(*core.RelationField)
	if !ok || rel == nil || rel.CascadeDelete {
		return false, nil
	}
	rel.CascadeDelete = true
	c.Fields.Add(rel)
	return true, nil
}

func (s *Store) ensureField(collection *core.Collection, field core.Field) error {
	if existing := collection.Fields.GetByName(field.GetName()); existing != nil {
		return nil
	}
	collection.Fields.Add(field)
	return s.App.Save(collection)
}

func (s *Store) migrateChapterCascadeDelete(c *core.Collection) error {
	changed, err := enableNovelCascadeDelete(c)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return s.App.Save(c)
}

func (s *Store) migrateJobCascadeDelete(c *core.Collection) error {
	changed, err := enableNovelCascadeDelete(c)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return s.App.Save(c)
}

func (s *Store) migrateEpubCascadeDelete(c *core.Collection) error {
	changed, err := enableNovelCascadeDelete(c)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return s.App.Save(c)
}

func (s *Store) ensureUsersCollection() (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(UsersCollection); err == nil {
		return s.migrateUsersCollection(existing)
	}
	c := core.NewAuthCollection(UsersCollection)
	c.ListRule = types.Pointer("@request.auth.id != '' && @request.auth.id = id")
	c.ViewRule = types.Pointer("@request.auth.id != '' && @request.auth.id = id")
	c.UpdateRule = types.Pointer("@request.auth.id != '' && @request.auth.id = id")
	c.DeleteRule = nil
	c.CreateRule = nil
	c.Fields.Add(&core.TextField{Name: "name", Max: 120})
	c.Fields.Add(&core.SelectField{Name: "theme", Values: []string{"light", "dark", "system"}, MaxSelect: 1})
	c.Fields.Add(&core.TextField{Name: "active_provider", Max: 120})
	c.Fields.Add(&core.TextField{Name: "title_provider", Max: 120})
	c.Fields.Add(&core.TextField{Name: "title_model", Max: 200})
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) migrateUsersCollection(c *core.Collection) (*core.Collection, error) {
	if err := s.ensureField(c, &core.TextField{Name: "name", Max: 120}); err != nil {
		return nil, err
	}
	if err := s.ensureField(c, &core.SelectField{Name: "theme", Values: []string{"light", "dark", "system"}, MaxSelect: 1}); err != nil {
		return nil, err
	}
	if err := s.ensureField(c, &core.TextField{Name: "active_provider", Max: 120}); err != nil {
		return nil, err
	}
	if err := s.ensureField(c, &core.TextField{Name: "title_provider", Max: 120}); err != nil {
		return nil, err
	}
	if err := s.ensureField(c, &core.TextField{Name: "title_model", Max: 200}); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ensureProvidersCollection(users *core.Collection) (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(ProvidersCollection); err == nil {
		return existing, nil
	}
	c := core.NewBaseCollection(ProvidersCollection)
	c.ListRule = types.Pointer("@request.auth.id != ''")
	c.ViewRule = types.Pointer("@request.auth.id != ''")
	c.CreateRule = nil
	c.UpdateRule = nil
	c.DeleteRule = nil
	c.Fields.Add(&core.TextField{Name: "key", Required: true, Max: 120})
	c.Fields.Add(&core.TextField{Name: "label", Required: true, Max: 120})
	c.Fields.Add(&core.TextField{Name: "base_url", Required: true, Max: 500})
	c.Fields.Add(&core.TextField{Name: "default_model", Required: true, Max: 200})
	c.Fields.Add(&core.TextField{Name: "kind", Required: true, Max: 80})
	c.Fields.Add(&core.TextField{Name: "models_json"})
	c.Fields.Add(&core.BoolField{Name: "enabled"})
	c.Fields.Add(&core.RelationField{Name: "owner", CollectionId: users.Id, MaxSelect: 1})
	c.AddIndex("idx_providers_key_unique", true, "key", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ensureUserProviderSettingsCollection(users, providers *core.Collection) (*core.Collection, error) {
	existing, err := s.App.FindCollectionByNameOrId(UserProviderSettingsCollection)
	if err == nil {
		if err := s.ensureField(existing, &core.NumberField{Name: "timeout_ms"}); err != nil {
			return nil, err
		}
		return existing, nil
	}
	c := core.NewBaseCollection(UserProviderSettingsCollection)
	ownerOnly := "@request.auth.id != '' && owner = @request.auth.id"
	c.ListRule = types.Pointer(ownerOnly)
	c.ViewRule = types.Pointer(ownerOnly)
	c.CreateRule = types.Pointer(ownerOnly)
	c.UpdateRule = types.Pointer(ownerOnly)
	c.DeleteRule = types.Pointer(ownerOnly)
	c.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1})
	c.Fields.Add(&core.RelationField{Name: "provider", Required: true, CollectionId: providers.Id, MaxSelect: 1})
	c.Fields.Add(&core.TextField{Name: "model", Max: 200})
	c.Fields.Add(&core.TextField{Name: "base_url", Max: 500})
	c.Fields.Add(&core.TextField{Name: "api_key_encrypted"})
	c.Fields.Add(&core.BoolField{Name: "api_key_configured"})
	c.Fields.Add(&core.DateField{Name: "api_key_updated_at"})
	c.Fields.Add(&core.NumberField{Name: "timeout_ms"})
	c.AddIndex("idx_user_provider_unique", true, "owner,provider", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ensureUserPromptSettingsCollection(users *core.Collection) (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(UserPromptSettingsCollection); err == nil {
		return existing, nil
	}
	c := core.NewBaseCollection(UserPromptSettingsCollection)
	ownerOnly := "@request.auth.id != '' && owner = @request.auth.id"
	c.ListRule = types.Pointer(ownerOnly)
	c.ViewRule = types.Pointer(ownerOnly)
	c.CreateRule = types.Pointer(ownerOnly)
	c.UpdateRule = types.Pointer(ownerOnly)
	c.DeleteRule = types.Pointer(ownerOnly)
	c.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1})
	c.Fields.Add(&core.TextField{Name: "key", Required: true, Max: 64})
	c.Fields.Add(&core.TextField{Name: "label", Max: 120})
	c.Fields.Add(&core.TextField{Name: "description"})
	c.Fields.Add(&core.EditorField{Name: "system_prompt"})
	c.Fields.Add(&core.EditorField{Name: "user_prompt"})
	c.Fields.Add(&core.BoolField{Name: "active"})
	c.AddIndex("idx_user_prompt_unique", true, "owner,key", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ensureUserTranslationSettingsCollection(users *core.Collection) (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(UserTranslationCollection); err == nil {
		return existing, nil
	}
	c := core.NewBaseCollection(UserTranslationCollection)
	ownerOnly := "@request.auth.id != '' && owner = @request.auth.id"
	c.ListRule = types.Pointer(ownerOnly)
	c.ViewRule = types.Pointer(ownerOnly)
	c.CreateRule = types.Pointer(ownerOnly)
	c.UpdateRule = types.Pointer(ownerOnly)
	c.DeleteRule = types.Pointer(ownerOnly)
	c.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1})
	c.Fields.Add(&core.BoolField{Name: "auto_segment"})
	c.Fields.Add(&core.NumberField{Name: "threshold_chars"})
	c.Fields.Add(&core.NumberField{Name: "max_chars"})
	c.Fields.Add(&core.NumberField{Name: "min_chars"})
	c.Fields.Add(&core.NumberField{Name: "max_retries"})
	c.Fields.Add(&core.BoolField{Name: "enable_check"})
	c.Fields.Add(&core.BoolField{Name: "include_previous_title_hints"})
	c.Fields.Add(&core.NumberField{Name: "concurrency"})
	c.AddIndex("idx_user_translation_owner_unique", true, "owner", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ensureNovelsCollection(users *core.Collection) (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(NovelsCollection); err == nil {
		return s.migrateNovelsCollection(existing)
	}
	c := core.NewBaseCollection(NovelsCollection)
	c.ListRule = types.Pointer("@request.auth.id != '' && (owner = @request.auth.id || is_public = true)")
	c.ViewRule = types.Pointer("@request.auth.id != '' && (owner = @request.auth.id || is_public = true)")
	c.CreateRule = types.Pointer("@request.auth.id != '' && owner = @request.auth.id")
	c.UpdateRule = types.Pointer("@request.auth.id != '' && owner = @request.auth.id")
	c.DeleteRule = types.Pointer("@request.auth.id != '' && owner = @request.auth.id")
	c.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1})
	c.Fields.Add(&core.TextField{Name: "source_language", Required: true, Max: 32})
	c.Fields.Add(&core.TextField{Name: "target_language", Required: true, Max: 32})
	c.Fields.Add(&core.TextField{Name: "source_title", Required: true, Max: 250})
	c.Fields.Add(&core.TextField{Name: "source_author", Max: 250})
	c.Fields.Add(&core.EditorField{Name: "source_description"})
	c.Fields.Add(&core.TextField{Name: "source_series", Max: 250})
	c.Fields.Add(&core.TextField{Name: "source_number", Max: 64})
	c.Fields.Add(&core.TextField{Name: "target_title", Max: 250})
	c.Fields.Add(&core.TextField{Name: "target_author", Max: 250})
	c.Fields.Add(&core.EditorField{Name: "target_description"})
	c.Fields.Add(&core.TextField{Name: "target_series", Max: 250})
	c.Fields.Add(&core.TextField{Name: "target_number", Max: 64})
	c.Fields.Add(&core.TextField{Name: "glossary"})
	c.Fields.Add(&core.EditorField{Name: "translation_system_prompt"})
	c.Fields.Add(&core.EditorField{Name: "translation_user_prompt"})
	c.Fields.Add(&core.EditorField{Name: "refine_system_prompt"})
	c.Fields.Add(&core.EditorField{Name: "refine_user_prompt"})
	c.Fields.Add(&core.EditorField{Name: "check_system_prompt"})
	c.Fields.Add(&core.EditorField{Name: "check_user_prompt"})
	c.Fields.Add(&core.EditorField{Name: "notes"})
	c.Fields.Add(&core.TextField{Name: "ai_options"})
	c.Fields.Add(&core.TextField{Name: "translation_options"})
	c.Fields.Add(&core.TextField{Name: "cleanup_rules"})
	c.Fields.Add(&core.TextField{Name: "url", Max: 1000})
	c.Fields.Add(&core.EditorField{Name: "custom_commands"})
	c.Fields.Add(&core.SelectField{Name: "status", Values: []string{"ongoing", "completed", "hiatus", "cancelled"}, MaxSelect: 1})
	c.Fields.Add(&core.TextField{Name: "tags"})
	c.Fields.Add(&core.FileField{Name: "cover", MaxSelect: 1})
	c.Fields.Add(&core.FileField{Name: "thumbnail", MaxSelect: 1})
	c.Fields.Add(&core.BoolField{Name: "is_public"})
	c.Fields.Add(&core.NumberField{Name: "chapter_count"})
	c.Fields.Add(&core.NumberField{Name: "translated_count"})
	c.Fields.Add(&core.NumberField{Name: "completed_count"})
	c.Fields.Add(&core.NumberField{Name: "original_char_count"})
	c.Fields.Add(&core.NumberField{Name: "translated_char_count"})
	c.Fields.Add(&core.NumberField{Name: "refined_char_count"})
	c.Fields.Add(&core.NumberField{Name: "total_char_count"})
	c.Fields.Add(&core.NumberField{Name: "max_chapter_order"})
	addSystemDateFields(c)
	c.AddIndex("idx_novels_owner", false, "owner", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) migrateNovelsCollection(c *core.Collection) (*core.Collection, error) {
	c, err := s.migrateSystemDateFields(c)
	if err != nil {
		return nil, err
	}
	for _, f := range []core.Field{
		&core.TextField{Name: "source_title", Max: 250},
		&core.TextField{Name: "source_author", Max: 250},
		&core.EditorField{Name: "source_description"},
		&core.TextField{Name: "source_series", Max: 250},
		&core.TextField{Name: "source_number", Max: 64},
		&core.TextField{Name: "target_title", Max: 250},
		&core.TextField{Name: "target_author", Max: 250},
		&core.EditorField{Name: "target_description"},
		&core.TextField{Name: "target_series", Max: 250},
		&core.TextField{Name: "target_number", Max: 64},
		&core.NumberField{Name: "chapter_count"},
		&core.NumberField{Name: "translated_count"},
		&core.NumberField{Name: "completed_count"},
		&core.NumberField{Name: "original_char_count"},
		&core.NumberField{Name: "translated_char_count"},
		&core.NumberField{Name: "refined_char_count"},
		&core.NumberField{Name: "total_char_count"},
		&core.NumberField{Name: "max_chapter_order"},
		&core.EditorField{Name: "translation_system_prompt"},
		&core.EditorField{Name: "translation_user_prompt"},
		&core.EditorField{Name: "refine_system_prompt"},
		&core.EditorField{Name: "refine_user_prompt"},
		&core.EditorField{Name: "check_system_prompt"},
		&core.EditorField{Name: "check_user_prompt"},
		&core.SelectField{Name: "status", Values: []string{"ongoing", "completed", "hiatus", "cancelled"}, MaxSelect: 1},
		&core.TextField{Name: "tags"},
	} {
		if err := s.ensureField(c, f); err != nil {
			return nil, err
		}
	}
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ensureChaptersCollection(novels *core.Collection) (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(ChaptersCollection); err == nil {
		c, err := s.migrateSystemDateFields(existing)
		if err != nil {
			return nil, err
		}
		for _, field := range []core.Field{
			&core.NumberField{Name: "original_char_count"},
			&core.NumberField{Name: "translated_char_count"},
			&core.NumberField{Name: "refined_char_count"},
		} {
			if err := s.ensureField(c, field); err != nil {
				return nil, err
			}
		}
		return c, nil
	}
	c := core.NewBaseCollection(ChaptersCollection)
	visible := "@request.auth.id != '' && (novel.owner = @request.auth.id || novel.is_public = true)"
	ownerOnly := "@request.auth.id != '' && novel.owner = @request.auth.id"
	c.ListRule = types.Pointer(visible)
	c.ViewRule = types.Pointer(visible)
	c.CreateRule = types.Pointer(ownerOnly)
	c.UpdateRule = types.Pointer(ownerOnly)
	c.DeleteRule = types.Pointer(ownerOnly)
	c.Fields.Add(&core.RelationField{Name: "novel", Required: true, CollectionId: novels.Id, MaxSelect: 1, CascadeDelete: true})
	c.Fields.Add(&core.NumberField{Name: "chapter_order", Required: true})
	c.Fields.Add(&core.TextField{Name: "title", Max: 500})
	c.Fields.Add(&core.TextField{Name: "translated_title", Max: 500})
	c.Fields.Add(&core.EditorField{Name: "original_content"})
	c.Fields.Add(&core.EditorField{Name: "translated_content"})
	c.Fields.Add(&core.EditorField{Name: "refined_content"})
	c.Fields.Add(&core.SelectField{Name: "status", Values: []string{"pending", "processing", "translated", "refined", "done", "failed"}, MaxSelect: 1})
	c.Fields.Add(&core.EditorField{Name: "error_message"})
	c.Fields.Add(&core.NumberField{Name: "original_char_count"})
	c.Fields.Add(&core.NumberField{Name: "translated_char_count"})
	c.Fields.Add(&core.NumberField{Name: "refined_char_count"})
	addSystemDateFields(c)
	c.AddIndex("idx_chapters_novel_order_unique", true, "novel,chapter_order", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ensureJobsCollection(users, novels *core.Collection) (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(JobsCollection); err == nil {
		c, err := s.migrateSystemDateFields(existing)
		if err != nil {
			return nil, err
		}
		for _, field := range []core.Field{
			&core.BoolField{Name: "auto_segment_enabled"},
			&core.BoolField{Name: "auto_segment_active"},
			&core.NumberField{Name: "auto_segment_count"},
			&core.NumberField{Name: "auto_segment_current_index"},
			&core.NumberField{Name: "auto_segment_completed_count"},
			&core.TextField{Name: "auto_segment_chapter_id", Max: 64},
			&core.TextField{Name: "auto_segment_chapter_title", Max: 500},
		} {
			if err := s.ensureField(c, field); err != nil {
				return nil, err
			}
		}
		if opField := c.Fields.GetByName("operation"); opField != nil {
			if sel, ok := opField.(*core.SelectField); ok {
				hasDownload := false
				for _, v := range sel.Values {
					if v == "download" {
						hasDownload = true
						break
					}
				}
				if !hasDownload {
					sel.Values = append(sel.Values, "download")
					if err := s.App.Save(c); err != nil {
						return nil, err
					}
				}
			}
		}
		if f := c.Fields.GetByName("options_json"); f != nil {
			if tf, ok := f.(*core.TextField); ok && tf.Max < 10000000 {
				tf.Max = 10000000
				if err := s.App.Save(c); err != nil {
					return nil, err
				}
			}
		}
		if f := c.Fields.GetByName("chapter_ids"); f != nil {
			if tf, ok := f.(*core.TextField); ok && tf.Max < 10000000 {
				tf.Max = 10000000
				if err := s.App.Save(c); err != nil {
					return nil, err
				}
			}
		}
		return c, nil
	}
	c := core.NewBaseCollection(JobsCollection)
	ownerOnly := "@request.auth.id != '' && owner = @request.auth.id"
	c.ListRule = types.Pointer(ownerOnly)
	c.ViewRule = types.Pointer(ownerOnly)
	c.CreateRule = types.Pointer(ownerOnly)
	c.UpdateRule = types.Pointer(ownerOnly)
	c.DeleteRule = nil
	c.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1})
	c.Fields.Add(&core.RelationField{Name: "novel", Required: true, CollectionId: novels.Id, MaxSelect: 1, CascadeDelete: true})
	c.Fields.Add(&core.SelectField{Name: "status", Values: []string{"pending", "running", "done", "cancelled", "failed"}, MaxSelect: 1})
	c.Fields.Add(&core.SelectField{Name: "operation", Values: []string{"translate", "refine", "download"}, MaxSelect: 1})
	c.Fields.Add(&core.TextField{Name: "provider", Max: 120})
	c.Fields.Add(&core.TextField{Name: "model", Max: 200})
	c.Fields.Add(&core.TextField{Name: "chapter_ids", Max: 10000000})
	c.Fields.Add(&core.TextField{Name: "options_json", Max: 10000000})
	c.Fields.Add(&core.EditorField{Name: "error_message"})
	c.Fields.Add(&core.NumberField{Name: "total_chapters"})
	c.Fields.Add(&core.NumberField{Name: "completed_chapters"})
	c.Fields.Add(&core.NumberField{Name: "failed_chapters"})
	c.Fields.Add(&core.BoolField{Name: "auto_segment_enabled"})
	c.Fields.Add(&core.BoolField{Name: "auto_segment_active"})
	c.Fields.Add(&core.NumberField{Name: "auto_segment_count"})
	c.Fields.Add(&core.NumberField{Name: "auto_segment_current_index"})
	c.Fields.Add(&core.NumberField{Name: "auto_segment_completed_count"})
	c.Fields.Add(&core.TextField{Name: "auto_segment_chapter_id", Max: 64})
	c.Fields.Add(&core.TextField{Name: "auto_segment_chapter_title", Max: 500})
	addSystemDateFields(c)
	c.AddIndex("idx_jobs_owner", false, "owner", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

// epubFileMaxSize overrides PocketBase's 5MB default for the epubs "file"
// field, since exported epubs for long novels (1000+ chapters) can easily
// exceed that.
const epubFileMaxSize int64 = 200 << 20 // 200MB

func (s *Store) ensureEpubsCollection(novels *core.Collection) (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(EpubsCollection); err == nil {
		c, err := s.migrateSystemDateFields(existing)
		if err != nil {
			return nil, err
		}
		return s.migrateEpubFileMaxSize(c)
	}
	c := core.NewBaseCollection(EpubsCollection)
	ownerOnly := "@request.auth.id != '' && novel.owner = @request.auth.id"
	c.ListRule = types.Pointer(ownerOnly)
	c.ViewRule = types.Pointer(ownerOnly)
	c.CreateRule = types.Pointer(ownerOnly)
	c.UpdateRule = types.Pointer(ownerOnly)
	c.DeleteRule = types.Pointer(ownerOnly)
	c.Fields.Add(&core.RelationField{Name: "novel", Required: true, CollectionId: novels.Id, MaxSelect: 1, CascadeDelete: true})
	c.Fields.Add(&core.SelectField{Name: "file_kind", Values: []string{"original", "translated"}, MaxSelect: 1})
	c.Fields.Add(&core.TextField{Name: "source_variant", Max: 64})
	c.Fields.Add(&core.TextField{Name: "label", Max: 250})
	c.Fields.Add(&core.FileField{Name: "file", Required: true, MaxSelect: 1, MaxSize: epubFileMaxSize})
	addSystemDateFields(c)
	c.AddIndex("idx_epubs_unique_variant", true, "novel,file_kind,source_variant", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

// migrateEpubFileMaxSize raises the max upload size of the "file" field on
// pre-existing epubs collections that were created before epubFileMaxSize
// was introduced (they default to PocketBase's 5MB limit).
func (s *Store) migrateEpubFileMaxSize(c *core.Collection) (*core.Collection, error) {
	field, ok := c.Fields.GetByName("file").(*core.FileField)
	if !ok || field.MaxSize >= epubFileMaxSize {
		return c, nil
	}
	field.MaxSize = epubFileMaxSize
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ensureReadingProgressCollection(users, novels *core.Collection) (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(ReadingProgressCollection); err == nil {
		return existing, nil
	}
	c := core.NewBaseCollection(ReadingProgressCollection)
	ownerOnly := "@request.auth.id != '' && user = @request.auth.id"
	c.ListRule = types.Pointer(ownerOnly)
	c.ViewRule = types.Pointer(ownerOnly)
	c.CreateRule = types.Pointer(ownerOnly)
	c.UpdateRule = types.Pointer(ownerOnly)
	c.DeleteRule = types.Pointer(ownerOnly)
	c.Fields.Add(&core.RelationField{Name: "user", Required: true, CollectionId: users.Id, MaxSelect: 1})
	c.Fields.Add(&core.RelationField{Name: "novel", Required: true, CollectionId: novels.Id, MaxSelect: 1, CascadeDelete: true})
	c.Fields.Add(&core.TextField{Name: "chapter_id", Max: 64})
	c.Fields.Add(&core.NumberField{Name: "scroll_percent"})
	addSystemDateFields(c)
	c.AddIndex("idx_reading_progress_user_novel_unique", true, "user,novel", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ensureWorkerTokensCollection(users *core.Collection) (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(WorkerTokensCollection); err == nil {
		return existing, nil
	}
	c := core.NewBaseCollection(WorkerTokensCollection)
	ownerOnly := "@request.auth.id != '' && owner = @request.auth.id"
	c.ListRule = types.Pointer(ownerOnly)
	c.ViewRule = types.Pointer(ownerOnly)
	c.CreateRule = nil
	c.UpdateRule = types.Pointer(ownerOnly)
	c.DeleteRule = types.Pointer(ownerOnly)
	c.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1})
	c.Fields.Add(&core.TextField{Name: "extension_id", Required: true, Max: 128})
	c.Fields.Add(&core.TextField{Name: "token_hash", Required: true, Max: 128})
	c.Fields.Add(&core.TextField{Name: "label", Max: 250})
	c.Fields.Add(&core.DateField{Name: "last_used_at"})
	c.Fields.Add(&core.BoolField{Name: "revoked"})
	addSystemDateFields(c)
	c.AddIndex("idx_worker_tokens_hash", true, "token_hash", "")
	c.AddIndex("idx_worker_tokens_owner", false, "owner", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) seedProviders() error {
	collection, err := s.App.FindCollectionByNameOrId(ProvidersCollection)
	if err != nil {
		return err
	}
	supported := make(map[string]struct{}, len(ai.Providers()))
	for _, info := range ai.Providers() {
		supported[info.ID] = struct{}{}
		record, err := s.App.FindFirstRecordByFilter(ProvidersCollection, "key = {:key}", dbx.Params{"key": info.ID})
		if err != nil {
			record = core.NewRecord(collection)
		}
		modelsJSON, _ := json.Marshal(info.Models)
		record.Set("key", info.ID)
		record.Set("label", info.Name)
		record.Set("base_url", info.BaseURL)
		record.Set("default_model", info.DefaultModel)
		record.Set("kind", providerKind(info))
		record.Set("models_json", string(modelsJSON))
		record.Set("enabled", true)
		if err := s.App.Save(record); err != nil {
			return err
		}
	}
	existing, err := s.App.FindRecordsByFilter(ProvidersCollection, "", "", 200, 0)
	if err != nil {
		return err
	}
	for _, record := range existing {
		if _, ok := supported[record.GetString("key")]; ok {
			continue
		}
		record.Set("enabled", false)
		if err := s.App.Save(record); err != nil {
			return err
		}
	}
	return nil
}

```

---

## Store — collection constant & EnsureSchema wiring

**Archivo:** `internal/store/store.go`

WorkerTokensCollection = "worker_tokens" constant, EnsureSchema calls ensureWorkerTokensCollection, and ErrNotFound/ErrForbidden sentinel errors used by the token layer.


```go
package store

import (
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"translator-server/internal/secure"
)

const (
	UsersCollection                = "users"
	ProvidersCollection            = "providers"
	UserProviderSettingsCollection = "user_provider_settings"
	UserPromptSettingsCollection   = "user_prompt_settings"
	UserTranslationCollection      = "user_translation_settings"
	NovelsCollection               = "novels"
	ChaptersCollection             = "chapters"
	JobsCollection                 = "translation_jobs"
	EpubsCollection                = "epubs"
	ReadingProgressCollection      = "reading_progress"
	WorkerTokensCollection         = "worker_tokens"
)

var ErrNotFound = errors.New("not found")
var ErrForbidden = errors.New("forbidden")

type Store struct {
	App       core.App
	Encryptor *secure.Encryptor
}

func New(app core.App, encryptor *secure.Encryptor) *Store {
	return &Store{App: app, Encryptor: encryptor}
}

func (s *Store) EnsureSchema() error {
	users, err := s.ensureUsersCollection()
	if err != nil {
		return err
	}
	providers, err := s.ensureProvidersCollection(users)
	if err != nil {
		return err
	}
	if _, err := s.ensureUserProviderSettingsCollection(users, providers); err != nil {
		return err
	}
	if _, err := s.ensureUserPromptSettingsCollection(users); err != nil {
		return err
	}
	if _, err := s.ensureUserTranslationSettingsCollection(users); err != nil {
		return err
	}
	novels, err := s.ensureNovelsCollection(users)
	if err != nil {
		return err
	}
	chapters, err := s.ensureChaptersCollection(novels)
	if err != nil {
		return err
	}
	if err := s.migrateChapterCascadeDelete(chapters); err != nil {
		return err
	}
	jobs, err := s.ensureJobsCollection(users, novels)
	if err != nil {
		return err
	}
	if err := s.migrateJobCascadeDelete(jobs); err != nil {
		return err
	}
	epubs, err := s.ensureEpubsCollection(novels)
	if err != nil {
		return err
	}
	if err := s.migrateEpubCascadeDelete(epubs); err != nil {
		return err
	}
	if _, err := s.ensureReadingProgressCollection(users, novels); err != nil {
		return err
	}
	if _, err := s.ensureWorkerTokensCollection(users); err != nil {
		return err
	}
	if err := s.seedProviders(); err != nil {
		return err
	}
	return nil
}

func (s *Store) ListPrompts(userID string) ([]Prompt, error) {
	defaults := []Prompt{
		{Key: "translation", Label: "Traducción", Description: "Prompt global para traducción de capítulos.", SystemPrompt: DefaultTranslationSystemPrompt, UserPrompt: DefaultTranslationUserPrompt, Active: 1},
		{Key: "title", Label: "Traducción de Título", Description: "Prompt global para traducción de títulos de capítulo.", SystemPrompt: DefaultTitleTranslationSystemPrompt, UserPrompt: DefaultTitleTranslationUserPrompt, Active: 1},
		{Key: "refine", Label: "Refinamiento", Description: "Prompt global para mejorar traducciones generadas.", SystemPrompt: DefaultRefineSystemPrompt, UserPrompt: DefaultRefineUserPrompt, Active: 1},
		{Key: "check", Label: "Verificación", Description: "Prompt global para revisar calidad de traducción.", SystemPrompt: DefaultCheckSystemPrompt, UserPrompt: DefaultCheckUserPrompt, Active: 1},
	}
	records, err := s.App.FindRecordsByFilter(UserPromptSettingsCollection, "owner = {:owner}", "", 20, 0, dbx.Params{"owner": userID})
	if err != nil {
		return nil, err
	}
	byKey := map[string]*core.Record{}
	for _, record := range records {
		byKey[record.GetString("key")] = record
	}
	out := make([]Prompt, 0, len(defaults))
	for _, item := range defaults {
		if record := byKey[item.Key]; record != nil {
			item.Label = defaultString(record.GetString("label"), item.Label)
			item.Description = defaultString(record.GetString("description"), item.Description)
			item.SystemPrompt = defaultString(record.GetString("system_prompt"), item.SystemPrompt)
			item.UserPrompt = defaultString(record.GetString("user_prompt"), item.UserPrompt)
			if !record.GetBool("active") {
				item.Active = 0
			}
			item.UpdatedAt = record.GetString("updated")
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Store) UpsertPrompt(userID string, prompt Prompt) (Prompt, error) {
	record, err := s.App.FindFirstRecordByFilter(UserPromptSettingsCollection, "owner = {:owner} && key = {:key}", dbx.Params{"owner": userID, "key": prompt.Key})
	if err != nil {
		collection, cErr := s.App.FindCollectionByNameOrId(UserPromptSettingsCollection)
		if cErr != nil {
			return Prompt{}, cErr
		}
		record = core.NewRecord(collection)
		record.Set("owner", userID)
		record.Set("key", prompt.Key)
	}
	record.Set("label", prompt.Label)
	record.Set("description", prompt.Description)
	record.Set("system_prompt", prompt.SystemPrompt)
	record.Set("user_prompt", prompt.UserPrompt)
	record.Set("active", prompt.Active != 0)
	if err := s.App.Save(record); err != nil {
		return Prompt{}, err
	}
	list, err := s.ListPrompts(userID)
	if err != nil {
		return Prompt{}, err
	}
	for _, item := range list {
		if item.Key == prompt.Key {
			return item, nil
		}
	}
	return Prompt{}, fmt.Errorf("prompt not found after update")
}

```

---

## Store — mapping: workerTokenFromRecord

**Archivo:** `internal/store/store_mapping.go`

workerTokenFromRecord helper that converts a PocketBase record to a WorkerToken struct.


```go
package store

import (
	"github.com/pocketbase/pocketbase/core"
)

func userFromRecord(record *core.Record) User {
	return User{
		ID:        record.Id,
		Email:     record.Email(),
		Name:      record.GetString("name"),
		Theme:     defaultString(record.GetString("theme"), "system"),
		CreatedAt: record.GetString("created"),
		UpdatedAt: record.GetString("updated"),
	}
}

func (s *Store) novelFromRecord(record *core.Record) Novel {
	coverFile := firstString(record.GetStringSlice("cover"))
	thumbFile := firstString(record.GetStringSlice("thumbnail"))
	return Novel{
		ID:                      record.Id,
		OwnerID:                 record.GetString("owner"),
		SourceLanguage:          record.GetString("source_language"),
		TargetLanguage:          record.GetString("target_language"),
		SourceTitle:             record.GetString("source_title"),
		SourceAuthor:            record.GetString("source_author"),
		SourceDescription:       record.GetString("source_description"),
		SourceSeries:            record.GetString("source_series"),
		SourceNumber:            record.GetString("source_number"),
		TargetTitle:             record.GetString("target_title"),
		TargetAuthor:            record.GetString("target_author"),
		TargetDescription:       record.GetString("target_description"),
		TargetSeries:            record.GetString("target_series"),
		TargetNumber:            record.GetString("target_number"),
		Glossary:                defaultString(record.GetString("glossary"), "[]"),
		TranslationSystemPrompt: record.GetString("translation_system_prompt"),
		TranslationUserPrompt:   record.GetString("translation_user_prompt"),
		RefineSystemPrompt:      record.GetString("refine_system_prompt"),
		RefineUserPrompt:        record.GetString("refine_user_prompt"),
		CheckSystemPrompt:       record.GetString("check_system_prompt"),
		CheckUserPrompt:         record.GetString("check_user_prompt"),
		Notes:                   record.GetString("notes"),
		AIOptions:               defaultString(record.GetString("ai_options"), "{}"),
		TranslationOptions:      defaultString(record.GetString("translation_options"), "{}"),
		CleanupRules:            defaultString(record.GetString("cleanup_rules"), "[]"),
		URL:                     record.GetString("url"),
		CustomCommands:          record.GetString("custom_commands"),
		Status:                  normalizeNovelStatus(record.GetString("status")),
		Tags:                    jsonString(parseNovelTagsJSON(record.GetString("tags")), "[]"),
		CoverFile:               coverFile,
		CoverPath:               buildPBFileURL(NovelsCollection, record.Id, coverFile),
		ThumbnailFile:           thumbFile,
		ThumbnailPath:           buildPBFileURL(NovelsCollection, record.Id, thumbFile),
		IsPublic:                record.GetBool("is_public"),
		ChapterCount:            asInt(record.GetFloat("chapter_count"), 0),
		TranslatedCount:         asInt(record.GetFloat("translated_count"), 0),
		CompletedCount:          asInt(record.GetFloat("completed_count"), 0),
		OriginalCharCount:       asInt(record.GetFloat("original_char_count"), 0),
		TranslatedCharCount:     asInt(record.GetFloat("translated_char_count"), 0),
		RefinedCharCount:        asInt(record.GetFloat("refined_char_count"), 0),
		TotalCharCount:          asInt(record.GetFloat("total_char_count"), 0),
		MaxChapterOrder:         asInt(record.GetFloat("max_chapter_order"), 0),
		CreatedAt:               record.GetString("created"),
		UpdatedAt:               record.GetString("updated"),
	}
}

func chapterFromRecord(record *core.Record) Chapter {
	return Chapter{
		ID:                record.Id,
		NovelID:           record.GetString("novel"),
		ChapterOrder:      asInt(record.GetFloat("chapter_order"), 0),
		Title:             record.GetString("title"),
		TranslatedTitle:   record.GetString("translated_title"),
		OriginalContent:   record.GetString("original_content"),
		TranslatedContent: record.GetString("translated_content"),
		RefinedContent:    record.GetString("refined_content"),
		Status:            defaultString(record.GetString("status"), "pending"),
		ErrorMessage:      record.GetString("error_message"),
		CreatedAt:         record.GetString("created"),
		UpdatedAt:         record.GetString("updated"),
	}
}

func jobFromRecord(record *core.Record) Job {
	return Job{
		ID:                        record.Id,
		OwnerID:                   record.GetString("owner"),
		NovelID:                   record.GetString("novel"),
		Status:                    defaultString(record.GetString("status"), "pending"),
		Operation:                 defaultString(record.GetString("operation"), "translate"),
		Provider:                  record.GetString("provider"),
		Model:                     record.GetString("model"),
		ChapterIDs:                defaultString(record.GetString("chapter_ids"), "[]"),
		OptionsJSON:               defaultString(record.GetString("options_json"), "{}"),
		ErrorMessage:              record.GetString("error_message"),
		TotalChapters:             asInt(record.GetFloat("total_chapters"), 0),
		CompletedChapters:         asInt(record.GetFloat("completed_chapters"), 0),
		FailedChapters:            asInt(record.GetFloat("failed_chapters"), 0),
		AutoSegmentEnabled:        record.GetBool("auto_segment_enabled"),
		AutoSegmentActive:         record.GetBool("auto_segment_active"),
		AutoSegmentCount:          asInt(record.GetFloat("auto_segment_count"), 0),
		AutoSegmentCurrentIndex:   asInt(record.GetFloat("auto_segment_current_index"), 0),
		AutoSegmentCompletedCount: asInt(record.GetFloat("auto_segment_completed_count"), 0),
		AutoSegmentChapterID:      record.GetString("auto_segment_chapter_id"),
		AutoSegmentChapterTitle:   record.GetString("auto_segment_chapter_title"),
		CreatedAt:                 record.GetString("created"),
		UpdatedAt:                 record.GetString("updated"),
	}
}

func epubFromRecord(record *core.Record) Epub {
	file := firstString(record.GetStringSlice("file"))
	return Epub{
		ID:            record.Id,
		NovelID:       record.GetString("novel"),
		FileKind:      record.GetString("file_kind"),
		SourceVariant: record.GetString("source_variant"),
		Label:         record.GetString("label"),
		FileName:      file,
		URL:           "/api/epubs/" + record.Id + "/download",
		CreatedAt:     record.GetString("created"),
		UpdatedAt:     record.GetString("updated"),
	}
}

func readingProgressFromRecord(record *core.Record) ReadingProgress {
	return ReadingProgress{
		ID:            record.Id,
		UserID:        record.GetString("user"),
		NovelID:       record.GetString("novel"),
		ChapterID:     record.GetString("chapter_id"),
		ScrollPercent: record.GetFloat("scroll_percent"),
		CreatedAt:     record.GetString("created"),
		UpdatedAt:     record.GetString("updated"),
	}
}

func workerTokenFromRecord(record *core.Record) WorkerToken {
	return WorkerToken{
		ID:          record.Id,
		UserID:      record.GetString("owner"),
		ExtensionID: record.GetString("extension_id"),
		Label:       record.GetString("label"),
		LastUsedAt:  record.GetString("last_used_at"),
		CreatedAt:   record.GetString("created"),
		Revoked:     record.GetBool("revoked"),
	}
}

func applyNovelToRecord(record *core.Record, novel *Novel) {
	record.Set("source_language", novel.SourceLanguage)
	record.Set("target_language", novel.TargetLanguage)
	record.Set("source_title", novel.SourceTitle)
	record.Set("source_author", novel.SourceAuthor)
	record.Set("source_description", novel.SourceDescription)
	record.Set("source_series", novel.SourceSeries)
	record.Set("source_number", novel.SourceNumber)
	record.Set("target_title", novel.TargetTitle)
	record.Set("target_author", novel.TargetAuthor)
	record.Set("target_description", novel.TargetDescription)
	record.Set("target_series", novel.TargetSeries)
	record.Set("target_number", novel.TargetNumber)
	record.Set("glossary", defaultString(novel.Glossary, "[]"))
	record.Set("translation_system_prompt", novel.TranslationSystemPrompt)
	record.Set("translation_user_prompt", novel.TranslationUserPrompt)
	record.Set("refine_system_prompt", novel.RefineSystemPrompt)
	record.Set("refine_user_prompt", novel.RefineUserPrompt)
	record.Set("check_system_prompt", novel.CheckSystemPrompt)
	record.Set("check_user_prompt", novel.CheckUserPrompt)
	record.Set("notes", novel.Notes)
	record.Set("ai_options", defaultString(novel.AIOptions, "{}"))
	record.Set("translation_options", defaultString(novel.TranslationOptions, "{}"))
	record.Set("cleanup_rules", defaultString(novel.CleanupRules, "[]"))
	record.Set("url", novel.URL)
	record.Set("custom_commands", novel.CustomCommands)
	record.Set("status", normalizeNovelStatus(novel.Status))
	record.Set("tags", jsonString(parseNovelTagsJSON(novel.Tags), "[]"))
	record.Set("is_public", novel.IsPublic)
	record.Set("chapter_count", novel.ChapterCount)
	record.Set("translated_count", novel.TranslatedCount)
	record.Set("completed_count", novel.CompletedCount)
	record.Set("original_char_count", novel.OriginalCharCount)
	record.Set("translated_char_count", novel.TranslatedCharCount)
	record.Set("refined_char_count", novel.RefinedCharCount)
	record.Set("total_char_count", novel.TotalCharCount)
	record.Set("max_chapter_order", novel.MaxChapterOrder)
}

```

---

## Extension manifest

**Archivo:** `browser-worker/manifest.json`

Chrome Extension Manifest v3 — permissions (tabs, activeTab, storage, scripting, <all_urls>), service worker, popup, OAuth callback page.


```json
{
  "manifest_version": 3,
  "name": "Yara Browser Worker",
  "version": "1.0.0",
  "description": "Browser proxy for Yara - fetches pages through real browser to bypass Cloudflare",
  "permissions": [
    "tabs",
    "activeTab",
    "storage",
    "scripting"
  ],
  "host_permissions": [
    "<all_urls>"
  ],
  "background": {
    "service_worker": "background/service-worker.js",
    "type": "module"
  },
  "action": {
    "default_popup": "popup/popup.html",
    "default_icon": {
      "16": "icons/icon16.png",
      "48": "icons/icon48.png",
      "128": "icons/icon128.png"
    }
  },
  "web_accessible_resources": [
    {
      "resources": ["auth/auth.html", "auth/auth.js"],
      "matches": ["<all_urls>"]
    }
  ],
  "icons": {
    "16": "icons/icon16.png",
    "48": "icons/icon48.png",
    "128": "icons/icon128.png"
  }
}

```

---

## WebSocket protocol constants

**Archivo:** `browser-worker/shared/protocol.js`

MessageType (JOB_REQUEST, PING, JOB_RESULT, PONG, HEARTBEAT, REGISTER), JobStatus (OK, ERROR, CHALLENGE, WAITING_USER), WorkerState enum, createMessage/parseMessage helpers.


```js
export const MessageType = {
  // Server -> Extension
  JOB_REQUEST: 'job_request',
  PING: 'ping',
  CANCEL_JOB: 'cancel_job',
  REGISTER_RESPONSE: 'register_response',

  // Extension -> Server
  JOB_RESULT: 'job_result',
  PONG: 'pong',
  HEARTBEAT: 'heartbeat',
  REGISTER: 'register',

  // Internal
  STATUS_UPDATE: 'status_update',
};

export const JobStatus = {
  OK: 'ok',
  ERROR: 'error',
  CHALLENGE: 'challenge',
  WAITING_USER: 'waiting_user',
};

export const WorkerState = {
  DISCONNECTED: 'disconnected',
  CONNECTING: 'connecting',
  CONNECTED: 'connected',
  IDLE: 'idle',
  DOWNLOADING: 'downloading',
  RECOVERING: 'recovering',
  UNAUTHENTICATED: 'unauthenticated',
};

export function createMessage(type, payload = {}) {
  return JSON.stringify({ type, payload, timestamp: Date.now() });
}

export function parseMessage(data) {
  try {
    const msg = typeof data === 'string' ? JSON.parse(data) : data;
    return { type: msg.type, payload: msg.payload || {}, timestamp: msg.timestamp };
  } catch {
    return null;
  }
}

```

---

## Chrome storage helpers

**Archivo:** `browser-worker/shared/storage.js`

getConfig, setConfig, getWorkerToken, setWorkerToken, clearWorkerToken — manages server address, auto-connect, and auth token.


```js
const STORAGE_KEY = 'yara_browser_worker';
const WORKER_TOKEN_KEY = 'workerToken';
const WORKER_USER_KEY = 'workerUserId';
const WORKER_CONNECTED_KEY = 'workerConnectedAt';

const defaults = {
  serverAddr: 'localhost:5176',
  autoConnect: true,
  heartbeatInterval: 5000,
};

export async function getConfig() {
  const result = await chrome.storage.local.get(STORAGE_KEY);
  const stored = result[STORAGE_KEY] || {};
  // Migrate old ws:// URL format to new host:port format
  if (stored.serverUrl && !stored.serverAddr) {
    try {
      const u = new URL(stored.serverUrl);
      stored.serverAddr = u.host;
    } catch {
      stored.serverAddr = defaults.serverAddr;
    }
    delete stored.serverUrl;
  }
  return { ...defaults, ...stored };
}

export async function setConfig(patch) {
  const current = await getConfig();
  const merged = { ...current, ...patch };
  await chrome.storage.local.set({ [STORAGE_KEY]: merged });
  return merged;
}

export async function getWorkerToken() {
  const result = await chrome.storage.local.get([WORKER_TOKEN_KEY, WORKER_USER_KEY, WORKER_CONNECTED_KEY]);
  return {
    token: result[WORKER_TOKEN_KEY] || null,
    userId: result[WORKER_USER_KEY] || null,
    connectedAt: result[WORKER_CONNECTED_KEY] || null,
  };
}

export async function setWorkerToken(token, userId) {
  await chrome.storage.local.set({
    [WORKER_TOKEN_KEY]: token,
    [WORKER_USER_KEY]: userId,
    [WORKER_CONNECTED_KEY]: new Date().toISOString(),
  });
}

export async function clearWorkerToken() {
  await chrome.storage.local.remove([WORKER_TOKEN_KEY, WORKER_USER_KEY, WORKER_CONNECTED_KEY]);
}

```

---

## Service worker — core proxy logic

**Archivo:** `browser-worker/background/service-worker.js`

WebSocket connection management, heartbeat, reconnect with exponential backoff, OAuth token registration. Core proxy strategy: 1) try background fetch() (shares cf_clearance cookies), 2) if challenge detected, open a hidden tab for Cloudflare challenge solving, 3) reuse challenge tab for same origin.


```js
import { MessageType, JobStatus, WorkerState, createMessage, parseMessage } from '../shared/protocol.js';
import { getConfig, setConfig, getWorkerToken } from '../shared/storage.js';

let ws = null;
let state = WorkerState.DISCONNECTED;
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
  chrome.tabs.onUpdated.addListener(handleTabUpdate);
}

function handleTabUpdate(tabId, changeInfo, tab) {
  if (changeInfo.status !== 'complete') return;
  const url = tab.url || '';
  const match = url.match(/\/api\/worker-auth\/callback\?token=([^&]+)&user=([^&]+)/);
  if (!match) return;

  const token = decodeURIComponent(match[1]);
  const userId = decodeURIComponent(match[2]);
  log('Auth callback detected, saving token...');

  chrome.storage.local.set({
    workerToken: token,
    workerUserId: userId,
    workerConnectedAt: new Date().toISOString()
  }, () => {
    chrome.tabs.remove(tabId).catch(() => {});
    chrome.runtime.sendMessage({ type: 'auth_complete', token, userId }).catch(() => {});
    disconnect();
    connect();
  });
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
      log('WebSocket connected, waiting for registration...');
      setState(WorkerState.CONNECTING);
      reconnectDelay = 1000;
      sendRegister(tokenData.token);
    };

    ws.onmessage = (event) => {
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
    case MessageType.REGISTER_RESPONSE:
      if (msg.payload.ok) {
        log('Registration successful');
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
        log('Challenge solved, waiting for redirects to settle...');
        // Wait a few seconds for any post-challenge redirects to complete
        // (many sites redirect after cf_clearance is set).
        await sleep(4000);

        // Verify the page actually loaded (not another challenge)
        const verifyChallenge = await checkForChallenge(tab.id);
        if (verifyChallenge) {
          log('Page still shows challenge after wait, continuing to poll...');
          await sleep(3000);
          continue;
        }

        log('Challenge fully resolved, extracting HTML...');
        const results = await chrome.scripting.executeScript({
          target: { tabId: tab.id },
          func: () => ({
            html: document.documentElement.outerHTML,
            text: document.body.innerText,
            title: document.title,
            url: window.location.href,
          }),
        });

        // Close the challenge tab — cf_clearance cookie persists in the
        // browser's cookie store regardless of whether the tab is open.
        try { await chrome.tabs.remove(tab.id); } catch { /* already closed */ }
        challengeTabId = null;
        challengeOrigin = null;

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

```

---

## Popup HTML

**Archivo:** `browser-worker/popup/popup.html`

Extension popup UI — status bar, auth panel, Cloudflare challenge panel, server address input, connect/disconnect buttons, info panel showing browser/uptime/token status.


```html
<!DOCTYPE html>
<html lang="es">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Yara Browser Worker</title>
  <link rel="stylesheet" href="popup.css">
</head>
<body>
  <div class="container">
    <div class="header">
      <img src="../icons/icon48.png" alt="Yara" class="logo">
      <h1>Browser Worker</h1>
    </div>

    <div id="status-bar" class="status-bar disconnected">
      <span class="status-dot"></span>
      <span id="status-text">Desconectado</span>
    </div>

    <div id="auth-panel" class="auth-panel hidden">
      <div class="auth-icon">&#128274;</div>
      <div class="auth-text">No autenticado</div>
      <div class="auth-hint">Conecta tu cuenta para usar el worker</div>
      <button id="btn-auth" class="btn btn-primary">Autenticar con el Servidor</button>
    </div>

    <div id="challenge-panel" class="challenge-panel hidden">
      <div class="challenge-icon">&#9888;</div>
      <div class="challenge-text">Cloudflare Challenge Detectado</div>
      <div class="challenge-hint">Resuelve el challenge en la pesta&ntilde;a del navegador</div>
      <button id="btn-refresh" class="btn btn-warning">Refrescar P&aacute;gina</button>
    </div>

    <div class="form-group">
      <label for="server-addr">Servidor</label>
      <input type="text" id="server-addr" placeholder="192.168.1.100:5176" spellcheck="false">
      <small class="help-text">host:puerto del servidor Yara</small>
    </div>

    <div class="form-group">
      <label>
        <input type="checkbox" id="auto-connect" checked>
        Conectar autom&aacute;ticamente al iniciar
      </label>
    </div>

    <div class="button-group">
      <button id="btn-connect" class="btn btn-primary">Conectar</button>
      <button id="btn-disconnect" class="btn btn-secondary" disabled>Desconectar</button>
    </div>

    <div id="info-panel" class="info-panel hidden">
      <div class="info-row">
        <span class="info-label">Navegador:</span>
        <span id="info-browser" class="info-value">-</span>
      </div>
      <div class="info-row">
        <span class="info-label">Uptime:</span>
        <span id="info-uptime" class="info-value">-</span>
      </div>
      <div class="info-row">
        <span class="info-label">Token:</span>
        <span id="info-token" class="info-value">-</span>
      </div>
    </div>

    <div id="error-panel" class="error-panel hidden">
      <span id="error-text"></span>
    </div>
  </div>

  <script type="module" src="popup.js"></script>
</body>
</html>

```

---

## Popup JS

**Archivo:** `browser-worker/popup/popup.js`

Popup logic — state-driven UI updates, connect/disconnect, OAuth trigger, Cloudflare challenge notification, server address persistence.


```js
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

  const tokenData = await getWorkerToken();
  updateAuthUI(tokenData);

  const response = await chrome.runtime.sendMessage({ type: 'GET_STATE' });
  updateUI(response.state, response.connected, tokenData);

  chrome.runtime.onMessage.addListener((msg) => {
    if (msg.type === 'STATE_CHANGED') {
      getWorkerToken().then(td => updateUI(msg.state, null, td));
    }
    if (msg.type === 'CHALLENGE_DETECTED') showChallenge(msg.url, msg.tabId);
    if (msg.type === 'AUTH_COMPLETE') {
      getWorkerToken().then(updateAuthUI);
    }
  });
}

async function updateUI(state, connected, tokenData) {
  if (!tokenData) tokenData = await getWorkerToken();
  const hasToken = !!(tokenData && tokenData.token);

  statusEl.className = `status-bar ${state}`;
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

  const tokenData = await getWorkerToken();
  if (!tokenData || !tokenData.token) {
    authPanel.classList.remove('hidden');
    return;
  }

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

```

---

## Popup CSS

**Archivo:** `browser-worker/popup/popup.css`

Popup styling — dark theme, status indicators (disconnected/connecting/connected/downloading), challenge panel with warning colors, form inputs in dark mode.


```css
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  width: 340px;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  background: #1a1a2e;
  color: #e0e0e0;
}

.container {
  padding: 16px;
}

.header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}

.logo {
  width: 32px;
  height: 32px;
}

h1 {
  font-size: 16px;
  font-weight: 600;
  color: #fff;
}

.status-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 6px;
  margin-bottom: 14px;
  font-size: 13px;
  font-weight: 500;
  transition: background 0.2s;
}

.status-bar.disconnected {
  background: #3d1f1f;
  color: #f87171;
}

.status-bar.connecting {
  background: #3d3520;
  color: #fbbf24;
}

.status-bar.connected {
  background: #1f3d2a;
  color: #4ade80;
}

.status-bar.downloading {
  background: #1f2d3d;
  color: #60a5fa;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.disconnected .status-dot {
  background: #f87171;
}

.connecting .status-dot {
  background: #fbbf24;
  animation: pulse 1s infinite;
}

.connected .status-dot {
  background: #4ade80;
}

.downloading .status-dot {
  background: #60a5fa;
  animation: pulse 0.5s infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

.form-group {
  margin-bottom: 12px;
}

.form-group label {
  display: block;
  font-size: 12px;
  font-weight: 500;
  color: #9ca3af;
  margin-bottom: 4px;
}

.form-group input[type="text"] {
  width: 100%;
  padding: 8px 10px;
  background: #0f0f23;
  border: 1px solid #374151;
  border-radius: 6px;
  color: #e0e0e0;
  font-size: 13px;
  font-family: 'SF Mono', 'Fira Code', monospace;
  outline: none;
  transition: border-color 0.2s;
}

.form-group input[type="text"]:focus {
  border-color: #6366f1;
}

.form-group input[type="checkbox"] {
  margin-right: 6px;
  accent-color: #6366f1;
}

.form-group label:has(input[type="checkbox"]) {
  display: flex;
  align-items: center;
  cursor: pointer;
  color: #d1d5db;
  font-size: 13px;
}

.help-text {
  display: block;
  font-size: 11px;
  color: #6b7280;
  margin-top: 3px;
}

.button-group {
  display: flex;
  gap: 8px;
  margin-bottom: 14px;
}

.btn {
  flex: 1;
  padding: 8px 12px;
  border: none;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.btn-primary {
  background: #6366f1;
  color: #fff;
}

.btn-primary:hover:not(:disabled) {
  background: #5558e6;
}

.btn-secondary {
  background: #374151;
  color: #d1d5db;
}

.btn-secondary:hover:not(:disabled) {
  background: #4b5563;
}

.info-panel {
  background: #0f0f23;
  border-radius: 6px;
  padding: 10px 12px;
}

.info-row {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  padding: 3px 0;
}

.info-label {
  color: #6b7280;
}

.info-value {
  color: #d1d5db;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.error-panel {
  background: #3d1f1f;
  border-radius: 6px;
  padding: 8px 12px;
  font-size: 12px;
  color: #f87171;
  margin-top: 10px;
}

.challenge-panel {
  background: linear-gradient(135deg, #3d3520 0%, #2d2515 100%);
  border: 1px solid #fbbf24;
  border-radius: 8px;
  padding: 14px;
  margin-bottom: 14px;
  text-align: center;
}

.challenge-icon {
  font-size: 28px;
  margin-bottom: 8px;
}

.challenge-text {
  font-size: 14px;
  font-weight: 600;
  color: #fbbf24;
  margin-bottom: 4px;
}

.challenge-hint {
  font-size: 11px;
  color: #d4a017;
  margin-bottom: 10px;
}

.btn-warning {
  background: #f59e0b;
  color: #000;
}

.btn-warning:hover:not(:disabled) {
  background: #d97706;
}

.hidden {
  display: none;
}

```

---

## Auth callback HTML

**Archivo:** `browser-worker/auth/auth.html`

OAuth callback page — receives token from server via URL parameter, displays success/error state.


```html
<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Autenticación del Worker</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #0f0f0f;
            color: #e0e0e0;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .card {
            background: #1a1a1a;
            border: 1px solid #2a2a2a;
            border-radius: 12px;
            padding: 32px;
            max-width: 400px;
            width: 90%;
            text-align: center;
        }
        .icon {
            width: 64px;
            height: 64px;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 20px;
        }
        .icon.success { background: #166534; }
        .icon.error { background: #7f1d1d; }
        .icon svg {
            width: 32px;
            height: 32px;
        }
        .icon.success svg { stroke: #4ade80; }
        .icon.error svg { stroke: #f87171; }
        h1 {
            font-size: 20px;
            font-weight: 600;
            margin-bottom: 8px;
            color: #fff;
        }
        p {
            font-size: 14px;
            color: #888;
            margin-bottom: 20px;
        }
        .btn {
            display: inline-block;
            padding: 10px 24px;
            background: #3b82f6;
            color: #fff;
            border: none;
            border-radius: 8px;
            font-size: 14px;
            font-weight: 500;
            cursor: pointer;
            text-decoration: none;
        }
        .btn:hover { background: #2563eb; }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon success" id="icon">
            <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="20 6 9 17 4 12"></polyline>
            </svg>
        </div>
        <h1 id="title">Procesando...</h1>
        <p id="message">Guardando token de autenticación...</p>
        <button class="btn" id="closeBtn" style="display: none;">Cerrar</button>
    </div>
    <script src="auth.js"></script>
</body>
</html>

```

---

## Auth callback JS

**Archivo:** `browser-worker/auth/auth.js`

Extracts token from URL query string, stores in chrome.storage.local, sends auth_complete message to service worker.


```js
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

```

---

## Resumen de problemas detectados

| Prioridad | Problema | Archivo(s) | Impacto |
|-----------|----------|------------|---------|
| 🔴 CRÍTICO | Race condition: dispatchBrowserJob roba resultado del canal | router_browser_worker.go:315-329 | Descargas fallan o cola se atasca 5 min |
| 🔴 CRÍTICO | Cloudflare bloquea HTTP directo | client.go + browser_worker_fallback.go | Sin proxy, no funciona |
| 🟡 ALTA | EnqueueBrowserJob usa len(browserWorkers) no autenticados | router_browser_worker.go:347 | Jobs encolados pero nadie los procesa |
| 🟡 ALTA | Selectores CSS catálogo obsoletos | 69shuba_metadata.go:233-267 | No se encuentran capítulos |
| 🟡 ALTA | Selector .qustime en info page obsoleto | 69shuba_metadata.go:174-206 | Fallback no funciona |
| 🟡 ALTA | Selector .txtnav en chapter obsoleto | 69shuba_chapters.go:36-43 | No se extrae contenido |
| 🟡 MEDIA | ensureChapterExtension asume /txt/ URL | 69shuba_metadata.go:323-328 | URLs malformadas |
| 🟡 MEDIA | Regex URLs pueden no matchear | 69shuba.go:12-14 | bookID no extraíble |
| 🟢 BAJA | HTTP directo siempre falla antes de proxy | runtime_worker.go | ~30s latencia extra por capítulo |
| 🟢 BAJA | GBK decoding duplicado | client.go + 69shuba*.go | Código redundante |


## Siguientes pasos recomendados

1. **Verificar que los bugs 1 y 2 estén corregidos** — Revisar `router_browser_worker.go`
   para confirmar que dispatchBrowserJob ya no lee de job.Result y EnqueueBrowserJob usa
   HasBrowserWorker(). Ambos están corregidos en el commit actual.
2. **Probar nuevamente** — Conectar el browser worker (OAuth), verificar que aparezca como
   "authenticated" en `/api/browser-workers`, luego probar preview desde URL 69shuba.
3. **Optimizar fallback** — En `runtime_worker.go`, si `IsBrowserRequiredSite` es true
   y hay browser worker, saltar HTTP directo y usar proxy directamente (evita ~30s de
   latencia por capítulo). Actualmente la lógica es: intenta HTTP directo → falla → proxy.
   Debería ser: si IsBrowserRequiredSite → solo proxy.
4. **Verificar selectores CSS de 69shuba** — Si el catálogo se obtiene vía proxy pero no
   se encuentran capítulos, actualizar los selectores en `fetchChapterList` y
   `extractChaptersFromInfoPage` para que coincidan con el HTML actual de 69shuba.
