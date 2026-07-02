# Plan completo de refactor del backend Go

## Objetivo

Reducir complejidad estructural del backend Go mediante una refactorización **incremental, segura y verificable**, enfocada en:

- bajar blast radius por archivo
- separar responsabilidades por dominio
- mejorar legibilidad y navegación
- preparar mejor testing
- mantener compatibilidad con API actual
- no tocar `frontend` salvo validación contractual indirecta

---

## Alcance y restricciones

Este plan asume como restricción principal que el objetivo es **reestructurar internamente el backend sin romper contratos HTTP ni comportamiento observable del frontend**.

### Invariantes que no deben romperse

#### Contrato externo
- mismas rutas HTTP
- mismos métodos HTTP
- mismos códigos de estado
- mismo shape de JSON
- misma semántica de jobs, chapters, novels y epubs
- misma política de permisos visible al cliente

#### Persistencia
- no cambiar nombres de colecciones PocketBase en esta fase
- no cambiar nombres de campos persistidos salvo migración explícita
- no cambiar reglas de acceso sin una razón funcional clara

#### Ejecución
- mantener worker secuencial mientras no haya rediseño explícito de concurrencia
- no activar `Concurrency` todavía si hoy no está soportada realmente
- no cambiar lógica de segmentación salvo fixes puntuales o encapsulación

---

## Diagnóstico estructural resumido

### Archivos críticos a refactorizar primero

#### 1. `internal/store/store.go`
Problemas:
- archivo dios
- mezcla schema, migrations, auth, settings, providers, novels, chapters, jobs, epubs, mappers y helpers

Impacto:
- máximo acoplamiento interno
- alto riesgo al cambiar cualquier parte

#### 2. `internal/api/router.go`
Problemas:
- routing + handlers + input binding + response shaping + import flows + helpers HTTP

Impacto:
- punto central demasiado ancho
- alta probabilidad de romper contratos por accidente

#### 3. `internal/api/runtime.go`
Problemas:
- worker + orchestration + config resolution + prompt building + segmentation + provider creation

Impacto:
- lógica operacional difícil de razonar
- mezcla timing, estado, prompting y algoritmos

### Archivos grandes, pero más cohesivos

#### `internal/epubimport/parser.go`
- grande, pero su tamaño viene más de la complejidad algorítmica que de mezclar bounded contexts

#### `internal/noveldownloader/novelfire.go`
- todavía tolerable por estar enfocado en un parser site-specific

#### `internal/store/settings.go`
- largo, pero no es el cuello de botella principal

---

## Estrategia general

No partir archivos solo por tamaño, sino por **fronteras de responsabilidad**.

### Orden recomendado
1. higiene mecánica y baseline de seguridad
2. extraer `store.go` por dominios
3. extraer `runtime.go` por workflow
4. extraer `router.go` por recurso API
5. extraer módulos algorítmicos/cohesivos (`epubimport`, `noveldownloader`)
6. endurecer tests y observabilidad
7. decidir si vale la pena una siguiente fase de rediseño

---

# Fase 0 — Baseline y guardrails

## Objetivo
Crear una base estable antes de mover piezas.

## Tareas

### 0.1 Arreglar chequeos mecánicos
- corregir `go vet` en `internal/config/config.go`
  - remover self-assignment de `port`
- correr `gofmt` en todo el backend Go

### 0.2 Congelar comportamiento actual con tests
Antes de refactor grande, asegurar una red mínima de regresión.

Prioridad de tests:
- `internal/api/router_integration_test.go`
- `internal/api/import_url_test.go`
- `internal/api/runtime_config_test.go`
- `internal/api/segmentation_test.go`
- `internal/api/refine_test.go`
- `internal/epubimport/parser_test.go`
- `internal/noveldownloader/*_test.go`

### 0.3 Crear checklist de no-regresión
Para cada refactor:
- `go test ./...`
- `go vet ./...`
- smoke de rutas críticas:
  - auth
  - settings
  - CRUD novels
  - CRUD chapters
  - translation jobs
  - epub upload/download
  - import from URL

## Entregable
- repo formateado
- `vet` limpio
- tests actuales corriendo como baseline

---

# Fase 1 — Refactor de `internal/store/store.go`

## Objetivo
Separar el store por contextos de dominio sin cambiar el tipo `Store` ni su API pública.

## Regla clave
**No cambiar firmas públicas en esta fase**, salvo helpers internos nuevos.

