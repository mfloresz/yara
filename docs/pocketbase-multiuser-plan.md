# Plan de implementación: PocketBase embebido + multiusuario

## Estado de decisión

Este plan ajusta la factibilidad inicial con las decisiones confirmadas:

- PocketBase se integra **dentro del binario `translator-server`**, no como proceso externo.
- Si no se especifica `--data-dir`, la carpeta `data/` se crea junto al binario ejecutado.
- Si se especifica `--data-dir`, PocketBase crea/usa sus archivos allí.
- No se requieren migraciones ni conservación de datos existentes.
- Cada usuario tiene aislamiento completo de:
  - novelas privadas
  - capítulos
  - EPUBs/files
  - jobs
  - API keys
  - prompts generales modificados
  - prompts/configuración de novelas
  - futuros providers custom
- Las novelas públicas son visibles por otros usuarios, pero solo el propietario puede modificarlas.
- Otros usuarios pueden copiar novelas públicas; la copia pasa a ser una novela nueva propiedad del usuario que copia.
- Las API keys son **write-only desde la UI**:
  - el usuario puede crear/reemplazar una key
  - el backend nunca devuelve el valor completo
  - la UI solo muestra un indicador tipo `••••••••` / `configured: true`
- Si cifrar API keys no rompe el uso para llamar proveedores, debe implementarse cifrado en reposo.
- El tema puede guardarse en `localStorage`, pero también debe persistirse en DB para sincronizar entre navegadores.
- Providers/endpoints por defecto son definidos en desarrollo/backend y se copian o resuelven para cada usuario nuevo.
- API keys son siempre por usuario.
- Prompts generales parten de defaults, pero si un usuario los modifica, quedan en su configuración personal.
- Prompts de novela también son por novela/propietario.
- El usuario solo ve jobs que inició o que pertenecen a sus novelas.
- El backend puede procesar jobs de diferentes usuarios internamente.
- PocketBase files puede usarse para EPUBs/covers/blobs.
- Hay que implementar auth frontend.

---

## Objetivo

Convertir `translator-server` de una aplicación local single-tenant con SQLite manual a una aplicación multiusuario con PocketBase embebido, manteniendo un único binario autocontenido con frontend embebido y persistencia local en una carpeta `data/`.

---

## Invariantes del diseño

### Estado / ownership

- `users` es la fuente de identidad.
- Toda entidad editable por usuario debe tener ownership directo o indirecto.
- Una novela tiene exactamente un `owner`.
- Capítulos, EPUBs y jobs pertenecen a una novela y heredan su ownership.
- API keys pertenecen directamente a un usuario.
- Prompts generales personalizados pertenecen directamente a un usuario.
- Prompts de novela pertenecen indirectamente al propietario de la novela.

### Feedback / observabilidad

- Cada job debe registrar estado, progreso, error y usuario/novela asociada.
- La UI solo consulta jobs visibles para el usuario autenticado.
- Los errores de permisos deben devolver 401/403/404 según corresponda, evitando filtrar existencia de recursos privados.

### Blast radius

- La integración debe minimizar cambios innecesarios en el frontend existente.
- Mantener rutas de dominio `/api/...` donde sea razonable, usando PocketBase como auth/storage interno.
- Evitar acoplar toda la UI directamente a la API nativa de PocketBase en la primera fase.

### Timing / jobs

- El worker debe resolver el contexto del job en tiempo de ejecución:
  - job
  - novela
  - owner
  - provider config
  - API key del owner
  - prompts efectivos
  - opciones de traducción efectivas
- Nunca debe usar API keys o settings de otro usuario.

---

## Arquitectura recomendada

### Enfoque

Usar PocketBase embebido como aplicación principal y registrar encima:

- rutas custom del dominio de Yara/translator
- static handler del frontend embebido
- worker de jobs
- seed de colecciones/defaults

El binario sigue siendo `translator-server`.

### Forma conceptual del arranque

```text
main
 ├─ cargar config CLI/env
 ├─ resolver data dir
 ├─ crear PocketBase app con DataDir
 ├─ registrar colecciones/defaults si no existen
 ├─ registrar rutas custom /api/...
 ├─ registrar frontend embebido
 ├─ iniciar worker de jobs
 └─ app.Start()
```

---

## CLI/configuración

### Flags deseados

