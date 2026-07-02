# Plan: Export/Import Per-Usuario

## Contexto

La aplicación es multi-usuario con aislamiento estricto de datos (cada usuario tiene `ownerId` en sus novelas). PocketBase's sistema de backup estándar hace backup de TODO el `pb_data`, lo que violaría la privacidad entre usuarios y permitiría restauraciones destructivas. No existe un concepto de admin/superuser en la app.

**Objetivo**: Permitir que cada usuario exporte e importe sus propios datos (novelas, capítulos, configuración, prompts, progreso de lectura) de forma segura y portable.

---

## Arquitectura

### Formato del ZIP de Exportación

```
user-export-YYYYMMDD-HHMMSS.zip
├── manifest.json              ← Metadatos del export
├── settings.json              ← Configuración del usuario (AI, theme)
├── prompts.json               ← Prompts personalizados del usuario
├── novels/
│   ├── mi-novela-1/
│   │   ├── metadata.json      ← Datos de la novela (serializados)
│   │   ├── chapters/
│   │   │   ├── 001-original.md
│   │   │   ├── 001-translated.md
│   │   │   ├── 001-refined.md
│   │   │   ├── 002-original.md
│   │   │   └── ...
│   │   └── cover.jpg          ← Portada (si existe)
│   └── mi-novela-2/
│       └── ...
└── reading-progress.json      ← Progreso de lectura por novela
```

### Estructura de `manifest.json`

```json
{
  "version": 1,
  "exportedAt": "2025-01-15T10:30:00Z",
  "exportedBy": "user@example.com",
  "novelCount": 5,
  "totalChapters": 320,
  "sourceApp": "translator-server"
}
```

### Estructura de `metadata.json` (por novela)

```json
{
  "sourceTitle": "Título Original",
  "sourceAuthor": "Autor",
  "sourceDescription": "Descripción",
  "sourceLanguage": "en",
  "targetLanguage": "es",
  "sourceSeries": "",
  "sourceNumber": "",
  "targetTitle": "Título Traducido",
  "targetAuthor": "",
  "targetDescription": "",
  "tags": ["fantasy", "completed"],
  "status": "completed",
  "notes": "",
  "glossary": [],
  "customCommands": "",
  "aiOptions": {
    "provider": "venice",
    "model": "llama-3.3-70b"
  },
  "translationOptions": {
    "autoSegment": true,
    "thresholdChars": 8000
  },
  "cleanupRules": []
}
```

### Estructura de `settings.json`

```json
{
  "theme": "dark",
  "ai": {
    "provider": "venice",
    "model": "llama-3.3-70b",
    "baseUrl": "https://api.venice.ai/api/v1",
    "timeoutMs": 120000
  },
  "translation": {
    "autoSegment": true,
    "thresholdChars": 8000,
    "maxChars": 4000,
    "minChars": 1000,
    "maxRetries": 2,
    "concurrency": 1,
    "enableCheck": false,
    "includePreviousChapterTitles": true
  }
}
```

**Nota**: NO se exportan API keys por seguridad. Solo se exporta la configuración de modelo/base URL.

---

## Cambios de Backend

### Archivo Nuevo: `internal/api/router_export.go`

Endpoints:

| Método | Ruta | Descripción |
|--------|------|-------------|
| `GET` | `/api/user/export/preview` | Retorna resumen de lo que se exportaría (count de novelas, capítulos, tamaño estimado) |
| `POST` | `/api/user/export` | Genera y descarga el ZIP con todos los datos del usuario |
| `POST` | `/api/user/import` | Recibe un ZIP de export y crea las novelas + configuración |

#### `GET /api/user/export/preview`

Response:
```json
{
  "novelCount": 5,
  "totalChapters": 320,
  "hasSettings": true,
  "hasPrompts": true,
  "hasReadingProgress": true,
  "estimatedSizeBytes": 1048576
}
```

#### `POST /api/user/export`

