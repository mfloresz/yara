# Plan: Boot rápido, SQLite sin contención y timeouts HTTP

## Problemas a corregir

1. **Backfills pesados en cada boot** — `BackfillChapterCharCounts` y `BackfillNovelStats` se ejecutan síncronamente en `EnsureSchema` realizando miles de transacciones y lecturas/escrituras. El servidor no puede aceptar peticiones de forma fiable hasta que terminan.
2. **SQLite sin `busy_timeout`** — Bajo concurrencia, escrituras simultáneas generan `SQLITE_BUSY` inmediato, abortando jobs y requests.
3. **Sin timeouts HTTP** — Handlers bloqueantes (`check-batch-updates`, importaciones) mantienen goroutines ocupadas indefinidamente, generando conexiones zombie.

## Principios de diseño

- **Boot minimalista:** `EnsureSchema` solo modifica estructura (colecciones y campos). Nunca ejecuta data migrations o backfills sincrónicos.
- **Preparado para consultar inmediatamente:** Una vez que el servidor empieza a escuchar, la base de datos debe responder sin operaciones pesadas pendientes.
- **Marcado de migrations:** Usar un archivo marker persistente en `<data-dir>` para marcar qué backfills ya fueron ejecutados, evitando reescaneos en boots sucesivos.
- **Filtrado sobre escaneo completo:** Los backfills solo procesan registros que realmente necesitan el dato faltante.
- **Fail-fast en timeouts:** Ningún request HTTP puede bloquear un goroutine del pool de workers indefinidamente.

---

## Fix 1: Backfills pesados en cada boot

### 1.1 Extraer backfills de `EnsureSchema`

**Archivo:** `internal/store/store.go`

Eliminar las llamadas síncronas a `BackfillChapterCharCounts()` y `BackfillNovelStats()` del método `EnsureSchema()`.

```go
// ANTES
func (s *Store) EnsureSchema() error {
    // ... ensure collections ...
    if err := s.BackfillChapterCharCounts(); err != nil {
        return err
    }
    if err := s.BackfillNovelStats(); err != nil {
        return err
    }
    return nil
}

// DESPUÉS
func (s *Store) EnsureSchema() error {
    // ... ensure collections ...
    return nil
}
```

Esto reduce el tiempo de boot de segundos/minutos a milisegundos.

### 1.2 Agregar método público `RunPendingBackfills`

**Archivo:** `internal/store/store_chapters.go`

Crear un método que verifique un archivo marker persistente y ejecute los backfills solo si es necesario. PocketBase v0.39.4 no expone `Settings().Get/Set` genéricos; se usa un archivo en `<data-dir>` como marcador:

```go
import (
    "context"
    "os"
    "path/filepath"
)

const backfillMarkerFile = ".backfills-v1.done"

func (s *Store) backfillMarkerPath() string {
    return filepath.Join(s.App.DataDir(), backfillMarkerFile)
}

func (s *Store) RunPendingBackfills(ctx context.Context) error {
    if _, err := os.Stat(s.backfillMarkerPath()); err == nil {
        return nil // backfill ya ejecutado en un boot anterior
    }

    if err := s.BackfillChapterCharCountsFiltered(ctx); err != nil {
        return err
    }
    if err := s.BackfillNovelStatsFiltered(ctx); err != nil {
        return err
    }
    if err := os.WriteFile(s.backfillMarkerPath(), []byte("done"), 0644); err != nil {
        return err
    }
    return nil
}
```

El archivo `<data-dir>/.backfills-v1.done` persiste entre reinicios. Si el servidor se detiene a mitad del backfill, el archivo no se escribe y el próximo boot reintenta.

### 1.3 Filtrar backfills para procesar solo registros que lo necesitan

**Archivo:** `internal/store/store_chapters.go`

`BackfillChapterCharCountsFiltered` debe usar un filtro para traer solo capítulos con contenido pero sin char counts poblados:

```go
func (s *Store) BackfillChapterCharCountsFiltered(ctx context.Context) error {
    filter := "original_char_count = 0 && (original_content != '' || translated_content != '' || refined_content != '')"
    offset := 0
    pageSize := 500
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        records, err := s.App.FindRecordsByFilter(ChaptersCollection, filter, "", pageSize, offset)
        if err != nil {
            return err
        }
        if len(records) == 0 {
            break
        }
        for _, record := range records {
            setCharCounts(record, record.GetString("original_content"), record.GetString("translated_content"), record.GetString("refined_content"))
            if err := s.App.Save(record); err != nil {
                return err
            }
        }
        offset += len(records)
    }
    return nil
}
```