```bash
translator-server
translator-server --port 9000
translator-server --addr 127.0.0.1:9000
translator-server --data-dir ./data
translator-server --port 9000 --data-dir /var/lib/yara/data
```

### Reglas

- `--addr` tiene prioridad sobre `--port` si ambos se especifican.
- `--port 9000` equivale a `--addr :9000`.
- `--data-dir` define la carpeta raíz de datos.
- Si `--data-dir` no se especifica:
  - obtener ruta del ejecutable con `os.Executable()`
  - crear/usar `<dir-del-binario>/data`
- En desarrollo con `go run`, aceptar que la ruta del ejecutable sea temporal solo si no se define `--data-dir`; para dev se recomienda pasar `--data-dir ./data`.

### Env opcionales

- `ADDR`
- `PORT`
- `DATA_DIR`
- `STATIC_DIR` para desarrollo
- `APP_ENCRYPTION_KEY` o equivalente para cifrado de API keys

---

## Modelo de datos PocketBase

> Nombres sugeridos. Pueden ajustarse durante implementación si PocketBase impone convenciones específicas.

### `users` auth collection

Campos adicionales sugeridos:

| Campo | Tipo | Notas |
|---|---|---|
| `name` | text | opcional |
| `theme` | select/text | `light`, `dark`, `system` |
| `created`/`updated` | built-in | PocketBase |

Auth:

- email/password inicialmente.
- OAuth puede quedar fuera de alcance inicial.

---

### `providers`

Providers definidos por backend/desarrollo.

| Campo | Tipo | Notas |
|---|---|---|
| `key` | text unique | ej. `openai`, `venice` |
| `label` | text | nombre visible |
| `base_url` | text | endpoint por defecto |
| `default_model` | text | modelo default |
| `enabled` | bool | visible/usable |
| `kind` | select | `openai-compatible` |
| `owner` | relation users nullable | reservado para futuros custom providers |

Regla inicial:

- providers built-in (`owner` vacío/null) son legibles por usuarios autenticados.
- providers custom futuros tendrán `owner = @request.auth.id`.

---

### `user_provider_settings`

Configuración por usuario para provider sin exponer API key.

| Campo | Tipo | Notas |
|---|---|---|
| `owner` | relation users required | propietario |
| `provider` | relation providers required | provider |
| `model` | text | override opcional |
| `base_url` | text | override opcional para custom/advanced si aplica |
| `api_key_encrypted` | text/blob | nunca devuelto a UI |
| `api_key_configured` | bool | derivado o mantenido para UI |
| `api_key_updated_at` | date | indicador |

Índice:

- unique `(owner, provider)`

Reglas:

- list/view/update/create: solo owner.
- El campo `api_key_encrypted` nunca debe serializarse en endpoints custom de UI.
- Preferir no usar API directa de PocketBase para este recurso desde frontend si no se puede ocultar el campo con seguridad suficiente.

---

### `user_prompt_settings`

Prompts generales por usuario.

| Campo | Tipo | Notas |
|---|---|---|
| `owner` | relation users required | propietario |
| `key` | select/text | `translation`, `refine`, `check` |
| `label` | text | visible |
| `description` | text | visible |
| `system_prompt` | text | personalizado |
| `user_prompt` | text | personalizado |
| `active` | bool | habilitado |

Índice:

- unique `(owner, key)`

Regla:

- solo owner.

Seed:

- al crear usuario, se pueden copiar defaults.
- alternativa recomendada: resolver defaults en lectura y crear registro solo cuando el usuario modifica.

---

### `user_translation_settings`

Defaults de traducción por usuario.

| Campo | Tipo | Notas |
|---|---|---|
| `owner` | relation users unique | propietario |
| `auto_segment` | bool | default actual |
| `threshold_chars` | number | default actual |
| `max_chars` | number | default actual |
| `min_chars` | number | default actual |
| `max_retries` | number | default actual |
| `enable_check` | bool | default actual |
| `include_previous_title_hints` | bool | default actual |
| `concurrency` | number | default actual |

Regla:

- solo owner.

---

### `novels`