- Carga todas las novelas del usuario via `s.Store.ListOwnedNovels(userID)`
- Para cada novela, carga sus capítulos via `s.Store.ListChapters(userID, novelID)`
- Serializa metadata, capítulos (original/translated/refined), cover
- Carga settings del usuario via `s.Store.GetSettings(userID)`
- Carga prompts del usuario via `s.Store.ListPrompts(userID)`
- Carga reading progress via query sobre `reading_progress` collection
- Genera el ZIP en streaming y lo retorna como `application/zip`
- Nombre del archivo: `user-export-YYYYMMDD-HHMMSS.zip`

#### `POST /api/user/import`

- Acepta multipart con archivo `.zip`
- Parsea el ZIP y valida `manifest.json`
- Para cada novela en el ZIP:
  - Verifica que no exista una novela con el mismo título (skip si existe, reportar en respuesta)
  - Crea la novela via `s.Store.CreateNovel()` con `ownerId = e.Auth.Id`
  - Crea capítulos via `s.Store.UpsertChapter()` 
  - Adjunta cover si existe via `s.Store.AttachCoverBlob()`
- Aplica settings si `settings.json` existe
- Crea prompts si `prompts.json` existe
- Aplica reading progress si `reading-progress.json` existe
- Response:
```json
{
  "imported": 3,
  "skipped": 2,
  "skippedReasons": [
    { "title": "Novela Existente", "reason": "already_exists" }
  ]
}
```

### Archivo Modificado: `internal/api/router.go`

- Añadir `registerExportRoutes(api, s)` en `registerProtectedRoutes`

### Métodos Necesarios en Store (`internal/store/`)

Verificar qué métodos existen y cuáles hay que crear:

| Método | ¿Existe? | Notas |
|--------|----------|-------|
| `ListOwnedNovels(userID)` | Sí | Retorna todas las novelas del usuario |
| `ListChapters(userID, novelID)` | Sí (`ListChaptersFull`) | Retorna capítulos con contenido |
| `GetSettings(userID)` | Sí | Retorna configuración del usuario |
| `ListPrompts(userID)` | Sí | Retorna prompts personalizados |
| `GetOwnedNovel(userID, novelID)` | Sí | Verifica ownership |
| `CreateNovel(userID, input)` | Sí | Crea novela con owner |
| `UpsertChapter(userID, novelID, ch)` | Sí | Crea/actualiza capítulo |
| `AttachCoverBlob(novelID, blob, mime)` | Sí | Adjunta portada |
| `ImportZipNovel(input)` | Sí | Ya existe para import de ZIP de novelas individuales |
| Reading progress query | **No explícito** | Necesita query manual o método nuevo |

**Reading progress**: Se puede obtener con `s.Store.App.FindRecordsByFilter("reading_progress", "user = {:userID}", userID)` ya que el campo `user` en `reading_progress` apunta al usuario.

---

## Cambios de Frontend

### Archivo Nuevo: `frontend/src/api/types.ts` (modificación)

```typescript
export interface ExportPreview {
  novelCount: number;
  totalChapters: number;
  hasSettings: boolean;
  hasPrompts: boolean;
  hasReadingProgress: boolean;
  estimatedSizeBytes: number;
}

export interface ImportResult {
  imported: number;
  skipped: number;
  skippedReasons: Array<{ title: string; reason: string }>;
}
```

### Archivo Modificado: `frontend/src/api/client.ts`

Añadir sección `export`:

```typescript
export: {
  async preview(): Promise<ExportPreview> {
    return http.get<ExportPreview>("/api/user/export/preview");
  },
  async download(): Promise<Blob> {
    // Fetch directo con Bearer token para descargar como blob
    const response = await fetch(`${getApiBaseUrl()}/api/user/export`, {
      headers: { Authorization: `Bearer ${authState.token.value}` },
    });
    if (!response.ok) throw new Error("Export failed");
    return response.blob();
  },
  async import(file: File): Promise<ImportResult> {
    const form = new FormData();
    form.set("file", file);
    return http.post<ImportResult>("/api/user/import", form);
  },
},
```