## 1.1 Estructura objetivo

Propuesta de archivos:

- `internal/store/store.go`
- `internal/store/store_schema.go`
- `internal/store/store_auth.go`
- `internal/store/store_settings.go`
- `internal/store/store_providers.go`
- `internal/store/store_novels.go`
- `internal/store/store_chapters.go`
- `internal/store/store_jobs.go`
- `internal/store/store_epubs.go`
- `internal/store/store_mapping.go`
- `internal/store/store_helpers.go`

## 1.2 Responsabilidad por archivo

### `store.go`
Debe quedar mínimo:
- tipo `Store`
- constructor `New`
- errores exportados (`ErrNotFound`, `ErrForbidden`)
- constantes de colecciones, si todavía no se extraen

### `store_schema.go`
Mover:
- `EnsureSchema`
- `ensureUsersCollection`
- `ensureProvidersCollection`
- `ensureUserProviderSettingsCollection`
- `ensureUserPromptSettingsCollection`
- `ensureUserTranslationSettingsCollection`
- `ensureNovelsCollection`
- `ensureChaptersCollection`
- `ensureJobsCollection`
- `ensureEpubsCollection`
- migraciones auxiliares
- `addSystemDateFields`
- `migrateSystemDateFields`
- `enableNovelCascadeDelete`
- `ensureField`

### `store_auth.go`
Mover:
- `CreateUser`
- `AuthenticateUser`
- `RefreshAuth`
- `FindAuthRecord`
- `userFromRecord`

### `store_settings.go`
Mover:
- `GetAppSettings`
- `GetTheme`
- `SaveTheme`
- `SaveAppSettings`
- `GetTranslationDefaults`
- `getUserTranslationSettings`
- `saveUserTranslationSettings`
- `normalizeTranslation`
- `normalizeTheme`

### `store_providers.go`
Mover:
- `seedProviders`
- `providerKind`
- `GetActiveProviderSettings`
- `ListProviderSettings`
- `UpsertProviderSettings`
- `ReplaceProviderAPIKey`
- `DeleteProviderAPIKey`
- `ResolveProviderAISettings`
- `getProviderByKey`
- `findUserProviderSettingsRecord`

### `store_novels.go`
Mover:
- `CreateNovel`
- `ListNovels`
- `GetNovelAccessible`
- `GetOwnedNovel`
- `UpdateNovel`
- `DeleteNovel`
- `SetNovelVisibility`
- `CopyNovel`
- `ImportEpubNovel`
- `ImportUrlNovel`
- `attachNovelCover`
- `AttachCoverBlob`
- `coverExtension`
- `applyNovelToRecord`

### `store_chapters.go`
Mover:
- `GetMaxChapterOrder`
- `GetExistingChapterURLs`
- `ListChaptersAccessible`
- `GetChapterAccessible`
- `UpsertChapter`
- `DeleteChapter`
- `BulkDeleteChapters`
- `UpdateChapterStatus`
- `UpdateChapterStatusForUser`
- `SaveChapterTranslation`

### `store_jobs.go`
Mover:
- `ReconcileProcessingChaptersForJob`
- `CreateJob`
- `GetJob`
- `GetOwnedJob`
- `ListRunnableJobs`
- `ListJobs`
- `ListActiveJobs`
- `UpdateJob`
- `UpdateJobForUser`
- `LoadJobChapters`

### `store_epubs.go`
Mover:
- `UpsertEpub`
- `ListEpubs`
- `GetEpubDownloadFile`

### `store_mapping.go`
Mover:
- `novelFromRecord`
- `chapterFromRecord`
- `jobFromRecord`
- `epubFromRecord`

### `store_helpers.go`
Mover:
- `buildPBFileURL`
- `jsonString`
- `defaultString`
- `clampText`
- `firstString`
- `asInt`
- `camelToSnake`

## 1.3 Orden interno de ejecución

### Paso 1
Crear archivos nuevos sin borrar nada todavía.

### Paso 2
Mover primero helpers puros:
- `defaultString`
- `jsonString`
- `asInt`
- `firstString`
- `clampText`

Verificar:
- compila
- tests pasan

### Paso 3
Mover mappers record ↔ domain.

Verificar:
- CRUD de novels, chapters, jobs, epubs sigue estable

### Paso 4
Mover verticalmente por contexto:
1. auth
2. settings
3. providers
4. novels
5. chapters
6. jobs
7. epubs
8. schema

Cada paso con validación.