| Campo | Tipo | Notas |
|---|---|---|
| `owner` | relation users required | propietario |
| `title` | text | requerido |
| `author` | text | opcional |
| `description` | text | opcional |
| `source_language` | text | requerido |
| `target_language` | text | requerido |
| `source_metadata` | json | metadata actual |
| `target_metadata` | json | metadata actual |
| `glossary` | json | array |
| `prompts` | json | prompts específicos de novela |
| `notes` | text | notas |
| `ai_options` | json | provider/model override de novela |
| `translation_options` | json | overrides |
| `cleanup_rules` | json | array |
| `url` | text | fuente |
| `custom_commands` | text | actuales |
| `cover` | file | PocketBase file |
| `is_public` | bool | visibilidad pública |

Reglas:

- list/view: `owner = @request.auth.id || is_public = true`
- create: auth requerida y `owner` debe ser el usuario autenticado
- update/delete: solo `owner = @request.auth.id`

Nota:

- En endpoints custom, el backend debe forzar `owner` desde auth, no confiar en body del cliente.

---

### `chapters`

| Campo | Tipo | Notas |
|---|---|---|
| `novel` | relation novels required | novela |
| `chapter_order` | number | orden |
| `title` | text | original |
| `translated_title` | text | traducido |
| `original_content` | text | original |
| `translated_content` | text | traducción |
| `refined_content` | text | refinado |
| `status` | select | `pending`, `processing`, `translated`, `refined`, `done`, `failed` |
| `error_message` | text | error |

Índice:

- unique `(novel, chapter_order)`

Reglas:

- list/view: visible si novela es propia o pública.
- create/update/delete: solo si `novel.owner = @request.auth.id`.

---

### `translation_jobs`

| Campo | Tipo | Notas |
|---|---|---|
| `owner` | relation users required | usuario que inició el job |
| `novel` | relation novels required | novela |
| `status` | select | `pending`, `running`, `done`, `cancelled`, `failed` |
| `operation` | select | `translate`, `refine` |
| `provider` | relation/text | provider efectivo |
| `model` | text | modelo efectivo |
| `chapter_ids` | json | ids seleccionados |
| `options_json` | json | opciones |
| `error_message` | text | error |
| `total_chapters` | number | progreso |
| `completed_chapters` | number | progreso |
| `failed_chapters` | number | progreso |

Reglas:

- list/view/update-visible: solo `owner = @request.auth.id`.
- create: solo si la novela es propia.
- no permitir crear jobs sobre novela pública ajena; primero debe copiarse.

Worker:

- puede leer todos internamente como backend/admin.
- UI solo recibe jobs propios.

---

### `epubs`

| Campo | Tipo | Notas |
|---|---|---|
| `novel` | relation novels required | novela |
| `file_kind` | select | `original`, `translated` |
| `source_variant` | select/text | `original`, `translated`, `refined`, vacío |
| `label` | text | opcional |
| `file` | file | PocketBase file |

Reglas:

- list/view/download: visible si novela propia o pública, según producto deseado.
- create/update/delete: solo owner de novela.

Nota:

- Para novelas públicas, decidir si el EPUB original/traducido también es descargable públicamente. Baseline recomendado: si la novela es pública, sus capítulos son visibles; EPUB download puede ser público solo si explícitamente se desea. Si no, limitar descarga a owner para reducir exposición.

---

## API keys: política write-only

### Reglas de UX

La UI nunca debe renderizar una API key real recibida desde backend.

Flujo:

1. Usuario abre settings.
2. Backend responde por provider:

```json
{
  "provider": "openai",
  "label": "OpenAI",
  "model": "gpt-4.1-mini",
  "baseUrl": "https://api.openai.com/v1",
  "apiKeyConfigured": true,
  "apiKeyUpdatedAt": "2026-06-14T...Z"
}
```

3. UI muestra campo password con placeholder/valor visual:

```text
••••••••••••
```

4. Si usuario escribe una key nueva y guarda:

```json
{
  "provider": "openai",
  "apiKey": "new-secret-key"
}
```

5. Backend cifra y guarda.
6. Backend responde sin key:

```json
{
  "provider": "openai",
  "apiKeyConfigured": true
}
```

### Backend

- Endpoint separado para reemplazar key:
  - `PUT /api/user/providers/:provider/key`
- Endpoint de settings nunca devuelve key.
- Al guardar una key vacía, definir semántica explícita:
  - no cambiar key, o
  - borrar key con endpoint dedicado `DELETE /api/user/providers/:provider/key`