### Archivo Nuevo: `frontend/src/pages/ExportPage.vue`

Página dedicada con dos secciones:

#### Sección 1: Exportar Datos
- Muestra preview al cargar (número de novelas, capítulos, tamaño estimado)
- Botón "Descargar export" que genera y descarga el ZIP
- Indicador de progreso durante la generación

#### Sección 2: Importar Datos
- FileUpload de PrimeVue aceptando `.zip`
- Botón "Importar" después de seleccionar archivo
- Resultado: novelas importadas, saltadas, razones

#### Diseño de UI

```
┌─────────────────────────────────────────────────┐
│ Exportar / Importar Datos                       │
│ Gestiona copias de seguridad de tus novelas     │
├─────────────────────────────────────────────────┤
│                                                 │
│  📦 Exportar                                    │
│  ┌───────────────────────────────────────────┐  │
│  │ 5 novelas · 320 capítulos · ~1.2 MB      │  │
│  │ Incluye: configuración, prompts, progreso │  │
│  │                                           │  │
│  │ [Descargar export]                        │  │
│  └───────────────────────────────────────────┘  │
│                                                 │
│  📥 Importar                                    │
│  ┌───────────────────────────────────────────┐  │
│  │ [Seleccionar archivo .zip]                │  │
│  │                                           │  │
│  │ Resultado:                                │  │
│  │ ✓ 3 novelas importadas                    │  │
│  │ ⚠ 2 novelas saltadas (ya existen)        │  │
│  └───────────────────────────────────────────┘  │
│                                                 │
└─────────────────────────────────────────────────┘
```

### Archivo Modificado: `frontend/src/router/index.ts`

Añadir ruta:
```typescript
{
  path: "/export",
  name: "export",
  component: ExportPage,
  meta: { requiresAuth: true },
},
```

### Archivo Modificado: `frontend/src/components/AppLayout.vue`

Añadir enlace en menú de usuario y menú móvil:
```typescript
{
  label: "Exportar / Importar",
  icon: "pi pi-database",
  command: () => router.push("/export"),
},
```

---

## Orden de Implementación

1. **`internal/api/router_export.go`** — Core backend: preview, export, import endpoints
2. **`internal/api/router.go`** — Wire `registerExportRoutes`
3. **`frontend/src/api/types.ts`** — Tipos `ExportPreview`, `ImportResult`
4. **`frontend/src/api/client.ts`** — Métodos `export.preview()`, `export.download()`, `export.import()`
5. **`frontend/src/pages/ExportPage.vue`** — UI completa de export/import
6. **`frontend/src/router/index.ts`** — Ruta `/export`
7. **`frontend/src/components/AppLayout.vue`** — Navegación

## Verificación

- [ ] `go build ./cmd/server` compila sin errores
- [ ] `go test ./...` pasa
- [ ] `npm run build` en `frontend/` pasa (typecheck)
- [ ] Export genera ZIP con estructura correcta
- [ ] Import crea novelas con owner correcto
- [ ] Import respeta novelas existentes (skip)
- [ ] Export solo incluye datos del usuario actual
- [ ] UI muestra preview correcto antes de exportar
- [ ] UI muestra resultado después de importar
- [ ] Navegación funciona desde menú

## Riesgos

| Riesgo | Impacto | Mitigación |
|--------|---------|------------|
| ZIPs muy grandes agotan memoria | Medio | Streaming en generación de ZIP; límite de tamaño en upload |
| Import parcial falla | Medio | Transacción por novela; reportar éxitos/errores individualmente |
| Formato ZIP incompatible entre versiones | Bajo | Campo `version` en manifest; validación al importar |
| Reading progress no tiene colección dedicada | Bajo | Query directa via PocketBase API |
