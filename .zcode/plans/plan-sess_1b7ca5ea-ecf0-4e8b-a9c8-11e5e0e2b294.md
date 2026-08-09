## Feature: Re-descargar capítulos (reemplaza el original, conserva traducciones)

**Idea clave**: el nuevo endpoint re-escrapea la lista de capítulos desde `novel.URL`, la empareja con los capítulos existentes (por orden, con fallback por título), descarga el contenido original fresco y actualiza SOLO el campo `original_content` de cada capítulo usando su ID. El `upsertChapter` existente ya preserva `translated_content`, `refined_content`, `status` y títulos cuando esos campos van vacíos (store_chapters.go:202-277). Los capítulos nuevos en el sitio NO se incluyen (eso lo hace "Actualizar" / update-from-url).

### Backend

1. **`internal/store/settings.go`** — añadir campo a `DownloadChapterInfo` (línea 268):
   ```go
   ChapterID string `json:"chapterId,omitempty"` // id del capítulo existente (modo re-download)
   ```

2. **`internal/api/runtime_worker.go`**:
   - Añadir `ReDownload bool json:"reDownload"` a `downloadJobOptions` (línea 234).
   - En `processDownloadJob`, cuando `opts.ReDownload` es true: guardar con
     `UpsertChapterWithoutStats(ownerID, novelID, &store.Chapter{ID: chInfo.ChapterID, ChapterOrder: chOrder, OriginalContent: ch.Markdown})` — sin `Title` ni `Status`, así el merge del upsert preserva traducciones, refinamientos, estado y título. Si `ChapterID` está vacío en modo re-download, contar como fallido (defensivo).

3. **`internal/api/router_import.go`** — nuevo endpoint `POST /db/novels/{id}/redownload-from-url` (junto a update-from-url, línea 508):
   - Body: `{ startChapter?, endChapter? }` (rango opcional, mismo formato que update-from-url).
   - `GetOwnedNovel` → error si no hay `novel.URL`.
   - `ListChaptersAccessible` → índice por `ChapterOrder` (fallback por `Title`).
   - Scrape fresco: `dl.GetNovelInfo(novel.URL)` (sin previewCache, como el fallback de update-from-url).
   - Por cada capítulo del sitio: `chNum := chapterOrderOf(ch)` (fallback `i+1`); buscar existente por orden (fallback título); si no existe → skip; aplicar filtro start/end; armar `DownloadChapterInfo{URL, Title, Order: chNum, ChapterID}`.
   - Si 0 coincidencias → `200 {pendingChapters: 0, message}` sin job.
   - Si no → job `Operation: "download"` con `OptionsJSON` incluyendo `reDownload: true`, `enqueueJob`, responder `{pendingChapters, downloadJobId, message}` (misma forma que update-from-url).
   - El progreso se ve igual en el UI: operation "download" ya está soportada en `jobRecord` y en el JobsDrawer.

4. **Tests — `internal/api/redownload_test.go`** (nuevo, patrón de `import_url_test.go`):
   - Mock httptest de novelfire + `hostRewritingTransport` + `DownloaderFactory` con `MinChapterDelay=0/MaxChapterDelay=0` (campos exportados, sleep se salta).
   - Importar novela → upsertar capítulos con `Status: "translated"` + `TranslatedContent` → cambiar el contenido del mock → POST redownload → esperar job done (poll de jobs activos) → assert: `original_content` actualizado, `translated_content`/`status` preservados, char counts recalculados.
   - Casos extra: filtro de rango, novela sin URL → 400.

### Frontend

5. **`frontend/src/api/client.ts`** — método nuevo bajo `novels` (junto a updateFromUrl, línea 238):
   ```ts
   async redownloadFromUrl(novelId: string, input: { startChapter?: number; endChapter?: number }): Promise<UpdateUrlResult> {
     return http.post<UpdateUrlResult>(`/api/db/novels/${novelId}/redownload-from-url`, input);
   },
   ```
   (reutiliza el tipo `UpdateUrlResult` existente: `pendingChapters`, `downloadJobId`, `message`).

6. **`frontend/src/features/projects/ProjectSettingsDialog.vue`** — en la pestaña "Avanzado", nuevo `n-collapse-item` "Redescargar capítulos" (encima de "Zona de peligro"):
   - Descripción: "Vuelve a descargar el contenido original desde la fuente y reemplaza el original de los capítulos. Las traducciones y refinamientos se conservan."
   - Dos `n-input-number` opcionales (Capítulo inicial / final) + `n-popconfirm` de advertencia + botón con `:loading="reDownloading"`, deshabilitado si `!props.novel.url`.
   - Handler: `api.novels.redownloadFromUrl(...)` → si `pendingChapters > 0`: `message.success` con el conteo + `emitJobChanged()` (importar de `@/utils/job-events`); si no, `message.info` con el message del backend; errores con `message.error`. El progreso aparece automáticamente en el JobsDrawer.
   - `NInputNumber` y `NPopconfirm` ya están importados en el archivo.

### Verificación
- `go build ./cmd/server` y `go vet ./...`, luego `go test -short ./...` (incluye el test nuevo).
- `cd frontend && npm run build` (typecheck).
- Al final: `make build` para re-embeder el frontend.

### Decisiones tomadas (puedes corregirlas)
- Se conserva el `status` del capítulo (translated/refined/done) aunque el original cambie — si quieres re-traducir un capítulo afectado, lo haces manualmente; nada se pierde.
- No se sobreescriben títulos ni `translated_title` desde el sitio.
- Los capítulos que fallen la descarga conservan su contenido anterior; el job reporta `failedChapters` y termina como "failed".