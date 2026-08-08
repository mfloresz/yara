## Diagnóstico

**Problema 1 — Solo se listan capítulos pendientes:** La pestaña "Limpieza" computa `cleanEligibleChapters` sobre `allSummaries`, que se carga con `api.chapters.listEligible()` (`GET /chapters/eligible`). Ese endpoint solo devuelve capítulos pending/failed para traducir o translated/failed para refinar. Por eso los capítulos ya traducidos (estado `translated`/`completed`) no aparecen.

**Problema 2 — Vista previa:** Solo existe un botón "Previsualizar" por capítulo que muestra una tarjeta inline debajo de la lista (todo el capítulo antes/después), no un modal con el listado de capítulos afectados.

## Cambios

### 1. Backend — `internal/api/`

**`cleaner.go`**: añadir el tipo `CleanDiffHunk` (`before []string`, `after []string`) y la función `diffLines(original, cleaned string) []CleanDiffHunk` — diff de líneas propio (sin dependencias nuevas): normaliza saltos de línea, dos punteros con ventana de resincronización, produce hunks de líneas eliminadas/añadidas (correcto para los 5 modos: remove_after, remove_duplicates, remove_line, remove_multiple_blanks, search_replace). Añadir `Changes []CleanDiffHunk` a `CleanPreviewResult`.

**`router_chapters.go`**:
- Nuevo endpoint `POST /db/novels/{novelId}/chapters/clean-preview-bulk`: acepta `chapterIds[]` + opciones de limpieza (mismas del endpoint `clean`). Para cada capítulo accesible aplica `cleaningSource` + `ApplyClean` y **solo incluye los que cambiarían** (`changed == true`), con `chapterId`, `chapterOrder`, `chapterTitle`, `changes` (líneas afectadas) y el `CleanResult`. Respuesta: `{ items, total, changed }`. Mismas validaciones (`isValidCleanMode`, `isValidApplyTo`, ownership).
- El endpoint `clean-preview` existente pasa a incluir también `changes`.

**Tests**: unitarios de `diffLines` en `cleaner_test.go` y test de integración del endpoint bulk en `router_integration_test.go` (crea novela + capítulos con contenido, llama a clean-preview-bulk, verifica que solo devuelve los que cambiarían y los campos).

### 2. Frontend — `frontend/src/`

**`api/types.ts`**: añadir `CleanDiffHunk`, `CleanPreviewItem`, `CleanPreviewBulkResponse`; extender `CleanPreviewResponse` con `changes`.

**`api/client.ts`**: añadir `cleanPreviewBulk()`.

**`pages/NovelDetailPage.vue`**:
- **Fix listado**: nuevo ref `cleanAllSummaries` cargado con `api.chapters.list()` (todos los capítulos, no elegibles). `cleanEligibleChapters` filtra `cleanAllSummaries` por presencia de contenido (`hasOriginalContent` / `hasTranslatedContent` / `hasRefinedContent`). Carga al activar la pestaña `clean` (en `watch(activeTab)` y `refreshChapterViews`), reseteo en `watch(novelId)` y refresco tras aplicar limpieza. `tabNeedsAllSummaries` deja de incluir "clean".
- **Modal de vista previa (lista apilada, solo líneas afectadas)**:
  - Botón "Previsualizar" junto a "Aplicar" → llama a `clean-preview-bulk` con los capítulos seleccionados y abre un `n-modal`.
  - El modal muestra: alerta resumen "Se modificarán X de Y capítulos seleccionados", y una lista apilada de los capítulos afectados; cada uno con cabecera (`#orden · título` + badge `−N líneas`) y **solo las líneas que cambian** (líneas `before` con prefijo `−` en tono `--danger`, líneas `after` con prefijo `+` en tono `--success`), truncadas con "+N más" si son muchas.
  - Pie: "Cerrar" + "Aplicar a N capítulos" (aplica la limpieza a los capítulos afectados mostrados, reutilizando el flujo de `applyCleaningToSelected`).
  - El botón "Previsualizar" por capítulo existente abre el mismo modal para ese capítulo (bulk con un solo id).
  - Se elimina la tarjeta inline de vista previa actual (`cleanPreview` + `<n-card v-if="cleanPreview">`).

## Verificación

- `go test ./internal/api/...` (o `go test ./...` para todo el backend).
- `go vet ./...`.
- `npm run build` en `frontend/` (typecheck vue-tsc). Como `frontend_embed.go` incrusta `frontend/dist`, después hay que correr `make build` (o `npm run build`) para que el binario sirva la SPA actualizada.

No tocaré `internal/ai/openai.go` ni `internal/ai/registry_test.go` (cambios ya presentes en el working tree, ajenos a esta tarea).