Recomendación:

- campo vacío en update normal = no cambiar
- botón explícito “Eliminar API key” = delete

---

## Cifrado de API keys

### Recomendación

Implementar cifrado en reposo desde el inicio.

### Requisito

El backend debe poder descifrar la key para llamar al proveedor.

### Diseño

- Usar una clave maestra de aplicación.
- Variable recomendada:

```bash
APP_ENCRYPTION_KEY=<32 bytes base64 o hex>
```

- Si no existe en desarrollo:
  - generar una clave local y guardarla en `data/app.key`
  - advertir en logs que perder esa clave inutiliza API keys cifradas
- En producción:
  - recomendar pasarla por env/secreto

### Algoritmo recomendado

- AES-256-GCM o XChaCha20-Poly1305.
- Guardar nonce + ciphertext + versión.

Formato conceptual:

```text
v1:<base64 nonce+ciphertext>
```

### Rotación

Fuera de alcance inicial.

---

## Providers y defaults

### Providers por defecto

Los providers/endpoints definidos por desarrollo/backend deben estar disponibles para todos los usuarios.

Estrategia recomendada:

- `providers` contiene providers built-in.
- `user_provider_settings` contiene solo overrides y API key del usuario.
- Al consultar settings, backend compone:

```text
provider built-in + user override + apiKeyConfigured
```

Ventaja:

- si cambia un endpoint default en desarrollo/backend, aplica globalmente salvo override explícito.
- no contamina API keys ni settings privados.

### Custom providers futuros

Reservar `providers.owner` nullable:

- `owner = null`: built-in/global
- `owner = user`: custom provider privado

No implementar custom providers ahora salvo que sea necesario.

---

## Prompts

### Prompts generales

Modelo efectivo:

```text
default prompt del backend
  overridden by user_prompt_settings(owner, key)
```

No compartir modificaciones entre usuarios.

### Prompts de novela

Permanecen dentro de la novela o colección relacionada por novela.

Modelo efectivo para job:

```text
backend default prompt
  -> user general prompt override
  -> novel prompt override
```

La novela pública expone sus prompts efectivos solo en la medida necesaria para lectura/copia. Para evitar fuga innecesaria, la vista pública puede devolver solo contenido y metadata pública, no necesariamente settings internos de traducción.

---

## Novelas públicas y copia

### Visibilidad

Una novela pública puede ser vista por otros usuarios.

### Edición

Solo el owner puede modificarla.

### Cambios del owner

Otros usuarios ven los cambios porque consultan el registro original público.

### Copia

Endpoint recomendado:

```http
POST /api/novels/{id}/copy
```

Condiciones:

- usuario autenticado requerido
- novela debe ser pública o propia

Acción:

- crea nueva novela con `owner = currentUser`
- `is_public = false` por defecto
- clona capítulos
- clona metadata/glossary/prompts de novela si aplica
- no clona jobs históricos
- no clona API keys ni settings de owner original
- decidir si clona EPUB files; baseline recomendado: no clonar EPUBs inicialmente, o clonar solo si es necesario para funcionalidad

---

## Jobs multiusuario

### UI

El usuario solo ve:

- jobs que inició
- jobs asociados a novelas propias

Recomendación: usar `owner` directo en `translation_jobs` para query simple.

### Backend worker

El worker puede procesar cola global.

Al tomar job:

1. cargar job
2. cargar owner
3. cargar novela
4. validar ownership interna
5. cargar provider efectivo
6. descifrar API key del owner para provider efectivo
7. cargar prompts efectivos
8. cargar capítulos seleccionados
9. procesar
10. actualizar progreso

### Concurrencia

Mantener configuración global inicial simple:

- worker único o pool pequeño.
- permitir jobs de distintos usuarios en cola.
- evitar exponer jobs ajenos por SSE/polling.

Si se añade realtime más adelante, las subscriptions deben filtrar por owner.

---

## Frontend auth

### Funcionalidades mínimas

- registro
- login
- logout
- restaurar sesión
- rutas protegidas
- estado de usuario actual
- manejo de sesión expirada

### Storage

Permitido:

- token/session de PocketBase auth
- tema local para pre-render/arranque rápido

No permitido:

- novelas
- capítulos
- API keys
- prompts
- settings funcionales
- jobs

### Tema

Flujo recomendado:

