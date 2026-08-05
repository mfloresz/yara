# Yara API Reference

> Documentación completa de la API REST de Yara (translator-server).

- **Base URL:** `http://<host>:5176`
- **Content-Type general:** `application/json`
- **Autenticación:** `Authorization: Bearer <token>` (obtenido via `/api/auth/login` o `/api/auth/register`)
- **Errores comunes:** `400 Bad Request`, `401 Unauthorized`, `403 Forbidden`, `404 Not Found`, `500 Internal Server Error`

---

## Índice

1. [Health Check](#1-health-check)
2. [Autenticación](#2-autenticación)
3. [Settings (Traducción por defecto)](#3-settings)
4. [Preferencias de Usuario](#4-preferencias-de-usuario)
5. [Providers (AI)](#5-providers-ai)
6. [Prompts (System/User)](#6-prompts)
7. [Novelas (CRUD)](#7-novelas-crud)
8. [Capítulos](#8-capítulos)
9. [Jobs (Trabajos de traducción/descarga)](#9-jobs)
10. [Importación (EPUB, ZIP, URL)](#10-importación)
11. [EPUBs (Archivos empaquetados)](#11-epubs)
12. [Exportación EPUB](#12-exportación-epub)
13. [Reading Progress](#13-reading-progress)
14. [Glosario](#14-glosario)
15. [Browser Worker / Proxy](#15-browser-worker--proxy)
16. [Worker Auth](#16-worker-auth)
17. [Backup](#17-backup)

---

## 1. Health Check

Verifica que el servidor esté activo. **No requiere autenticación.**

### `GET /healthz`

```
GET /healthz
```

**Response `200 OK`**

```json
{
  "ok": true
}
```

---

## 2. Autenticación

Todas las rutas bajo `/api/auth`. **No requieren autenticación previa** (excepto `/me`, `/refresh`, `/logout`).

### `POST /api/auth/register`

```
POST /api/auth/register
Content-Type: application/json

{
  "email": "usuario@email.com",
  "password": "contraseña_segura",
  "name": "Nombre del Usuario"
}
```

**Response `201 Created`**

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "abc123def456",
    "email": "usuario@email.com",
    "name": "Nombre del Usuario",
    "theme": "system",
    "createdAt": "2024-01-15 12:00:00.000Z",
    "updatedAt": "2024-01-15 12:00:00.000Z"
  }
}
```

### `POST /api/auth/login`

```
POST /api/auth/login
Content-Type: application/json

{
  "email": "usuario@email.com",
  "password": "contraseña_segura"
}
```

**Response `200 OK`**

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "abc123def456",
    "email": "usuario@email.com",
    "name": "Nombre del Usuario",
    "theme": "system",
    "createdAt": "2024-01-15 12:00:00.000Z",
    "updatedAt": "2024-01-15 12:00:00.000Z"
  }
}
```

> El `token` es un JWT de PocketBase. En todos los requests siguientes incluir: `Authorization: Bearer <token>`.

### `GET /api/auth/me`

Requiere autenticación.

```
GET /api/auth/me
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "id": "abc123def456",
  "email": "usuario@email.com",
  "name": "Nombre del Usuario",
  "theme": "system",
  "createdAt": "2024-01-15 12:00:00.000Z",
  "updatedAt": "2024-01-15 12:00:00.000Z"
}
```

### `POST /api/auth/refresh`

Requiere autenticación. Refresca el token JWT.

```
POST /api/auth/refresh
Authorization: Bearer <token_antiguo>
```

**Response `200 OK`**

```json
{
  "token": "nuevo_token_jwt",
  "user": { ... }
}
```

### `POST /api/auth/logout`

Requiere autenticación.

```
POST /api/auth/logout
Authorization: Bearer <token>
```

**Response `204 No Content`**

---

## 3. Settings

### `GET /api/defaults`

Obtiene valores por defecto de traducción. **Requiere autenticación.**

```
GET /api/defaults
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "translation": {
    "autoSegment": true,
    "thresholdChars": 20000,
    "maxChars": 10000,
    "minChars": 500,
    "maxRetries": 2,
    "enableCheck": false,
    "includePreviousChapterTitles": false,
    "concurrency": 1
  }
}
```

---

## 4. Preferencias de Usuario

### `GET /api/user/settings`

Requiere autenticación.

```
GET /api/user/settings
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "theme": "system",
  "ai": {
    "provider": "venice",
    "baseUrl": "https://api.venice.ai/api/v1",
    "model": "deepseek-v4-flash",
    "timeoutMs": 120000,
    "concurrency": 1
  },
  "translation": {
    "autoSegment": true,
    "thresholdChars": 20000,
    "maxChars": 10000,
    "minChars": 500,
    "maxRetries": 2,
    "enableCheck": false,
    "includePreviousChapterTitles": false,
    "concurrency": 1
  },
  "titleProvider": "venice",
  "titleModel": "deepseek-v4-flash"
}
```

### `PUT /api/user/settings`

Requiere autenticación.

```
PUT /api/user/settings
Authorization: Bearer <token>
Content-Type: application/json

{
  "theme": "dark",
  "ai": {
    "provider": "venice",
    "baseUrl": "https://api.venice.ai/api/v1",
    "model": "deepseek-v4-flash",
    "timeoutMs": 120000,
    "concurrency": 1
  },
  "titleProvider": "venice",
  "titleModel": "deepseek-v4-flash",
  "translation": {
    "autoSegment": true,
    "thresholdChars": 20000,
    "maxChars": 10000,
    "minChars": 500,
    "maxRetries": 2,
    "enableCheck": false,
    "includePreviousChapterTitles": false,
    "concurrency": 1
  }
}
```

**Response `200 OK`** — misma estructura que GET.

---

## 5. Providers (AI)

### `GET /api/user/providers`

Lista los proveedores AI configurados.

```
GET /api/user/providers
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "providers": [
    {
      "provider": "venice",
      "label": "Venice",
      "baseUrl": "https://api.venice.ai/api/v1",
      "model": "deepseek-v4-flash",
      "models": ["deepseek-v4-flash", "deepseek-r1-671b", "qwen-2.5-coder-32b"],
      "kind": "openai",
      "apiKeyConfigured": true,
      "apiKeyUpdatedAt": "2024-01-15 12:00:00.000Z",
      "enabled": true,
      "timeoutMs": 120000,
      "concurrency": 1
    }
  ]
}
```

- `apiKeyConfigured` es `true` si el usuario ha guardado una API key (nunca se devuelve la key).
- `apiKeyUpdatedAt` timestamp de cuando se configuró la key por última vez.

### `PUT /api/user/providers/{providerKey}`

Actualiza configuración de un proveedor (modelo, base URL, timeout).

```
PUT /api/user/providers/venice
Authorization: Bearer <token>
Content-Type: application/json

{
  "model": "deepseek-v4-flash",
  "baseUrl": "https://api.venice.ai/api/v1",
  "timeoutMs": 120000
}
```

**Response `200 OK`** — estructura de provider.

### `PUT /api/user/providers/{providerKey}/key`

Guarda la API key de un proveedor. La key se encripta (AES-GCM) antes de persistir.

```
PUT /api/user/providers/venice/key
Authorization: Bearer <token>
Content-Type: application/json

{
  "apiKey": "sk-your-api-key-here"
}
```

**Response `200 OK`**

### `DELETE /api/user/providers/{providerKey}/key`

Elimina la API key de un proveedor.

```
DELETE /api/user/providers/venice/key
Authorization: Bearer <token>
```

**Response `204 No Content`**

---

## 6. Prompts

### `GET /api/user/prompts`

Lista los prompts personalizables del usuario.

```
GET /api/user/prompts
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
[
  {
    "id": "translation",
    "key": "translation",
    "label": "Traducción",
    "description": "Prompt global para traducción de capítulos.",
    "prompt": {
      "systemPrompt": "Eres un traductor literario...",
      "userPrompt": "Traduce el siguiente texto..."
    },
    "active": true,
    "updatedAt": "2024-01-15 12:00:00.000Z"
  },
  {
    "id": "refine",
    "key": "refine",
    "label": "Refinamiento",
    "prompt": { "systemPrompt": "...", "userPrompt": "..." },
    "active": true,
    "updatedAt": "..."
  },
  {
    "id": "check",
    "key": "check",
    "label": "Verificación",
    "prompt": { "systemPrompt": "...", "userPrompt": "..." },
    "active": true,
  },
  {
    "id": "glossary",
    "key": "glossary",
    "label": "Glosario",
    "prompt": { "systemPrompt": "...", "userPrompt": "" },
    "active": true,
  }
]
```

### `PUT /api/user/prompts/{key}`

Actualiza un prompt.

```
PUT /api/user/prompts/translation
Authorization: Bearer <token>
Content-Type: application/json

{
  "label": "Traducción",
  "description": "Prompt global para traducción de capítulos.",
  "prompt": {
    "systemPrompt": "Eres un traductor literario experto...",
    "userPrompt": "Traduce el siguiente texto al español..."
  },
  "active": true
}
```

**Response `200 OK`** — estructura de prompt.

---

## 7. Novelas (CRUD)

### `GET /api/db/novels`

Lista novelas accesibles por el usuario. Soporta paginación y búsqueda por texto.

```
GET /api/db/novels?limit=50&offset=0&select=id,sourceTitle,sourceAuthor
Authorization: Bearer <token>

GET /api/db/novels?q=harry+potter&limit=20&offset=0
Authorization: Bearer <token>
```

**Query params:**
- `limit` (opcional, default 100, max 1000) — cantidad de resultados por página.
- `offset` (opcional, default 0) — desplazamiento para paginación.
- `select` (opcional) — campos específicos separados por coma. Ej: `id,sourceTitle,sourceAuthor,coverPath`.
- `q` (opcional) — texto de búsqueda. Busca coincidencias parciales en `sourceTitle`, `sourceAuthor`, `sourceSeries`, `targetTitle`, `targetAuthor`, `targetSeries`, incluyendo novelas públicas (`isPublic: true`) además de las propias. Usa la misma paginación que el listado normal (`limit`/`offset`, con `hasMore`).

**Response `200 OK`**

```json
{
  "items": [
    {
      "id": "novel-id-abc",
      "ownerId": "user-id-123",
      "sourceLanguage": "en",
      "targetLanguage": "es",
      "sourceTitle": "The Wandering Inn",
      "sourceAuthor": "pirateaba",
      "sourceDescription": "An open world fantasy web novel...",
      "sourceSeries": "",
      "sourceNumber": "",
      "targetTitle": "La Posada Errante",
      "targetAuthor": "",
      "targetDescription": "",
      "targetSeries": "",
      "targetNumber": "",
      "url": "https://wanderinginn.com/",
      "customCommands": "",
      "status": "ongoing",
      "coverPath": "/api/files/novels/novel-id-abc/cover_filename.jpg",
      "isPublic": false,
      "chapterCount": 142,
      "translatedCount": 50,
      "completedCount": 30,
      "originalCharCount": 2500000,
      "translatedCharCount": 500000,
      "refinedCharCount": 300000,
      "totalCharCount": 3000000,
      "maxChapterOrder": 142,
      "lastCheckedAt": "2024-01-15 14:00:00.000Z",
      "lastCheckNewChapters": 3,
      "lastReadAt": "2024-01-15 12:00:00.000Z",
      "createdAt": "2024-01-10 10:00:00.000Z",
      "updatedAt": "2024-01-15 14:00:00.000Z",
      "glossary": [],
      "tags": ["fantasy", "webnovel"],
      "prompts": {
        "translation": { "systemPrompt": "", "userPrompt": "" },
        "refine": { "systemPrompt": "", "userPrompt": "" },
        "check": { "systemPrompt": "", "userPrompt": "" }
      },
      "aiOptions": {},
      "translationOptions": {},
      "cleanupRules": [],
      "notes": ""
    }
  ],
  "hasMore": true
}
```

> **Sobre covers:** El campo `coverPath` contiene una URL relativa como `/api/files/novels/{novelId}/{filename}`. Para obtener la imagen completa, anteponer el base URL del servidor: `http://192.168.1.105:5176/api/files/novels/{novelId}/{filename}`. Si hay un thumbnail disponible, `coverPath` devuelve el thumbnail; si no, devuelve la cover original. PocketBase maneja el file serving internamente — no se necesita un endpoint especial.

### `GET /api/db/novels/{id}`

Obtiene una novela específica.

```
GET /api/db/novels/novel-id-abc
Authorization: Bearer <token>
```

**Response `200 OK`** — mismo objeto novel que en el listado.

### `POST /api/db/novels`

Crea una novela nueva.

```
POST /api/db/novels
Authorization: Bearer <token>
Content-Type: application/json

{
  "sourceLanguage": "en",
  "targetLanguage": "es",
  "sourceTitle": "The Wandering Inn",
  "sourceAuthor": "pirateaba",
  "sourceDescription": "An open world fantasy...",
  "sourceSeries": "The Wandering Inn",
  "sourceNumber": "1",
  "targetTitle": "",
  "targetAuthor": "",
  "url": "https://wanderinginn.com/",
  "customCommands": "",
  "notes": "",
  "glossary": [],
  "prompts": {},
  "aiOptions": {},
  "translationOptions": {},
  "cleanupRules": [],
  "tags": ["fantasy"]
}
```

**Response `201 Created`** — objeto novel completo.

### `PATCH /api/db/novels/{id}`

Actualiza campos específicos de una novela. Solo se envían los campos a modificar.

```
PATCH /api/db/novels/novel-id-abc
Authorization: Bearer <token>
Content-Type: application/json

{
  "targetTitle": "La Posada Errante",
  "status": "completed",
  "notes": "Finalizada la traducción principal"
}
```

**Response `200 OK`** — objeto novel completo post-actualización.

### `POST /api/db/novels/{id}/cover`

Sube una imagen de portada para la novela.

```
POST /api/db/novels/novel-id-abc/cover
Authorization: Bearer <token>
Content-Type: multipart/form-data

campo: "cover" (archivo de imagen)
```

**Response `200 OK`** — objeto novel completo con el nuevo `coverPath`.

### `DELETE /api/db/novels/{id}`

Elimina una novela y todos sus capítulos.

```
DELETE /api/db/novels/novel-id-abc
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "ok": true
}
```

### `POST /api/db/novels/{id}/copy`

Crea una copia de la novela (incluye capítulos).

```
POST /api/db/novels/novel-id-abc/copy
Authorization: Bearer <token>
```

**Response `201 Created`** — objeto novel de la copia.

### `POST /api/db/novels/{id}/recalculate-stats`

Recalcula las estadísticas de la novela (conteo de capítulos, caracteres, etc).

```
POST /api/db/novels/novel-id-abc/recalculate-stats
Authorization: Bearer <token>
```

**Response `200 OK`** — objeto novel con stats actualizados.

### `PATCH /api/db/novels/{id}/visibility`

Cambia la visibilidad de la novela (pública/privada).

```
PATCH /api/db/novels/novel-id-abc/visibility
Authorization: Bearer <token>
Content-Type: application/json

{
  "isPublic": true
}
```

**Response `200 OK`** — objeto novel completo.

### `GET /api/db/novels/{id}/full`

Obtiene la novela **completa con todos sus capítulos** (útil para exportación offline).

```
GET /api/db/novels/novel-id-abc/full
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "novel": { ... },
  "chapters": [
    {
      "id": "chapter-id-1",
      "novelId": "novel-id-abc",
      "chapterOrder": 1,
      "title": "1.00 - Welcome to Inn",
      "translatedTitle": "1.00 - Bienvenido a la Posada",
      "originalContent": "Full chapter text...",
      "translatedContent": "Texto del capítulo traducido...",
      "refinedContent": "Texto refinado...",
      "status": "refined",
      "errorMessage": "",
      "createdAt": "...",
      "updatedAt": "..."
    }
  ]
}
```

### Auxiliares

#### `GET /api/db/novels/tags/suggestions`

Sugiere tags existentes.

```
GET /api/db/novels/tags/suggestions?q=fant&limit=10
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "items": ["fantasy", "fantastic", "fantasía"]
}
```

#### `GET /api/db/novels/series/suggestions`

Sugiere series existentes.

```
GET /api/db/novels/series/suggestions?q=wandering&limit=10
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "items": ["The Wandering Inn", "Wandering Inn"]
}
```

---

## 8. Capítulos

Todas bajo `/api/db/novels/{novelId}/...`. Requieren autenticación.

### `GET /api/db/novels/{novelId}/chapters`

Lista todos los capítulos de una novela (solo resumen, sin contenido).

```
GET /api/db/novels/novel-id-abc/chapters
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
[
  {
    "id": "chapter-id-1",
    "novelId": "novel-id-abc",
    "chapterOrder": 1,
    "title": "1.00 - Welcome to Inn",
    "translatedTitle": "1.00 - Bienvenido a la Posada",
    "status": "refined",
    "errorMessage": "",
    "hasOriginalContent": true,
    "hasTranslatedContent": true,
    "hasRefinedContent": true,
    "originalChars": 3500,
    "translatedChars": 3800,
    "refinedChars": 3700,
    "createdAt": "2024-01-10 10:00:00.000Z",
    "updatedAt": "2024-01-15 12:00:00.000Z"
  }
]
```

### `GET /api/db/novels/{novelId}/chapters/eligible`

Lista capítulos elegibles para una operación (traducir o refinar).

```
GET /api/db/novels/novel-id-abc/chapters/eligible?operation=translate
Authorization: Bearer <token>
```

**Query params:**
- `operation` — `translate` (default) o `refine`

**Response `200 OK`** — mismo formato que `/chapters`.

### `GET /api/db/novels/{novelId}/chapters/full`

Lista todos los capítulos **con contenido completo**.

```
GET /api/db/novels/novel-id-abc/chapters/full
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
[
  {
    "id": "chapter-id-1",
    "novelId": "novel-id-abc",
    "chapterOrder": 1,
    "title": "1.00 - Welcome to Inn",
    "translatedTitle": "1.00 - Bienvenido a la Posada",
    "originalContent": "Full chapter text...",
    "translatedContent": "Texto traducido...",
    "refinedContent": "Texto refinado...",
    "status": "refined",
    "errorMessage": "",
    "createdAt": "...",
    "updatedAt": "..."
  }
]
```

### `GET /api/db/novels/{novelId}/chapter-summaries`

Lista paginada de resúmenes de capítulos.

```
GET /api/db/novels/novel-id-abc/chapter-summaries?limit=50&offset=0
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "items": [ ... ],
  "total": 142,
  "limit": 50,
  "offset": 0
}
```

### `GET /api/db/novels/{novelId}/chapter-stats`

Estadísticas agregadas de capítulos.

```
GET /api/db/novels/novel-id-abc/chapter-stats
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "totalChapters": 142,
  "completedChapters": 30,
  "translatedChapters": 50,
  "originalCharacters": 2500000,
  "translatedCharacters": 500000,
  "refinedCharacters": 300000,
  "totalCharacters": 3000000,
  "maxChapterOrder": 142
}
```

### `GET /api/db/novels/{novelId}/chapters/{chapterId}`

Obtiene un capítulo específico con todo su contenido.

```
GET /api/db/novels/novel-id-abc/chapters/chapter-id-1
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "id": "chapter-id-1",
  "novelId": "novel-id-abc",
  "chapterOrder": 1,
  "title": "1.00 - Welcome to Inn",
  "translatedTitle": "1.00 - Bienvenido a la Posada",
  "originalContent": "Full chapter text...",
  "translatedContent": "Texto traducido...",
  "refinedContent": "Texto refinado...",
  "status": "refined",
  "errorMessage": "",
  "createdAt": "2024-01-10 10:00:00.000Z",
  "updatedAt": "2024-01-15 12:00:00.000Z"
}
```

### `POST /api/db/novels/{novelId}/chapters`

Crea o actualiza un capítulo (upsert — si el `chapterOrder` ya existe, se actualiza).

```
POST /api/db/novels/novel-id-abc/chapters
Authorization: Bearer <token>
Content-Type: application/json

{
  "chapterOrder": 1,
  "title": "1.00 - Welcome to Inn",
  "originalContent": "Full chapter text...",
  "status": "pending"
}
```

**Response `201 Created`** — chapter record completo.

### `DELETE /api/db/novels/{novelId}/chapters/{chapterId}`

Elimina un capítulo.

```
DELETE /api/db/novels/novel-id-abc/chapters/chapter-id-1
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "ok": true
}
```

### `POST /api/db/novels/{novelId}/chapters/bulk-delete`

Elimina múltiples capítulos a la vez.

```
POST /api/db/novels/novel-id-abc/chapters/bulk-delete
Authorization: Bearer <token>
Content-Type: application/json

{
  "ids": ["chapter-id-1", "chapter-id-2", "chapter-id-3"]
}
```

**Response `200 OK`**

```json
{
  "deleted": 3,
  "requested": 3
}
```

### `PATCH /api/db/novels/{novelId}/chapters/{chapterId}/status`

Actualiza el estado de un capítulo.

```
PATCH /api/db/novels/novel-id-abc/chapters/chapter-id-1/status
Authorization: Bearer <token>
Content-Type: application/json

{
  "status": "done",
  "errorMessage": ""
}
```

**Response `200 OK`** — chapter record completo.

### `GET /api/db/novels/{novelId}/chapters/gaps`

Encuentra gaps numéricos en la secuencia de capítulos (capítulos faltantes).

```
GET /api/db/novels/novel-id-abc/chapters/gaps
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "gaps": [
    { "from": 5, "to": 8, "count": 4 },
    { "from": 12, "to": 12, "count": 1 }
  ]
}
```

### Clean (Preview y Aplicar)

#### `POST /api/db/novels/{novelId}/chapters/clean-preview`

Vista previa de una operación de limpieza sobre un capítulo.

```
POST /api/db/novels/novel-id-abc/chapters/clean-preview
Authorization: Bearer <token>
Content-Type: application/json

{
  "chapterId": "chapter-id-1",
  "mode": "remove_duplicates",
  "applyTo": "original"
}
```

**Modes disponibles:** `remove_after`, `remove_duplicates`, `remove_line`, `remove_multiple_blanks`, `search_replace`.

**Campos adicionales según mode:**
- `searchText` / `replaceText` — para `search_replace`
- `caseSensitive` / `useRegex` — booleanos (opcional)

**applyTo:** `original`, `translated`, `refined`, `all`

**Response `200 OK`**

```json
{
  "chapterTitle": "1.00 - Welcome to Inn",
  "original": "Texto original...",
  "cleaned": "Texto limpio...",
  "changed": true,
  "removedLines": 5
}
```

#### `POST /api/db/novels/{novelId}/chapters/clean`

Aplica limpieza a múltiples capítulos.

```
POST /api/db/novels/novel-id-abc/chapters/clean
Authorization: Bearer <token>
Content-Type: application/json

{
  "chapterIds": ["chapter-id-1", "chapter-id-2"],
  "mode": "remove_duplicates",
  "applyTo": "original"
}
```

**Response `200 OK`**

```json
{
  "modified": 2,
  "total": 2,
  "skipped": 0,
  "notFound": 0,
  "failed": 0
}
```

---

## 9. Jobs

Los jobs son trabajos asíncronos (traducción, descarga, generación de glosario).

### `GET /api/db/translation-jobs/active/status`

Verifica si hay jobs activos.

```
GET /api/db/translation-jobs/active/status
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "hasActive": false
}
```

### `GET /api/db/translation-jobs/active`

Lista jobs activos del usuario.

```
GET /api/db/translation-jobs/active
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
[
  {
    "id": "job-id-abc",
    "novelId": "novel-id-abc",
    "status": "running",
    "operation": "translate",
    "provider": "venice",
    "model": "deepseek-v4-flash",
    "totalChapters": 10,
    "completedChapters": 4,
    "failedChapters": 1,
    "errorMessage": "",
    "createdAt": "2024-01-15 12:00:00.000Z",
    "updatedAt": "2024-01-15 12:05:00.000Z",
    "novelTitle": "The Wandering Inn",
    "chapterIds": ["ch-1", "ch-2", ...],
    "autoSegmentEnabled": false,
    "autoSegmentActive": false,
    "autoSegmentCount": 0,
    "autoSegmentCurrentIndex": 0,
    "autoSegmentCompletedCount": 0,
    "autoSegmentChapterId": "",
    "autoSegmentChapterTitle": "",
    "newChapters": 0
  }
]
```

### `GET /api/db/novels/{novelId}/translation-jobs`

Lista jobs de una novela.

```
GET /api/db/novels/novel-id-abc/translation-jobs?failedOnly=1
Authorization: Bearer <token>
```

**Response `200 OK`** — array de job records (misma estructura que arriba).

### `POST /api/db/novels/{novelId}/translation-jobs`

Crea un nuevo job de traducción/refinamiento.

```
POST /api/db/novels/novel-id-abc/translation-jobs
Authorization: Bearer <token>
Content-Type: application/json

{
  "chapterIds": ["chapter-id-1", "chapter-id-2"],
  "operation": "translate",
  "options": {
    "provider": "venice",
    "model": "deepseek-v4-flash"
  }
}
```

**operation:** `translate`, `refine`, `download`, `check`, `generate-glossary`

**Response `201 Created`** — job record.

### `PATCH /api/db/translation-jobs/{jobId}`

Actualiza un job (cancelar, reanudar).

```
PATCH /api/db/translation-jobs/job-id-abc
Authorization: Bearer <token>
Content-Type: application/json

{
  "status": "cancelled"
}
```

**Status:** `pending`, `running`, `done`, `failed`, `cancelled`.  
Si se setea a `pending`, se reencola. Si se setea a `cancelled`, se cancela y se reconcilian los capítulos.

**Response `200 OK`** — job record actualizado.

---

## 10. Importación

### EPUB

#### `POST /api/db/novels/import-epub`

Importa una novela desde un archivo EPUB.

```
POST /api/db/novels/import-epub
Authorization: Bearer <token>
Content-Type: multipart/form-data

campos:
  file: (archivo .epub)
  sourceLanguage: "en" (opcional, se detecta del EPUB si no se envía)
  targetLanguage: "es"
```

**Response `201 Created`**

```json
{
  "novel": { ... },
  "epub": {
    "id": "epub-id",
    "novelId": "novel-id-abc",
    "fileKind": "original",
    "sourceVariant": "raw",
    "label": "...",
    "fileName": "novel.epub",
    "url": "",
    "createdAt": "...",
    "updatedAt": "..."
  },
  "chaptersImported": 50
}
```

#### `POST /api/epubs/preview`

Previsualiza el contenido de un EPUB sin importarlo.

```
POST /api/epubs/preview
Authorization: Bearer <token>
Content-Type: multipart/form-data

campo: file (archivo .epub)
```

**Response `200 OK`**

```json
{
  "title": "The Wandering Inn",
  "author": "pirateaba",
  "description": "...",
  "language": "en",
  "series": "The Wandering Inn",
  "number": "1",
  "chapters": [
    { "title": "1.00 - Welcome to Inn", "content": "..." }
  ]
}
```

### ZIP

#### `POST /api/db/novels/import-from-zip`

Importa desde un archivo ZIP con estructura `originals/` y opcionalmente `translated/` y `metadata.json`.

```
POST /api/db/novels/import-from-zip
Authorization: Bearer <token>
Content-Type: multipart/form-data

campo: file (archivo .zip)
```

**Estructura del ZIP:**
```
novel.zip
├── metadata.json        { sourceTitle, sourceAuthor, sourceLanguage, targetLanguage, ... }
├── cover.jpg            (opcional)
├── originals/
│   ├── capitulo-01.md
│   ├── capitulo-02.md
│   └── ...
└── translated/
    ├── capitulo-01.md   (opcional)
    └── ...
```

**Response `201 Created`**

```json
{
  "novel": { ... },
  "chaptersImported": 50
}
```

### URL

#### `POST /api/db/novels/preview-from-url`

Previsualiza información de una novela desde su URL (scrapea el sitio).

```
POST /api/db/novels/preview-from-url
Authorization: Bearer <token>
Content-Type: application/json

{
  "url": "https://wanderinginn.com/"
}
```

**Response `200 OK`**

```json
{
  "title": "The Wandering Inn",
  "author": "pirateaba",
  "description": "An open world fantasy...",
  "coverURL": "https://wanderinginn.com/cover.jpg",
  "totalChapters": 142,
  "sourceURL": "https://wanderinginn.com/"
}
```

#### `POST /api/db/novels/import-from-url`

Importa una novela desde URL. Descarga el primer capítulo y encola el resto como job de descarga.

```
POST /api/db/novels/import-from-url
Authorization: Bearer <token>
Content-Type: application/json

{
  "url": "https://wanderinginn.com/",
  "sourceLanguage": "en",
  "targetLanguage": "es",
  "startChapter": 1,
  "endChapter": 142
}
```

**Response `201 Created`**

```json
{
  "novel": { ... },
  "chaptersImported": 1,
  "totalChapters": 142,
  "downloadJob": {
    "id": "job-id-abc",
    "totalChapters": 141
  }
}
```

### Actualización desde URL

#### `GET /api/db/novels/{id}/update-preview`

Verifica si hay capítulos nuevos en la URL fuente de una novela.

```
GET /api/db/novels/novel-id-abc/update-preview
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "title": "The Wandering Inn",
  "author": "pirateaba",
  "description": "...",
  "coverURL": "...",
  "sourceURL": "https://wanderinginn.com/",
  "currentChapters": 100,
  "totalChapters": 142,
  "newChapters": 42,
  "firstNewChapter": 101,
  "lastNewChapter": 142
}
```

#### `POST /api/db/novels/{id}/update-from-url`

Descarga capítulos nuevos desde la URL fuente.

```
POST /api/db/novels/novel-id-abc/update-from-url
Authorization: Bearer <token>
Content-Type: application/json

{
  "startChapter": 101,
  "endChapter": 142
}
```

**Response `200 OK`**

```json
{
  "chaptersAdded": 0,
  "chapters": [],
  "totalChapters": 142,
  "pendingChapters": 42,
  "downloadJobId": "job-id-xyz",
  "message": "Descarga iniciada. 42 capítulos se están descargando en segundo plano."
}
```

### Batch Operations

#### `GET /api/db/novels/check-batch-updates`

Verifica actualizaciones para múltiples novelas con URL.

```
GET /api/db/novels/check-batch-updates
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "results": [
    {
      "novelId": "novel-id-abc",
      "sourceTitle": "The Wandering Inn",
      "sourceAuthor": "pirateaba",
      "coverUrl": "https://...",
      "newChapters": 42,
      "firstNewChapter": 101,
      "lastNewChapter": 142,
      "startOrder": 101,
      "currentChapters": 100,
      "totalChapters": 142,
      "newChapterInfo": [
        { "url": "https://...", "title": "5.00 - New Chapter", "order": 101 }
      ],
      "error": ""
    }
  ],
  "checked": 5,
  "withUpdates": 2,
  "errors": 0
}
```

#### `POST /api/db/novels/batch-update-from-url`

Inicia descargas batch para múltiples novelas.

```
POST /api/db/novels/batch-update-from-url
Authorization: Bearer <token>
Content-Type: application/json

{
  "selections": [
    {
      "novelId": "novel-id-abc",
      "startOrder": 101,
      "startChapter": 101,
      "endChapter": 142,
      "newChapterInfo": [ ... ]
    }
  ]
}
```

**Response `200 OK`**

```json
{
  "jobs": [
    { "novelId": "novel-id-abc", "jobId": "job-id-xyz", "pendingChapters": 42 }
  ],
  "totalPending": 42
}
```

#### `GET /api/db/novels/batch-translate-preview`

Previsualiza qué novelas tienen capítulos pendientes de traducir.

```
GET /api/db/novels/batch-translate-preview
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "results": [
    {
      "novelId": "novel-id-abc",
      "sourceTitle": "The Wandering Inn",
      "sourceAuthor": "pirateaba",
      "coverUrl": "/api/files/novels/novel-id-abc/cover.jpg",
      "pendingChapters": 50,
      "totalChapters": 142,
      "translatedCount": 92,
      "completedCount": 30,
      "hasOriginalContent": true
    }
  ],
  "totalNovels": 1,
  "withPending": 1
}
```

#### `POST /api/db/novels/batch-translate`

Inicia traducción batch para múltiples novelas.

```
POST /api/db/novels/batch-translate
Authorization: Bearer <token>
Content-Type: application/json

{
  "selections": [
    {
      "novelId": "novel-id-abc",
      "chapterIds": ["ch-1", "ch-2"]     // opcional: si se omite, se traducen todos los pendientes
    }
  ]
}
```

**Response `200 OK`**

```json
{
  "jobs": [
    { "novelId": "novel-id-abc", "jobId": "job-id-xyz", "pendingChapters": 50 }
  ],
  "totalPending": 50
}
```

#### `POST /api/db/novels/batch-check`

Encola jobs de verificación de actualizaciones.

```
POST /api/db/novels/batch-check
Authorization: Bearer <token>
Content-Type: application/json

{
  "novelIds": ["novel-id-abc", "novel-id-def"]
}
```

**Response `200 OK`**

```json
{
  "jobs": [
    { "novelId": "novel-id-abc", "jobId": "job-1" },
    { "novelId": "novel-id-def", "jobId": "job-2" }
  ]
}
```

---

## 11. EPUBs

### `GET /api/epubs`

Lista archivos EPUB asociados al usuario.

```
GET /api/epubs?novelId=novel-id-abc
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
[
  {
    "id": "epub-id",
    "novelId": "novel-id-abc",
    "fileKind": "original",
    "sourceVariant": "raw",
    "label": "source=original",
    "fileName": "novel.epub",
    "url": "",
    "createdAt": "...",
    "updatedAt": "..."
  }
]
```

### `POST /api/epubs`

Sube un archivo EPUB asociado a una novela.

```
POST /api/epubs
Authorization: Bearer <token>
Content-Type: multipart/form-data

campos:
  novelId: "novel-id-abc"
  fileKind: "original" | "translated"
  sourceVariant: "raw" | "refined" | "translated"
  label: "source=original"
  file: (archivo .epub)
```

**Response `201 Created`** — epub record.

### `GET /api/epubs/{id}/download`

Descarga un archivo EPUB.

```
GET /api/epubs/epub-id/download
Authorization: Bearer <token>
```

**Response `200`** — stream del archivo EPUB (application/epub+zip).  
Cache-Control: `no-store` (los EPUBs se regeneran in-place).

---

## 12. Exportación EPUB

### `POST /api/epubs/build`

Genera un EPUB a partir de los capítulos de una novela (lado servidor).

```
POST /api/epubs/build
Authorization: Bearer <token>
Content-Type: application/json

{
  "novelId": "novel-id-abc",
  "source": "translated"
}
```

**source:** `original`, `translated`, `refined`

**Response `201 Created`** — epub record.

```json
{
  "id": "epub-id",
  "novelId": "novel-id-abc",
  "fileKind": "translated",
  "sourceVariant": "translated",
  "label": "source=translated",
  "fileName": "The-Wandering-Inn.epub",
  "url": "",
  "createdAt": "...",
  "updatedAt": "..."
}
```

Luego se descarga via `GET /api/epubs/{id}/download`.

---

## 13. Reading Progress

### `GET /api/user/novels/{novelId}/reading-progress`

Obtiene el progreso de lectura de una novela.

```
GET /api/user/novels/novel-id-abc/reading-progress
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "id": "progress-id",
  "userId": "user-id-123",
  "novelId": "novel-id-abc",
  "chapterId": "chapter-id-5",
  "scrollPercent": 75.5,
  "createdAt": "...",
  "updatedAt": "..."
}
```

Si no hay progreso registrado: `{}` (objeto vacío).

### `PUT /api/user/novels/{novelId}/reading-progress`

Guarda el progreso de lectura.

```
PUT /api/user/novels/novel-id-abc/reading-progress
Authorization: Bearer <token>
Content-Type: application/json

{
  "chapterId": "chapter-id-5",
  "scrollPercent": 75.5
}
```

**Response `200 OK`** — reading progress record.

---

## 14. Glosario

### `POST /api/db/novels/{novelId}/generate-glossary`

Encola un job de generación de glosario para un rango de capítulos.

```
POST /api/db/novels/novel-id-abc/generate-glossary
Authorization: Bearer <token>
Content-Type: application/json

{
  "chapterFrom": 1,
  "chapterTo": 50,
  "mode": "together",
  "maxTokensPerBatch": 8000,
  "provider": "venice",
  "model": "deepseek-v4-flash"
}
```

**mode:** `together` (default) — todos los capítulos juntos, `batch` — por lotes.

**Response `200 OK`**

```json
{
  "jobId": "job-id-abc",
  "status": "pending",
  "operation": "generate-glossary"
}
```

### `GET /api/db/novels/{novelId}/estimate-glossary-tokens`

Estima los tokens necesarios para generar glosario en un rango.

```
GET /api/db/novels/novel-id-abc/estimate-glossary-tokens?from=1&to=50
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "totalTokens": 45000,
  "chapterCount": 50
}
```

---

## 15. Browser Worker / Proxy

### `GET /api/browser-workers`

Lista los browser workers conectados del usuario autenticado.

```
GET /api/browser-workers
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "count": 1,
  "workers": [
    {
      "id": "worker-uuid",
      "browser": "Chrome",
      "version": "120.0.0",
      "state": "idle",
      "capabilities": ["fetch_page", "get_html"],
      "connectedAt": "2024-01-15T12:00:00Z",
      "lastHeartbeat": "2024-01-15T12:05:00Z"
    }
  ]
}
```

### `GET /api/proxy/status`

Estado del proxy del browser worker.

```
GET /api/proxy/status
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "connected": true,
  "count": 1,
  "workers": [ ... ]
}
```

### `POST /api/proxy/fetch`

Solicita a un browser worker que descargue el HTML de una URL (útil para sitios protegidos por Cloudflare).

```
POST /api/proxy/fetch
Authorization: Bearer <token>
Content-Type: application/json

{
  "url": "https://example.com/novel/chapter-1",
  "timeout": 120
}
```

**Response `200 OK`**

```json
{
  "url": "https://example.com/novel/chapter-1",
  "title": "Chapter 1",
  "html": "<html>...page content...</html>",
  "text": "Page text content...",
  "status": "ok"
}
```

### `GET /ws/browser-worker`

WebSocket para la conexión del browser worker (Chrome extension). **No requiere auth HTTP** — la autenticación es in-band mediante mensaje `register`.

```
ws://<host>:5176/ws/browser-worker
```

---

## 16. Worker Auth

Endpoints para el flujo de autorización de Chrome Extensions como browser workers.

### `GET /api/worker-auth/authorize`

Inicia el flujo de autorización (redirige a página de consentimiento si hay sesión).

```
GET /api/worker-auth/authorize?extension_id=abc123def456
```

**Response `200`** — HTML page (consentimiento o login requerido).

### `GET /api/worker-auth/validate`

Valida un token de worker.

```
GET /api/worker-auth/validate?token=worker-token
```

**Response `200 OK`**

```json
{
  "valid": true,
  "userId": "user-id-123",
  "extensionId": "abc123def456",
  "label": "Chrome Extension (abc123de)"
}
```

### `POST /api/worker-auth/approve` (protegido)

Aprueba la autorización de una extensión. **Requiere autenticación.**

```
POST /api/worker-auth/approve
Authorization: Bearer <token>
Content-Type: application/x-www-form-urlencoded

state=state-string-from-authorize
```

**Response `200`** — HTML success page.

### `POST /api/worker-auth/revoke/{id}` (protegido)

Revoca un token de worker.

```
POST /api/worker-auth/revoke/token-id
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "ok": true
}
```

### `POST /api/worker-auth/delete/{id}` (protegido)

Elimina permanentemente un token de worker.

```
POST /api/worker-auth/delete/token-id
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "ok": true
}
```

### `GET /api/worker-auth/tokens` (protegido)

Lista tokens de worker del usuario.

```
GET /api/worker-auth/tokens
Authorization: Bearer <token>
```

**Response `200 OK`**

```json
{
  "tokens": [ ... ],
  "count": 1
}
```

### `GET /api/worker-auth/callback`

Callback de autorización exitosa para la extensión.

```
GET /api/worker-auth/callback?token=worker-token&user=user-id
```

**Response `200`** — HTML page que la extensión puede parsear para extraer el token.

---

## 17. Backup

### `GET /api/backup/download`

Descarga un backup ZIP completo del directorio `data/`.

```
GET /api/backup/download
Authorization: Bearer <token>
```

**Response `200`** — archivo ZIP con nombre `backup-YYYYMMDD-HHMMSS.zip`.

El backup incluye la base de datos SQLite de PocketBase y todos los archivos subidos (covers, epubs, etc.).

---

## Notas Adicionales

### Sobre Covers

El campo `coverPath` en los objetos novel usa el formato:

```
/api/files/{collection}/{recordId}/{filename}
```

Ejemplo completo:
```
http://192.168.1.105:5176/api/files/novels/novel-id-abc/cover_filename.jpg
```

PocketBase sirve estos archivos directamente sin necesidad de un endpoint adicional. El servidor usa CGO-disabled, por lo que las imágenes se sirven vía el filesystem interno de PocketBase.

Si la novela tiene un thumbnail generado, `coverPath` apunta al thumbnail en lugar de la imagen original. El campo `thumbnailFile` / `thumbnailPath` en el modelo Novel permite acceso directo al thumbnail si es necesario.

### Estados de Capítulo

| Status | Significado |
|---|---|
| `pending` | Capítulo nuevo, sin traducir (importado o descargado) |
| `processing` | Marcado para procesar por un job encolado o en ejecución |
| `translated` | Traducido (elegible para refinar) |
| `refined` | Refinado |
| `done` | Completado manualmente (estado terminal) |
| `failed` | Error en la operación |

### Estados de Job

| Status | Significado |
|---|---|
| `pending` | En cola para ejecutar |
| `running` | En ejecución |
| `done` | Finalizado exitosamente |
| `failed` | Falló |
| `cancelled` | Cancelado por el usuario |

### Operaciones de Job

| Operation | Descripción |
|---|---|
| `translate` | Traducir capítulos |
| `refine` | Refinar traducciones |
| `download` | Descargar capítulos desde URL |
| `check` | Verificar actualizaciones |
| `generate-glossary` | Generar glosario automático |

### Autenticación

- Los tokens JWT se obtienen via `POST /api/auth/login` o `POST /api/auth/register`.
- El token se envía en el header `Authorization: Bearer <token>`.
- Opcionalmente, el token también puede enviarse como cookie `auth.token` (HttpOnly, Secure, SameSite=Strict).
- El endpoint `POST /api/auth/refresh` permite renovar el token antes de que expire.
- Los tokens de PocketBase son válidos por 30 días por defecto.

### Content-Type

Todas las respuestas de API son `application/json` excepto:
- `GET /api/epubs/{id}/download` → `application/epub+zip`
- `GET /api/backup/download` → `application/zip`
- Endpoints de worker auth → `text/html`
- Static files (SPA) → según extensión