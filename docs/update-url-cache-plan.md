# Plan: Cache en memoria para actualización de novela desde URL

## Problema actual

Cuando el usuario actualiza una novela desde URL, el sistema scrapea la fuente **dos veces**:

1. `GET /update-preview` — scrape → retorna resumen (sin URLs de capítulos)
2. `POST /update-from-url` — scrape **otra vez** → crea job de descarga

Esto es redundante y costoso: dos requests HTTP al mismo sitio, con delay de rate-limiting incluido.

## Solución

Guardar la lista de capítulos (con URLs) en un caché en memoria después del primer scrape. El segundo endpoint reutiliza esa lista en lugar de volver a scrapear. La entrada se borra inmediatamente después de crear el job.

```
Preview  → scrape → guarda en caché → responde al frontend
Update   → lee de caché → crea job → BORRA la entrada de caché
```

Si no hay caché (preview no se hizo, o usuario accedió directamente al update), hace fallback al scrape normal.

## Cambios por archivo

### 1. `internal/api/router.go` — Campo en Server + inicialización

Agregar al struct `Server`:
```go
previewCacheMu sync.RWMutex
previewCache   map[string]previewCacheEntry // key: "{userID}:{novelID}"
```

En `New()`, inicializar:
```go
previewCache: make(map[string]previewCacheEntry),
```

### 2. `internal/api/router_import.go` — Tipo + handlers

**Constante y tipo nuevos** (junto a `chapterOrderRegex`):
```go
const previewCacheTTL = 15 * time.Minute

type previewCacheEntry struct {
    chapters  []noveldownloader.ChapterURL
    createdAt time.Time
}
```

**Handler `GET /update-preview`** (línea 368): después de obtener `info` del scrape, guardar en caché con auto-expiración:
```go
cacheKey := e.Auth.Id + ":" + novelID
s.previewCacheMu.Lock()
s.previewCache[cacheKey] = previewCacheEntry{
    chapters:  info.Chapters,
    createdAt: time.Now(),
}
s.previewCacheMu.Unlock()

// Auto-borrar después del TTL por si el usuario nunca confirma
time.AfterFunc(previewCacheTTL, func() {
    s.previewCacheMu.Lock()
    defer s.previewCacheMu.Unlock()
    if entry, exists := s.previewCache[cacheKey]; exists {
        if time.Since(entry.createdAt) >= previewCacheTTL {
            delete(s.previewCache, cacheKey)
        }
    }
})
```

**Handler `POST /update-from-url`** (línea 424): reemplazar el segundo scrape (líneas 440-444) con:
```go
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
```

Luego usar `chapters` en lugar de `info.Chapters` en todo el resto del handler.

### 3. `internal/api/import_url_test.go` — Tests existentes

- Los tests existentes siguen funcionando: `TestUpdateFromUrlRangeIncludesEndChapter` llama `update-from-url` sin preview previo, el fallback al scrape cubre ese caso.
- Opcional: agregar test de integración preview→update verificando que solo se hace 1 request HTTP al mock.

### 4. Frontend — sin cambios

El contrato API no cambia. El frontend ya hace preview→update en secuencia.

## Edge cases

| Caso | Comportamiento |
|------|----------------|
| Server restart entre preview y update | Caché perdida, update hace fallback a scrape |
| Usuario preview pero nunca confirma | `time.AfterFunc` borra la entrada después de 15 minutos |
| Bulk update (`/operations`) | Flow independiente, no usa estos endpoints |
| Mismo usuario, preview de otra novela | Keys distintas (`userID:novelID`), sin conflicto |
| Dos usuarios, misma novela | Keys distintas (incluyen userID), sin conflicto |

## Archivos a modificar

| Archivo | Cambio |
|---------|--------|
| `internal/api/router.go` | Campo `previewCache` + `previewCacheMu` + init en `New()` |
| `internal/api/router_import.go` | Tipo `previewCacheEntry` + preview guarda + update lee y borra |
| `internal/api/import_url_test.go` | Opcional: test integración preview→update |
