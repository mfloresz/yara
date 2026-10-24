# Plan: layout estilo Scancore + soporte de comics en Yara

> Estado: propuesta (pendiente de aprobación). Rama: `feat/scancore-layout-and-comics`.
> Fecha: 2026-10-24.

## 1. Objetivo

Incorporar a Yara dos capacidades tomadas de dos proyectos de referencia:

1. **El tipo de layout/UI de Scancore** — navegación por secciones (sidebar/rail en desktop, tabs/drawer en móvil), home de "continuar leyendo", y sobre todo la experiencia de **browse por source** (popular/latest/search con scroll infinito y filtros).
2. **El soporte de comics de Tira** — descarga de capítulos por parser (páginas de imágenes), biblioteca de comics, reader web y progreso de lectura.

Yara conserva íntegro su dominio actual de novelas + traducción AI. Los comics **conviven** con las novelas, no las reemplazan.

## 2. Qué es cada proyecto (hallazgos del análisis)

### Yara (este repo)
Go + PocketBase embebido + Vue 3/Vite/**Naive UI** (nota: AGENTS.md dice PrimeVue pero el código usa Naive UI — `frontend/package.json`, `frontend/src/theme/naive-theme.ts`), binario único, `/api/v1/*` con envelope, parsers goja por sitio (`parsers/*.js`, 26 scripts) con manifiesto firmado y auto-update, worker de jobs in-process, reader de novelas inmersivo (`ReaderPage.vue`) con progreso offline-first. Ya tiene: `reading_progress` (por usuario+novela), jobs drawer, PWA offline.

### Scancore (`/home/misael/Dev/z.or.src/scancore`)
**C++20 / Qt 6.9 / QML** — app desktop+móvil estilo Mihon/Tachiyomi (comics, novelas, anime; ejecuta extensiones APK de Mihon/Aniyomi y plugins LNReader). No es un fork de Yara: no hay API HTTP ni Vue; **se portan patrones de UX, no archivos**. Lo relevante:

- **Shell** (`src/qml/Main.qml`): navegación configurable `sidebar | rail | dock | floating` en desktop, bottom-tabs en móvil; 9 secciones (home, comics, novels, anime, library, history, activity, downloads, settings); cada sección mantiene su propio page stack; reader abre en modo inmersivo.
- **Browse por source** (`src/qml/pages/SourcePage.qml`): una página por fuente con **tabs `popular | latest | search`**, filtros descritos por la propia fuente (JSON → `FilterSheet.qml`), **scroll infinito** con generation-guard contra respuestas obsoletas, dedupe por id y corte cuando una página no aporta ítems nuevos.
- **Hubs por medio** (`ComicsPage.qml`): chips de fuentes + búsqueda global cross-source.
- **Reader comics** (`ComicReader.qml`): modos strip (webtoon vertical) / paged, LTR/RTL, split de dobles páginas, tap-zones configurables, filtros de brillo/grises/invert, preferencias de reader por serie, resume + prefetch del capítulo siguiente.
- **Modelo de datos** (SQLite, `src/core/Database.cpp`): `series` / `units` (capítulos) / `library` (categorías, nuevos capítulos) / `progress` (posición+porcentaje) / `downloads` — traduce casi 1:1 a colecciones PocketBase.

### Tira (`/home/misael/Dev/tira`)
**Hermano directo de Yara**: mismo esqueleto `translator-server` (Go + PocketBase + Vue/Naive UI + goja + goai + browser-workers + binario único), pero **el dominio de novelas fue reemplazado por comics** (no hay `NovelsCollection` ni epubs; `ChaptersCollection` apunta a comics). Es un port **entre sibling repos**: mover/adaptivear código Go y Vue con fricción baja pero merge manual en los archivos compartidos. Trae:

- **Colecciones comics** (`internal/store/store_schema.go`): `comics` (metadatos bilingües, glossary, prompts por comic, cover/thumbnail, stats agregadas), `chapters` (doble orden `chapter_order` + `position`, `excluded` para borrado lógico, `source_key` para diff de TOC), `chapter_pages` (imagen en FileField protegido ≤25MB + artefactos de traducción visual), `comic_images` (imágenes sueltas de import ZIP), `reading_progress` (usuario+comic: `chapter_id` + `scroll_percent`).
- **Parser host de comics** (`internal/comicparserhost/`): mismo contrato goja que Yara pero `chapter(ctx,url) → {title, pages[]}`; 10 parsers en `parsers/` (manhwaweb, zonatmo, manhuahot, …) con el mismo sistema de manifiesto firmado + auto-update + browser-worker.
- **Downloader** (`internal/comicdownloader/`): rotación de Referer (origin del hint → origin de la imagen, o directiva `#tira-referer=`), validación magic-bytes, reintento x4 con backoff, **descifrado AES-CTR** vía directiva `#tira-enc=` en fragmento de URL (CDNs de imágenes cifradas), throttle 5-10s por host.
- **API**: `/api/v1/comics/*`, imagen autenticada de página (`GET .../pages/{pageId}/image`, `Cache-Control: immutable`), import ZIP de imágenes y import URL con preview, jobs, glosario, progreso.
- **Frontend**: `ComicDetailPage` (sidebar + tabs), `ReaderPage` (scroll vertical, ancho 520-1100px persistido, `translatedUrl || url` por página, progreso offline-first IndexedDB con "local-no-sincronizado gana"), `MangaEditorPage` (editor visual OCR/typeset), PWA.
- **Traducción visual** (OCR + LLM + re-render): `internal/mangatranslate/` + `runtime_manga.go` — el pipeline más pesado de tira.

## 3. Decisiones de diseño (D1–D6)

| # | Decisión | Justificación / alternativa descartada |
|---|---|---|
| **D1** | Comics como dominio paralelo con colecciones propias: `comics`, `comic_chapters`, `chapter_pages`, `comic_images`, `user_manga_settings`. Colecciones de novelas intactas. | Tira reutilizó el nombre `chapters` porque borró las novelas; aquí reutilizarlo obligaría a renombrar/migrar datos existentes (breaking). Con nombres propios el cambio es **aditivo** vía `EnsureSchema` — sin flag de migración. |
| **D2** | Jobs y progreso **compartidos** con discriminante: `translation_jobs` gana relación nullable `comic` (validación: exactamente uno de `novel`/`comic`); `reading_progress` gana `comic` nullable junto a `novel`. | Un solo worker, una sola cola, un solo Operations/JobsDrawer. Alternativa: colección `comic_jobs` + cola propia — más aislada pero duplica UI, cola y página de operaciones. |
| **D3** | **Parserhost unificado**: un solo paquete motor; el script declara `media: 'novel'` (default) \| `'comic'`; `chapter()` devuelve `content` (novel) o `pages` (comic) y el host valida según `media`. El manifiesto `parsers/index.json` gana campo `media` por script. | Engine/bindings/fetcher/update de Yara son agnósticos al medio; el puerto literal de `comicparserhost` como paquete paralelo duplicaría motor+update+SSRF guards. El contrato de tira ya demostró que la única diferencia real es el return de `chapter`. |
| **D4** | Se replica la **estructura** de Scancore, no su estética: shell de secciones + browse por fuente, implementado sobre el design system actual ("The Quiet Shelf", Naive UI + tokens de `frontend/src/app/styles.css`). Sin segunda paleta, sin gradientes. | Scancore tiene su propio theming (.sct, acentos) que chocaría con DESIGN.md. |
| **D5** | Browse por fuente = **contrato de parser v2 con ops opcionales**: `popular(page)`, `latest(page)`, `search(query, page, filters?)`, `filters()` opcionales; bump de `apiVersion`; los 26 parsers existentes siguen funcionando sin cambios. Las portadas del browse se sirven por un endpoint proxy autenticado que aplica el Referer del parser. | Scancore lo resuelve con la interfaz `browse(listing, page, query, filters)` de sus sources; en Yara el análogo natural es extender el contrato goja. Hacerlo opcional evita reescribir el catálogo actual. |
| **D6** | La **traducción visual** de comics (OCR+LLM+typeset de tira) queda **fuera del alcance inicial** (fase opcional final). | Es el subsistema más pesado (procesador externo + editor visual) y es independiente de "descargar, administrar y leer comics". |

Supuestos que se dan por sentados (corregir si no): Yara sigue siendo un solo binario (sin nuevos `cmd/`); no se portan de tira el chat/agente ni password-resets (otros PRs si se quieren); el catálogo de comics se publica en el manifiesto de Yara firmado con la key de Yara.

## 4. Fases

Tamaños relativos: S (≤1 sesión), M (1-3 sesiones), L (multi-sesión, partir en PRs).

### F0 — Preparación (S) — **parcialmente hecho**
- [x] Rama `feat/scancore-layout-and-comics`.
- [ ] Baseline verde: `go test ./...` y `cd frontend && npm run build`.
- [ ] Corregir AGENTS.md (dice PrimeVue; el código usa Naive UI) para no inducir a error en fases siguientes.
- [ ] Cerrar D1–D6 (objeciones → ajustar este doc).

### F1 — Shell estilo Scancore (M, solo frontend)
Replica del shell de Scancore (D4) sobre las páginas existentes:

- `frontend/src/app/App.vue` + nuevo `components/AppShell.vue`: **sidebar colapsable (rail)** en desktop, **bottom tabs + drawer** en móvil. Secciones: **Home**, **Biblioteca**, **Fuentes** (placeholder hasta F4), **Operaciones**, **Ajustes** (+ Admin para admins; Chat/Asistente si se decide portarlo). Header actual se reduce a contextual.
- Pasar de "cada página instancia su `AppLayout`" a shell persistente con router anidado; `ReaderPage` y `ChapterPage` quedan **fuera** del shell (inmersivas, como hoy).
- **Home** nuevo: "continuar leyendo" (ya existe `reading_progress` + `useReadingProgress`) + jobs activos (reusa `useActiveJobs`).
- Movil: targets 44px, drawer por defecto (DESIGN.md).
- Verificación: `npm run build` verde; todas las rutas actuales navegables; menú móvil funcional; sin cambios de API.

### F2 — Backend de comics (L, port desde tira — partir en PRs)
1. **Store** (PR 1): colecciones D1 en `store_schema.go` + `store_comics.go` / `store_comic_chapters.go` / `store_chapter_pages.go`; doble orden `chapter_order`+`position` y `source_key`; extender jobs/progress (D2); stats por operación (patrón `RecalculateNovelStats`, sin backfill en boot).
2. **Parserhost unificado** (PR 2): contrato `media` (D3) en `internal/parserhost/contract.go` + validación en `engine.go`; `docs/parsers.md` actualizado.
3. **Downloader** (PR 3): port de `internal/comicdownloader` (referer rotation, magic-bytes, AES-CTR, caps) integrado al throttle por host existente (`parser_fetcher.go`).
4. **API + worker** (PR 4): `router_comics.go` (CRUD + jobs + import ZIP/URL), `router_pages.go`/`router_images.go` (imágenes autenticadas, `Cache-Control: immutable`), `runtime_comic.go` (job: toc → download pages), ops nuevas en `enqueueJob` (`runtime_worker.go`), shapers en `router_responses.go`, OpenAPI + README de API.
5. **Parsers comics** (PR 5): portar los 10 scripts de tira con `media: 'comic'`; `make parsers-manifest` (re-firmado con key de Yara); sync `SUPPORTED_SITE_HOSTS` en las 4 copias de extensiones.
- Tests: integración por ruta nueva (patrón `router_integration_test.go`: envelope, 201/202/204, Location) + unit tests del downloader.
- Verificación: en dev, importar un comic por URL → job descarga capítulos → imágenes servidas autenticadas → `go test ./...` verde.

### F3 — Frontend de comics (M/L)
- **Biblioteca unificada**: filtro por tipo (novela/comic) en `DashboardPage`; `ComicCard` siguiendo `NovelCard` (portada 2:3, Cover Rest/Lift).
- `pages/ComicDetailPage.vue`: espejo de `NovelDetailPage` (sidebar + tabs: Capítulos, Jobs, Importar, Exportar, Opciones). Sin tab de traducción visual (D6).
- `pages/ComicReaderPage.vue`: port de los patrones del `ReaderPage` de tira — scroll vertical continuo, ancho 520-1100px persistido, `translatedUrl || url`, progreso dual offline-first ("local no sincronizado gana"), prev/next + lista buscable de capítulos. Reusa el scaffolding (barra inferior, modal de capítulos) del `ReaderPage` de novelas.
- `useOfflineCache`: stores de comics/chapterPages.
- Verificación: flujo completo import→leer→reanudar en dev; build verde.

### F4 — Browse de fuentes (M) — la pieza "multi-source" de Scancore
- Contrato v2 (D5): ops opcionales `popular/latest/search/filters` en `contract.go` + `docs/parsers.md`; bump `apiVersion`.
- Backend: `router_sources.go` — `GET /api/v1/sources` (catálogo instalado, agrupado por media), `GET /api/v1/sources/{name}/browse?listing=popular|latest|search&q=&page=`, proxy autenticado de portadas con Referer del parser. Reusa límites por-invocación del engine.
- Frontend `pages/SourcesPage.vue`: chips de fuentes por media, tabs `popular | latest | search`, filtros en sheet (cuando el parser los expone), **scroll infinito con generation-guard + dedupe** (patrones de `SourcePage.qml` de Scancore), resultado → import con el `ImportUrlDialog` existente.
- Implementar las ops nuevas primero en 2-3 parsers insignia (p.ej. `novelfire` + un comic) como referencia contractual; `make parsers-manifest`.
- Verificación: browse contra sitio real (debug proxy si hay Cloudflare); añadir a biblioteca desde el browse; los 26 parsers existentes pasan `-check-parser` sin cambios.

### F5 — Opcionales post-MVP (no bloquean el objetivo)
Reader paginado LTR/RTL + tap-zones + filtros brillo/grises + split de dobles páginas y preload N-ahead (patrones de `ComicReader.qml`); historial/actividad; categorías y favoritos; **traducción visual** completa de tira (`mangatranslate` + `MangaEditorPage`); búsqueda global cross-source.

## 5. Riesgos y mitigaciones

- **Divergencia Yara↔Tira**: tira añadió cosas que Yara no tiene (password-resets, agente/chat) y a Yara le faltan otras (epubs no existen en tira, y tira no tiene el hardening v0.50.x de Yara). **Mitigación**: port selectivo archivo por archivo con merge manual en los compartidos (`store.go`, `router_v1.go`, `runtime_worker.go`, `store_schema.go`) — nunca copiar directorios completos.
- **Colisiones de semántica** (`chapters`, `translation_jobs`, `reading_progress`): resueltas por D1/D2; los nombres de colección nuevos se registran en `store.go` con tests.
- **Manifiesto firmado**: los parsers de comics deben firmarse con la key de Yara; con auto-update activo, instalaciones existentes los recibirán en cuanto el manifiesto se publique. Regenerar siempre con `make parsers-manifest` antes de commit (regla ya existente).
- **Tamaño de F2**: mitigado partiendo en 5 PRs por capa; cada PR compila y pasa tests solo.
- **Scope creep del reader**: Scancore tiene decenas de features; el MVP es scroll vertical (F3) y el resto se enumera en F5 para no perderlo, no para implementarlo ya.
- **F1 sin backend**: si el shell nuevo complica, fallback = mantener header actual y añadir solo Home + Fuentes al drawer (menos fiel a Scancore, cero riesgo).

## 6. Pendientes de decisión del usuario

1. ¿Traducción visual de comics (OCR+typeset) al alcance? (Recomendación: no — F5+.)
2. ¿Portar también de tira el asistente/chat con tools y password-reset? (Recomendación: no en esta rama.)
3. ¿Jobs compartidos (D2) y parserhost unificado (D3), o prefieres el port literal con paquetes/colas separadas?
4. ¿Alguna sección de Scancore que quieras sí o sí en F1 (p. ej. descargas como sección propia, historial)?