## 1.4 Riesgos y mitigaciones

### Riesgo
romper métodos usados por `router.go` y `runtime.go`

### Mitigación
- no cambiar nombres públicos
- no cambiar firmas
- solo mover ubicación física

### Riesgo
ciclos entre archivos

### Mitigación
- mantener todo en package `store`
- mover helpers comunes temprano

### Riesgo
deriva accidental en serialización

### Mitigación
- no tocar structs públicos todavía
- no cambiar tags JSON

## 1.5 Resultado esperado
- `store.go` baja drásticamente
- acceso más claro por dominio
- menor blast radius al tocar jobs o novels

---

# Fase 2 — Refactor de `internal/api/runtime.go`

## Objetivo
Separar el runtime por capas de responsabilidad: worker, config, translate, refine, prompts y segmentación.

## Estructura objetivo

- `internal/api/runtime_worker.go`
- `internal/api/runtime_config.go`
- `internal/api/runtime_translate.go`
- `internal/api/runtime_refine.go`
- `internal/api/runtime_prompts.go`
- `internal/api/segmentation.go`
- `internal/api/runtime_types.go`

## 2.1 Distribución propuesta

### `runtime_types.go`
Mover:
- `promptTemplate`
- `promptSettings`
- `glossaryEntry`
- `novelAIOptions`
- `novelTranslationOptions`
- `resolvedJobConfig`
- `chapterSegment`
- `chapterSegmentationStatus`
- `refineChunk`

### `runtime_worker.go`
Mover:
- `startJobWorker`
- `enqueueJob`
- `jobWorkerLoop`
- `processJob`
- `loadJobContext`

Responsabilidad:
- scheduling
- state transitions del job
- coordinación de alto nivel

### `runtime_config.go`
Mover:
- `resolveJobConfig`
- `applyGlobalPromptFallbacks`
- `newAIProvider`
- `effectiveModel`

Responsabilidad:
- resolución de configuración efectiva
- defaults
- overrides por app/novela/job
- construcción del provider

### `runtime_translate.go`
Mover:
- `previewChapterSegmentation`
- `runTranslateChapter`
- `runTranslateChapterDetailed`
- `translateSegment`
- `joinSegments`

Responsabilidad:
- flujo de traducción de capítulos
- retries
- actualización de progreso por segmento

### `runtime_refine.go`
Mover:
- `runRefineChapter`
- `buildRefineChunks`
- `splitLines`
- `applyRefineEdits`
- `trimEditBoundaryNewlines`
- `buildCheckPrompt`
- `buildRefinePrompt`

Responsabilidad:
- refine/check workflow
- chunking de refinamiento

### `runtime_prompts.go`
Mover:
- `fillPrompt`
- `formatGlossary`

### `segmentation.go`
Mover:
- `buildSegments`
- `findCutIndex`
- `byteIndexAtRuneOffset`
- `runeOffsets`
- `runeIndexAtOrBefore`
- `runeIndexAtOrAfter`
- `hasRunePrefix`
- `findWordBoundary`
- `absInt`
- `minFloat`
- `minInt`
- `max`
- `errorString`

## 2.2 Mejoras permitidas dentro del refactor

### Mejora 1 — sleep cancelable en retries
Hoy `translateSegment` usa `time.Sleep(...)`.

Cambiar por helper cancelable, por ejemplo:
- `sleepWithContext(ctx, d time.Duration) error`

Beneficio:
- jobs cancelados reaccionan mejor
- timing más correcto

### Mejora 2 — normalizar semántica de retries
Hoy `MaxRetries` no significa exactamente lo mismo en translate y refine.

Definir una sola semántica:
- `MaxRetries = reintentos adicionales después del primer intento`

Sin cambiar comportamiento externo abruptamente:
- introducir helper interno
- actualizar tests del runtime

### Mejora 3 — marcar `Concurrency` como no implementada o ignorada explícitamente
Hoy existe en settings, pero no gobierna la ejecución.

Opciones seguras en esta fase:
1. mantenerla persistida pero documentada como no usada
2. agregar comentario interno y test que garantice ejecución secuencial

No recomiendo activarla todavía.

## 2.3 Orden de ejecución

1. mover utilidades puras a `segmentation.go`
2. mover prompting/config helpers
3. mover refine workflow
4. mover translate workflow
5. dejar `processJob` al final

## 2.4 Riesgos

### Riesgo
romper actualizaciones de progreso del job