1. Antes de login:
   - usar `localStorage.theme` si existe
   - si no, usar `prefers-color-scheme`
2. Después de login:
   - cargar `user.theme` desde backend
   - aplicar theme
   - actualizar `localStorage.theme` como cache local
3. Cuando usuario cambia theme:
   - aplicar inmediatamente
   - guardar en `localStorage`
   - persistir en backend

---

## Backend routes recomendadas

Mantener API de dominio para evitar acoplamiento directo excesivo a PocketBase.

### Auth

Se puede usar API nativa de PocketBase o envolverla.

Opción recomendada:

- usar SDK/REST PocketBase desde frontend para auth si simplifica
- o crear endpoints propios si se quiere una superficie estable

Endpoints propios posibles:

```http
POST /api/auth/register
POST /api/auth/login
POST /api/auth/logout
GET  /api/auth/me
POST /api/auth/refresh
```

### User settings

```http
GET /api/user/settings
PUT /api/user/settings
GET /api/user/providers
PUT /api/user/providers/{providerKey}
PUT /api/user/providers/{providerKey}/key
DELETE /api/user/providers/{providerKey}/key
GET /api/user/prompts
PUT /api/user/prompts/{key}
```

### Novels

Mantener equivalentes actuales:

```http
GET    /api/db/novels
POST   /api/db/novels
GET    /api/db/novels/{id}
PATCH  /api/db/novels/{id}
DELETE /api/db/novels/{id}
POST   /api/db/novels/{id}/copy
PATCH  /api/db/novels/{id}/visibility
```

### Chapters

```http
GET    /api/db/novels/{novelId}/chapters
GET    /api/db/novels/{novelId}/chapters/{chapterId}
POST   /api/db/novels/{novelId}/chapters
DELETE /api/db/novels/{novelId}/chapters/{chapterId}
POST   /api/db/novels/{novelId}/chapters/bulk-delete
PATCH  /api/db/novels/{novelId}/chapters/{chapterId}/status
```

Debe validar ownership en cada operación de escritura.

### Jobs

```http
POST /api/db/novels/{novelId}/translation-jobs
GET  /api/db/novels/{novelId}/translation-jobs
GET  /api/db/translation-jobs/active
PATCH /api/db/translation-jobs/{jobId}
```

`active` debe devolver solo jobs del usuario autenticado.

---

## Cambios por área del código actual

### `internal/config/config.go`

Cambios:

- añadir `DataDir string`
- añadir `Port string/int` si se quiere flag separado
- resolver prioridad `--addr` > `--port` > env > default
- resolver `data` junto al binario por defecto
- reemplazar o deprecar `DBPath`

### `cmd/server`

Actualmente el checkout inspeccionado muestra `cmd/server` vacío y no se encontró `package main`.

Antes de implementar hay que:

- restaurar/crear entrypoint real
- inicializar PocketBase embebido ahí
- conectar config, routes, static y worker

### `internal/store`

Opciones:

1. Reemplazar progresivamente SQL manual por repositorios PocketBase.
2. Crear una interfaz de store y una implementación PocketBase.

Recomendación:

- crear una capa `internal/repository` o reemplazo incremental de `internal/store`.
- evitar conservar migraciones antiguas si no hacen falta.

### `internal/api/router.go`

Cambios:

- añadir auth middleware o helper para obtener usuario actual.
- validar ownership en todas las rutas.
- adaptar CRUD a PocketBase.
- nunca confiar en `owner` enviado por frontend.
- ocultar API keys en todas las respuestas.

### `internal/api/runtime.go`

Cambios:

- `resolveJobConfig` debe ser user-aware.
- `newAIProvider` debe recibir provider config + API key descifrada del usuario.
- jobs deben cargarse con owner.
- si falta API key del usuario para provider, job falla con error claro.

### `frontend/src/api/http.ts`

Cambios:

- adjuntar token auth si se usa Authorization Bearer.
- manejar 401 con logout/redirect.
- mantener `credentials` solo si se decide cookie-based; PocketBase usa Authorization header por defecto.

### `frontend/src/app/services.ts`

Cambios:

- añadir auth service/store.
- cargar settings de usuario después de auth.
- cargar providers compuestos por usuario.

### `frontend/index.html`

Cambios:

- se permite mantener `localStorage.theme`.
- asegurar que no se guarda nada más funcional.
- el theme debe sincronizarse con backend tras login.

### UI nueva

Añadir:

- Login/Register page
- Account/User Settings page o sección
- Provider/API key settings con campo write-only
- Indicador `API key configurada`
- Botón reemplazar key
- Botón eliminar key
- Toggle publicar novela
- Acción copiar novela pública

---

## Fases de implementación

### Fase 0 — Preparación

Objetivo:

- arreglar entrypoint si falta
- confirmar build
- introducir config nueva sin cambiar dominio aún

Tareas:

- crear/restaurar `cmd/server/main.go`
- añadir `--data-dir`
- añadir `--port`
- documentar `data/`
- verificar build mínimo

Validación:

```bash
rtk err go build ./cmd/server
```

---

### Fase 1 — PocketBase embebido

Objetivo:

- arrancar PocketBase dentro del binario
- usar `data/` resuelto por config
- servir frontend embebido y healthcheck

Tareas:

- añadir dependencia PocketBase
- inicializar app con DataDir
- registrar static handler actual
- registrar `/healthz`
- crear seed/schema inicial sin migraciones versionadas complejas

Validación:

```bash
rtk err go build ./cmd/server
rtk test go test ./...
```

---

### Fase 2 — Auth backend + frontend

Objetivo:

- usuarios reales
- login/register/logout
- sesión persistente
- rutas protegidas

Tareas backend:

- configurar `users` auth collection
- crear helpers auth en rutas custom
- endpoint/me o uso API nativa PB

Tareas frontend:

- auth store
- login page
- register page
- route guards
- logout
- sesión expirada

Validación:

- registrar usuario
- login
- refresh/restaurar sesión
- logout
- acceso anónimo bloqueado a dashboard

---

### Fase 3 — Modelo de datos multiusuario

Objetivo:

- reemplazar tablas actuales por colecciones PB con ownership

Tareas:

- crear colecciones:
  - providers
  - user_provider_settings
  - user_prompt_settings
  - user_translation_settings
  - novels
  - chapters
  - translation_jobs
  - epubs
- definir reglas de acceso
- seed providers/prompts defaults

Validación:

- usuario A no ve novelas privadas de usuario B
- usuario A no modifica novela de B
- novela pública de B es visible para A
- A no puede editar novela pública de B

---

### Fase 4 — API keys write-only + cifrado

Objetivo:

- permitir configurar/reemplazar/borrar API keys sin exponerlas

Tareas:

- implementar cifrado en reposo
- resolver/generar key de cifrado dev en `data/app.key` si no existe env
- endpoints para provider settings
- respuesta con `apiKeyConfigured` sin key real
- UI write-only

Validación:

- guardar key
- recargar UI y confirmar que no aparece key real
- reemplazar key
- borrar key
- intentar interceptar response y confirmar que no contiene key
- job puede usar key descifrada internamente

---

### Fase 5 — Novelas/capítulos/files con ownership

Objetivo:

- portar CRUD actual a PB y files

Tareas:

- crear/listar/editar/eliminar novelas propias
- importar EPUB como novela propia
- guardar covers/files en PocketBase files
- CRUD capítulos con ownership
- bulk delete con ownership
- endpoints de descarga seguros

Validación:

- A importa EPUB
- B no ve novela privada
- A elimina novela y se eliminan capítulos/files relacionados según comportamiento configurado

---

### Fase 6 — Publicación y copia

Objetivo:

- soportar novelas públicas y copia privada

Tareas:

- `is_public`
- toggle publicar/despublicar
- vista/listado de novelas públicas si aplica
- endpoint copy
- UI copiar novela pública

Validación:

- A publica novela
- B puede verla
- B no puede editarla
- A cambia contenido y B ve cambio
- B copia novela
- B puede editar su copia
- cambios posteriores de A no modifican la copia de B

---

### Fase 7 — Jobs multiusuario

Objetivo:

- jobs aislados por usuario y worker global seguro

Tareas:

- crear job solo para novela propia
- owner en job
- listar active jobs por owner
- resolver provider/API key/prompts por owner
- adaptar worker
- errores claros si falta API key

Validación:

- A y B ejecutan jobs simultáneos
- A solo ve jobs de A
- B solo ve jobs de B
- cada job usa API key de su owner
- job sobre novela pública ajena se rechaza