`BackfillNovelStatsFiltered` debe filtrar novelas con `chapter_count = 0` (stats no calculadas):

```go
func (s *Store) BackfillNovelStatsFiltered(ctx context.Context) error {
    filter := "chapter_count = 0"
    offset := 0
    pageSize := 5000
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        novels, err := s.App.FindRecordsByFilter(NovelsCollection, filter, "", pageSize, offset)
        if err != nil {
            return err
        }
        if len(novels) == 0 {
            break
        }
        for _, novel := range novels {
            if err := s.RecalculateNovelStats(novel.Id); err != nil {
                return err
            }
        }
        offset += len(novels)
    }
    return nil
}
```

Nota: Si `chapter_count` está en 0 porque la novela realmente no tiene capítulos, `RecalculateNovelStats` hará un save con todo en 0. Eso es correcto y deja la novela "backfilled". Novelas con capítulos pero sin stats por una migración previa quedarán correctas con esta llamada.

### 1.4 Ejecutar backfills asíncronamente después del boot

**Archivo:** `internal/api/router.go`

Agregar `"log/slog"` a los imports del archivo. Lanzar una goroutine que ejecute `RunPendingBackfills` al final de `api.New`. La goroutine se inicia antes de que el servidor empiece a escuchar, pero corre concurrentemente — el servidor acepta peticiones inmediatamente mientras el backfill avanza en background:

- El servidor acepta peticiones inmediatamente.
- La base de datos está disponible para consultas desde el primer request.
- Los backfills se ejecutan en background sin bloquear el event loop HTTP.

```go
func (s *Server) StartBackfills() {
    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        defer cancel()
        if err := s.Store.RunPendingBackfills(ctx); err != nil {
            slog.Error("background backfill failed", "error", err)
        }
    }()
}
```

Llamar a `s.StartBackfills()` al final de `api.New`.

**Nota sobre shutdown:** El `context.Background()` no se cancela automáticamente al apagar el servidor. Para una implementación más robusta, recibir un `context.Context` de shutdown del caller (ej: `context.WithCancel(mainCtx)`) y pasarlo al método. En la práctica, el proceso se detiene y la goroutine muere con él.

---

## Fix 2: SQLite sin `busy_timeout` — contención bajo concurrencia

### 2.1 Configurar `busy_timeout` después del bootstrap

**Archivo:** `cmd/server/main.go`

Agregar `"time"` a los imports del archivo. Después de `app.Bootstrap()` y antes de crear el `Store`, ejecutar el PRAGMA:

```go
if _, err := app.DB().NewQuery("PRAGMA busy_timeout = 5000").Execute(); err != nil {
    slog.Warn("failed to set sqlite busy_timeout", "error", err)
}
```

Esto hace que SQLite espere hasta 5 segundos antes de devolver `SQLITE_BUSY`, eliminando la mayoría de los fallos por contención momentánea entre el worker y los handlers.

### 2.2 Verificar WAL (opcional pero recomendado)

Confirmar que WAL está activo. Los archivos `.db-wal` en `/data/` indican que sí. Si alguna instalación no tuviera WAL, ejecutar:

```go
app.DB().NewQuery("PRAGMA journal_mode = WAL").Execute()
```

### 2.3 Transacciones agrupadas en backfills (si aplica)

Si durante `BackfillChapterCharCountsFiltered` hay muchos registros, envolver lotes de `Save()` en transacciones de PocketBase:

```go
// Dentro del loop de BackfillChapterCharCountsFiltered
if err := s.App.RunInTransaction(func(txApp core.App) error {
    for _, record := range batch {
        if err := txApp.Save(record); err != nil {
            return err
        }
    }
    return nil
}); err != nil {
    return err
}
```

Esto reduce el overhead de `BEGIN`/`COMMIT` por cada registro.

---

## Fix 3: Sin timeouts HTTP — conexiones zombies

### 3.1 Reemplazar `http.ListenAndServe` por `http.Server`

**Archivo:** `cmd/server/main.go`

Asegurar que `"time"` está en los imports. Reemplazar `http.ListenAndServe` por `http.Server` con timeouts:

```go
// ANTES
if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
    slog.Error("server error", "error", err)
    os.Exit(1)
}

// DESPUÉS
srv := &http.Server{
    Addr:         cfg.Addr,
    Handler:      handler,
    ReadTimeout:  15 * time.Second,
    WriteTimeout: 60 * time.Second,
    IdleTimeout:  60 * time.Second,
    MaxHeaderBytes: 1 << 20, // 1 MB
}
if err := srv.ListenAndServe(); err != nil {
    slog.Error("server error", "error", err)
    os.Exit(1)
}
```

Propósito de cada timeout:
- **ReadTimeout (15s):** Limita el tiempo para leer el request completo, incluyendo headers y body. Previene slowloris.
- **WriteTimeout (60s):** Corta escrituras lentas. Los handlers largos (importaciones, AI) deberían responder antes o usar streaming; 60s es un límite superior razonable.
- **IdleTimeout (60s):** Cierra conexiones keep-alive que permanecen inactivas.
- **MaxHeaderBytes (1MB):** Previene memory exhaustion por headers gigantes.

### 3.2 Invalidar timeouts en endpoints de larga duración (opcional)

Si hay endpoints específicos que requieren más tiempo (ej: subida de EPUB grande), aumentar el `WriteTimeout` desde el handler:

```go
e.Request.Context()
// Nota: http.Server timeouts no son configurables por-request de forma nativa.
// Si un endpoint realmente necesita más tiempo, considerar chunked uploads
// o mover la operación a background (ver sección de handlers bloqueantes).
```

El diseño correcto es que los endpoints largos devuelvan `202 Accepted` inmediatamente y delegen el trabajo a jobs background. El `WriteTimeout` de 60s protege contra handlers que se cuelgan.

---

## Archivos modificados

| Archivo | Cambios |
|---------|---------|
| `internal/store/store.go` | Eliminar `BackfillChapterCharCounts()` y `BackfillNovelStats()` de `EnsureSchema` |
| `internal/store/store_chapters.go` | Renombrar backfills actuales a `BackfillChapterCharCountsFiltered` y `BackfillNovelStatsFiltered` (con `ctx`). Agregar `RunPendingBackfills(ctx)` con marker de archivo. Imports: `context`, `os`, `path/filepath`. |
| `internal/api/router.go` | Agregar `StartBackfills()` en `Server` y llamarla desde `New`. Import: `log/slog`. |
| `cmd/server/main.go` | PRAGMA `busy_timeout = 5000` después de `Bootstrap`. Reemplazar `http.ListenAndServe` por `http.Server` con timeouts. Import: `time`. |

---

## Validación

1. **Boot rápido (sin actividad de DB):**
   - Iniciar el servidor y medir tiempo hasta que acepta conexiones.
   - Verificar que no haya queries de backfill en el log al arrancar.

2. **Backfill bajo demanda:**
   - Iniciar el servidor. Llamar inmediatamente a un endpoint de listado de novelas/capítulos. La consulta debe responder aunque el backfill no haya corrido aún.
   - Verificar que tras unos segundos/minutos, el backfill se ejecuta en background (logs).
   - Reiniciar el servidor. Verificar que NO vuelve a ejecutar el backfill (marcador presente).

3. **Contención SQLite:**
   - Simular carga concurrente: desde dos clientes simultáneos, crear/modificar registros que generen escrituras (jobs, capítulos).
   - Verificar ausencia de errores `SQLITE_BUSY` en logs.
   - Medir tasa de fallos antes/después del PRAGMA.

4. **Timeouts HTTP:**
   - Desde un cliente, abrir una conexión y enviar headers extremadamente lentos (slowloris). Verificar que el servidor cierra la conexión tras ~15s.
   - Ejecutar `check-batch-updates` con 20 novelas. Medir duración total. El endpoint debería ser refactorizado eventualmente, pero con `WriteTimeout` el servidor no colapsa.
   - Probar subida de archivo grande. Verificar que el servidor no mantiene conexiones abiertas más allá de `WriteTimeout`.

---

## Notas

- Los backfills corren como best-effort en background. Si el servidor se detiene a mitad del backfill, al reiniciar se reanudará desde donde quedó (gracias al offset/paginación y al marcador global que solo se pone al finalizar completamente).
- El marcador global (archivo `.backfills-v1.done`) asume que ambos backfills terminan. Si falla uno, el archivo no se escribe y el próximo boot reintenta solo el que faltó.
- Para bases de datos con cientos de miles de capítulos, considerar paginación de 200-500 registros por lote y pequeños `time.Sleep` entre lotes para ceder el CPU a requests HTTP.