### Mitigación
- tests sobre `autoSegment*`
- smoke sobre jobs traduciendo capítulos segmentados

### Riesgo
alterar contenido final por cambios en segmentación

### Mitigación
- no cambiar algoritmo en fase de extracción
- snapshot tests o equivalencia de segment count

### Riesgo
romper cancelación/reintentos

### Mitigación
- agregar tests dirigidos a context cancellation y retries

## 2.5 Resultado esperado
- runtime más navegable
- separación clara entre workflow y algoritmo
- menor fricción para futuras mejoras de worker/concurrency

---

# Fase 3 — Refactor de `internal/api/router.go`

## Objetivo
Separar rutas por recurso sin cambiar contratos HTTP.

## Regla clave
Las rutas, payloads y response shapes deben permanecer iguales.

## Estructura objetivo

- `internal/api/router.go`
- `internal/api/router_auth.go`
- `internal/api/router_settings.go`
- `internal/api/router_providers.go`
- `internal/api/router_prompts.go`
- `internal/api/router_novels.go`
- `internal/api/router_chapters.go`
- `internal/api/router_jobs.go`
- `internal/api/router_epubs.go`
- `internal/api/router_import.go`
- `internal/api/router_responses.go`
- `internal/api/router_helpers.go`

## 3.1 Distribución propuesta

### `router.go`
Debe quedar mínimo:
- `Server`
- `New`
- `Router`
- `registerRoutes`
- wiring de grupos principales

### `router_auth.go`
Mover:
- `registerAuthRoutes`

### `router_settings.go`
Mover handlers de:
- `/api/defaults`
- `/api/user/settings`

### `router_providers.go`
Mover handlers de:
- `/api/user/providers`
- `/api/user/providers/{providerKey}`
- `/api/user/providers/{providerKey}/key`

### `router_prompts.go`
Mover handlers de:
- `/api/user/prompts`
- `/api/user/prompts/{key}`

### `router_import.go`
Mover handlers de:
- `/api/db/novels/import-epub`
- `/api/db/novels/preview-from-url`
- `/api/db/novels/import-from-url`
- `/api/db/novels/{id}/update-preview`
- `/api/db/novels/{id}/update-from-url`
- `/api/epubs/preview`

### `router_novels.go`
Mover handlers de:
- `/api/db/novels`
- `/api/db/novels/{id}`
- `/api/db/novels/{id}/copy`
- `/api/db/novels/{id}/visibility`

### `router_chapters.go`
Mover handlers de:
- `/api/db/novels/{novelId}/chapters`
- `/api/db/novels/{novelId}/chapters/{chapterId}`
- `/api/db/novels/{novelId}/chapters/bulk-delete`
- `/api/db/novels/{novelId}/chapters/{chapterId}/status`

### `router_jobs.go`
Mover handlers de:
- `/api/db/novels/{novelId}/translation-jobs`
- `/api/db/translation-jobs/active`
- `/api/db/translation-jobs/{jobId}`

### `router_epubs.go`
Mover handlers de:
- `/api/epubs`
- `/api/epubs/{id}/download`

### `router_responses.go`
Mover:
- `parseJSONFields`
- `promptToResponse`
- `promptsToResponse`
- `chapterRecord`
- `jobRecord`
- `epubRecord`

### `router_helpers.go`
Mover:
- `notFoundOrForbidden`
- `defaultTheme`
- `jsonString` si sigue siendo necesario aquí
- `defaultString` si sigue siendo necesario aquí
- `bearerToken`

## 3.2 Refactor adicional recomendado

### Sustituir handlers inline muy grandes por métodos con nombre
Ejemplo:
- `handleImportEpub`
- `handlePreviewNovelFromURL`
- `handleImportNovelFromURL`
- `handleUpdateNovelFromURL`

Beneficio:
- menos nesting visual
- handlers más fáciles de testear en aislamiento

### Separar DTOs request/response cuando convenga
No hace falta rediseñar todo, pero sí conviene crear structs para requests muy repetidos.

Ejemplos claros:
- auth register/login
- provider update
- provider API key replace
- create translation job
- update chapter status

Regla:
- mantener exactos los mismos campos JSON públicos

## 3.3 Riesgos

### Riesgo
drift en JSON de respuesta al tocar `map[string]any`

### Mitigación
- golden tests o assertions exactas sobre payloads críticos
- no cambiar nombres de keys

### Riesgo
romper binding/validación

### Mitigación
- tests de status codes por endpoint
- snapshot de errores principales