---

### Fase 8 — Settings/prompts/tema por usuario

Objetivo:

- eliminar contaminación global entre usuarios

Tareas:

- settings de traducción por usuario
- prompts generales por usuario
- theme sync DB + localStorage
- limpiar dependencia de `meta` global viejo

Validación:

- A cambia prompt general; B conserva default
- A cambia theme; al loguearse en otro navegador se carga theme de DB
- localStorage solo tiene auth/session y theme

---

### Fase 9 — Limpieza y documentación

Objetivo:

- eliminar restos single-tenant y documentar operación

Tareas:

- actualizar README
- documentar flags
- documentar ubicación `data/`
- documentar pérdida de datos por no migraciones
- documentar API key encryption key
- eliminar `translator.db` del flujo
- revisar `.gitignore` para `data/`, `pb_data`, keys locales

Validación:

```bash
rtk test go test ./...
rtk err npm run build
rtk err go build ./cmd/server
```

---

## Criterios de aceptación

### Multiusuario

- Un usuario no autenticado no puede acceder a dashboard ni APIs privadas.
- Usuario A no puede listar, ver, modificar ni eliminar novelas privadas de B.
- Usuario A no puede ver jobs de B.
- Usuario A no puede usar API keys de B.

### API keys

- Backend nunca devuelve API key real a frontend.
- UI solo muestra indicador de key configurada.
- API key puede reemplazarse.
- API key puede eliminarse explícitamente.
- Jobs pueden usar la key descifrada internamente.

### Publicación

- Novela pública puede ser vista por otros usuarios.
- Solo owner puede modificar novela pública.
- Otro usuario puede copiar novela pública.
- La copia es independiente y propiedad del usuario que copia.

### PocketBase/binario

- `translator-server` arranca PocketBase embebido.
- No hace falta proceso PocketBase externo.
- `data/` se crea junto al binario si no se especifica ruta.
- `--data-dir` cambia la ruta de datos.
- `--port` o `--addr` controlan el puerto/listen address.

### Storage navegador

- Se permite auth/session.
- Se permite theme.
- No se almacenan novelas, prompts, API keys, jobs ni settings funcionales en browser storage.
- Theme se sincroniza con DB por usuario.

---

## Dudas cerradas por decisión actual

- API keys: write-only en UI, cifradas si es compatible. Sí es compatible si el backend descifra antes de llamar provider.
- PocketBase: embebido en binario. Sí.
- Data dir: `--data-dir` o junto al binario. Sí.
- Theme: permitido en localStorage, también DB. Sí.
- Providers: built-in/defaults globales definidos por backend; API keys privadas por usuario. Sí.
- Prompts: defaults iniciales, overrides por usuario y por novela. Sí.
- Jobs: visibles solo para owner, worker global interno. Sí.
- Files: usar PocketBase files. Sí.
- Auth frontend: implementar. Sí.

---

## Riesgos principales

1. **Entry point actual ausente en el checkout inspeccionado**
   - `Makefile` compila `./cmd/server`, pero `cmd/server` aparece vacío.
   - Hay que restaurar/crear `main.go` antes de integrar PB.

2. **API rules vs endpoints custom**
   - Si el frontend usa APIs nativas PB directamente, hay que asegurar reglas perfectas.
   - Para API keys, preferir endpoints custom para controlar serialización.

3. **Cifrado y clave maestra**
   - Si se pierde la clave maestra, no se podrán descifrar API keys guardadas.
   - Documentar claramente.

4. **Jobs con permisos**
   - El worker corre como backend/admin y puede leer todo.
   - Debe validar owner por lógica de dominio para no mezclar contextos.

5. **Novela pública y exposición de metadata**
   - Decidir cuidadosamente qué metadata/settings internos se devuelven en vistas públicas.
   - Baseline seguro: devolver contenido/metadata editorial, no API/provider internals.

---

## Recomendación final

Proceder con implementación por fases, empezando por:

1. Restaurar/crear entrypoint `cmd/server/main.go`.
2. Integrar PocketBase embebido con `--data-dir`.
3. Añadir auth.
4. Migrar dominio a colecciones PB con ownership.
5. Implementar API keys write-only cifradas.

No conviene empezar por UI pública/copia antes de tener ownership, auth y storage asentados.