## 3.4 Resultado esperado
- routing más legible
- ownership por recurso claro
- menor riesgo de tocar auth al cambiar jobs

---

# Fase 4 — Refactor de `internal/epubimport/parser.go`

## Objetivo
Partir el parser EPUB en módulos algorítmicos más pequeños manteniendo el mismo punto de entrada público.

## Regla clave
`Parse(blob, filename)` debe seguir siendo el entrypoint principal.

## Estructura objetivo

- `internal/epubimport/parser.go`
- `internal/epubimport/container.go`
- `internal/epubimport/metadata.go`
- `internal/epubimport/manifest.go`
- `internal/epubimport/ncx.go`
- `internal/epubimport/chapter_extract.go`
- `internal/epubimport/normalize.go`
- `internal/epubimport/zip.go`
- `internal/epubimport/types.go`

## 4.1 Distribución sugerida

### `types.go`
- `Chapter`
- `Result`
- `manifestItem`
- `ncxNavPoint`

### `zip.go`
- `readZipFile`
- helpers de paths dentro del zip

### `container.go`
- `parseContainer`
- regex/container-specific helpers

### `metadata.go`
- `parseMetadata`
- `parseCoverID`
- `findCover`
- helpers de extracción de metadata

### `manifest.go`
- `parseManifest`
- `parseSpine`
- `isHTMLItem`
- `shouldSkipManifestItem`

### `ncx.go`
- `parseNCXNavPoints`
- NCX parsing helpers

### `chapter_extract.go`
- `splitChaptersFromHeadings`
- `splitChaptersFromNCXToFragments`
- `extractTitle`
- `shouldSkipChapter`

### `normalize.go`
- `removeScriptTags`
- `normalizeMarkdown`
- `normalizeDescription`
- `stripLeadingMarkdownHeading`
- `extractTagValues`
- `extractFirstMatch`
- `firstNonEmpty`

## 4.2 Riesgos

### Riesgo
cambiar comportamiento sobre EPUBs edge-case

### Mitigación
- ampliar tests con fixtures reales
- mantener `Parse` intacto mientras solo delega

## 4.3 Resultado esperado
- parser aún grande a nivel de package, pero ya no monolítico por archivo
- más fácil agregar soporte a variantes EPUB conflictivas

---

# Fase 5 — Refactor de `internal/noveldownloader`

## Objetivo
Separar parsers site-specific y consolidar helpers compartidos.

## 5.1 Estructura sugerida

### Para `novelfire`
- `internal/noveldownloader/novelfire.go`
- `internal/noveldownloader/novelfire_metadata.go`
- `internal/noveldownloader/novelfire_chapters.go`
- `internal/noveldownloader/novelfire_content.go`

### Para `novelbin`
Mismo patrón:
- `novelbin.go`
- `novelbin_metadata.go`
- `novelbin_chapters.go`
- `novelbin_content.go`

### Compartidos
- `internal/noveldownloader/url_helpers.go`
- `internal/noveldownloader/html_helpers.go`

## 5.2 Qué dejar igual
- `Downloader`
- `Parser` interface
- `FindParser`
- `GetNovelInfo`
- `DownloadChapter`
- `DownloadChapters`

## 5.3 Mejora opcional segura
Unificar helpers como:
- `resolveURL`
- `extractBaseURL`
- limpieza común de títulos/contenido

## 5.4 Riesgos

### Riesgo
romper scraping por cambios involuntarios de selector

### Mitigación
- no cambiar selectores en fase de extracción
- ampliar tests por fixture HTML real

---

# Fase 6 — Observabilidad, errores y consistencia interna

## Objetivo
Cerrar la deuda no estructural que quedó visible durante el análisis.

## 6.1 Logging

### Problema actual
Mezcla de:
- `slog`
- `fmt.Fprintf(os.Stderr, ...)`

### Plan
- usar `slog` de forma consistente para logging operacional
- mantener error/status en DB como feedback de producto
- evitar stderr manual salvo bootstrap fatal muy específico

### Tareas
- reemplazar en `router.go` los warnings de covers por `slog.Warn`
- revisar si `job failed`, `list runnable jobs`, `refine edits skipped` tienen campos consistentes

## 6.2 Errores

### Problema actual
- algunos errores son operacionales y otros contractuales, pero no siempre están claramente diferenciados

### Plan
- mantener HTTP errors visibles como hoy
- mejorar wrapping interno con contexto uniforme

Ejemplo deseable:
- `fmt.Errorf("resolve provider settings: %w", err)`
- `fmt.Errorf("load job chapters: %w", err)`

## 6.3 Helpers duplicados

### Problema actual
- helpers tipo `jsonString` / `defaultString` existen en más de un paquete

### Plan
- no introducir paquete util genérico todavía
- consolidar solo dentro de cada package
- evaluar después si merece `internal/xjson` o `internal/xstrings`, pero solo si la duplicación persiste y es estable

---

# Fase 7 — Testing de regresión más fuerte

## Objetivo
Hacer seguro el mantenimiento futuro después del refactor.

## 7.1 Tests por contrato API
Agregar o reforzar tests para:
- auth success/failure
- user settings read/write
- provider settings update
- prompt update
- novel CRUD
- chapter CRUD
- create/cancel/retry jobs
- epub upload/download
- import from URL

## 7.2 Tests de runtime
Agregar o reforzar:
- retry semantics
- cancelación por contexto
- segmentación con distintos thresholds
- refine edit application
- check-enabled vs check-disabled

## 7.3 Tests de store
Agregar tests por contexto separado:
- auth
- providers
- translation settings
- novel visibility
- chapter updates
- job reconciliation
- epub retrieval

## 7.4 Tests de scraping/parsing
- fixture tests para `novelfire`
- fixture tests para `novelbin`
- fixtures EPUB edge-case

---

# Roadmap de ejecución recomendado

## Sprint 1 — Base segura
- arreglar `go vet`
- correr `gofmt`
- estabilizar tests baseline
- documentar `Concurrency` como no implementada operacionalmente

## Sprint 2 — Partir `store.go`
- helpers
- mappers
- auth/settings/providers
- novels/chapters/jobs/epubs
- schema al final

## Sprint 3 — Partir `runtime.go`
- segmentation
- prompt/config
- refine
- translate
- worker
- mejora de sleep cancelable

## Sprint 4 — Partir `router.go`
- responses/helpers
- auth/settings/providers/prompts
- novels/chapters/jobs
- import/epubs

## Sprint 5 — Cohesivos grandes
- `epubimport`
- `noveldownloader`

## Sprint 6 — Consolidación
- logging uniforme
- más tests
- cleanup menor

---

# Criterios de éxito

## Éxito mínimo
- mismo comportamiento externo
- `go test ./...` pasa
- `go vet ./...` pasa
- archivos principales reducidos y mejor distribuidos

## Éxito bueno
- ownership por dominio claro
- runtime más entendible
- router más navegable
- store deja de ser cuello de botella cognitivo

## Éxito excelente
- futuras features de jobs/parsers/settings pueden tocarse sin miedo transversal
- agregar soporte a nuevos providers o nuevas fuentes web resulta local, no global

---

# Riesgos estratégicos

## Riesgo 1 — “Refactor infinito”
Si se intenta rediseñar mientras se parte, el alcance explota.

### Control
- esta fase es estructural, no de producto
- no rediseñar contratos HTTP
- no activar concurrencia real todavía

## Riesgo 2 — Cambios invisibles de comportamiento
Mover lógica puede cambiar defaults sutilmente.

### Control
- tests antes y después
- cambios semánticos aislados en commits separados

## Riesgo 3 — Introducir nuevas abstracciones innecesarias
No conviene llenar de interfaces o services artificiales.

### Control
- priorizar separación por archivo/package antes que por patterns sofisticados
- mantener concrete types donde hoy funcionan

---

# Decisiones explícitas del plan

## Lo que sí se hace
- separar por responsabilidad
- mejorar observabilidad básica
- reforzar tests
- arreglar timing/cancelación donde es claramente incorrecto

## Lo que no se hace todavía
- rediseñar el modelo de dominio
- introducir arquitectura hexagonal completa
- activar procesamiento concurrente real
- cambiar colección/field names de PocketBase
- cambiar frontend
- cambiar shape de respuestas API

---

# Siguiente paso recomendado

Si este plan se va a ejecutar, el mejor inicio práctico es abrir un primer PR con alcance chico y seguro:

## PR 1 recomendado
- fix `go vet` en `internal/config/config.go`
- correr `gofmt`
- extraer `internal/store/store_helpers.go`
- extraer `internal/store/store_mapping.go`
- validar `go test ./...`

Eso crea tracción sin entrar todavía en las zonas más riesgosas del runtime o del router.
