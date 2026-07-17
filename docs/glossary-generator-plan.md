# Plan: Generador de Glosario con IA

## Overview

Añade un botón "Generar glosario" en el tab de Glosario de `ProjectSettingsDialog.vue` que envía capítulos (por rango) al LLM para extraer automáticamente términos del glosario. Soporta dos modos de envío (todo junto o por lotes), prompt editable como los demás tipos, y fusión incremental con el glosario existente.

**UI**: Card en la parte superior del tab de Glosario con los controles de generación; debajo, la lista de entradas del glosario existente.

## Requirements

- Card superior en el tab de Glosario con botón "Generar glosario" y configuración
- Selector de rango de capítulos (desde/hasta)
- Opción de envío: todo junto o por lotes (con estimación de tokens via `pandodao/tokenizer-go`)
- Límite por defecto de 90K tokens por lote
- Prompt editable (nuevo tipo `glossary` en el sistema de prompts existente)
- Fusión inteligente: entradas nuevas se añaden, duplicados se actualizan, manuales se conservan
- Spinner de progreso durante la generación
- Nuevo tipo de job: `generate-glossary`

## Architecture Changes

| Archivo | Cambio |
|---|---|
| `internal/ai/provider.go` | Nuevo método `GenerateGlossary(ctx, input) ([]GlossaryEntry, error)` en la interfaz |
| `internal/ai/openai.go` | Implementación del nuevo método con structured output |
| `internal/ai/google.go` | Implementación del nuevo método |
| `internal/api/router_glossary.go` | **Nuevo archivo**: handler `POST /api/db/novels/{novelId}/generate-glossary` |
| `internal/api/runtime_glossary.go` | **Nuevo archivo**: lógica de procesamiento del job |
| `internal/api/runtime_worker.go` | Añadir case `generate-glossary` al switch de `processJob()` |
| `internal/api/runtime_types.go` | Añadir campo `GlossaryGenerationConfig` al `jobContext` |
| `internal/store/store_jobs.go` | Añadir `"generate-glossary"` a los valores de `operation` |
| `internal/store/store_schema.go` | Añadir `"generate-glossary"` al select de `operation` en jobs |
| `internal/store/prompt_defaults.go` | Nuevo `DefaultGlossaryPrompt` con plantilla |
| `internal/store/store.go` | Sembrar prompt de glosario en `EnsureSchema` |
| `internal/api/router_prompts.go` | Añadir tipo `glossary` a los prompts soportados |
| `frontend/src/features/projects/ProjectSettingsDialog.vue` | Card superior con controles + lista de glosario debajo |
| `frontend/src/domain/project-settings.ts` | Tipo `GlossaryGenerationOptions` |
| `frontend/src/client.ts` | Método `api.novels.generateGlossary()` |
| `go.mod` | Añadir `github.com/pandodao/tokenizer-go` |

## Implementation Steps

### Phase 1: Backend — Provider Interface & Tokenizer

1. **Añadir `GenerateGlossary` al Provider interface** (File: `internal/ai/provider.go`)
   - Action: Añadir tipo de input `GenerateGlossaryInput` y método a la interfaz
   - Why: Define el contrato para generar glosarios
   - Dependencies: None
   - Risk: Bajo — es solo añadir a interfaz
   ```go
   type GenerateGlossaryInput struct {
       Texts         []string // textos de capítulos (uno por capítulo o lote)
       SourceLang    string
       TargetLang    string
       ExistingTerms []string // términos source ya existentes (para evitar duplicados)
       BatchInfo     string   // ej: "Lote 1 de 3" para context awareness
   }

   type GlossaryEntry struct {
       Source  string `json:"source"`
       Target  string `json:"target"`
       Context string `json:"context,omitempty"`
   }
   ```

2. **Implementar en OpenAIProvider** (File: `internal/ai/openai.go`)
   - Action: Implementar `GenerateGlossary` usando `goai.GenerateObject[[]GlossaryEntry]` con el prompt configurado
   - Why: OpenAI y compatibles soportan structured output
   - Dependencies: Step 1
   - Risk: Medio — requiere validar que el output del modelo sea JSON válido

3. **Implementar en GoogleProvider** (File: `internal/ai/google.go`)
   - Action: Implementar `GenerateGlossary` con el mismo patrón
   - Dependencies: Step 1
   - Risk: Bajo

4. **Añadir `tokenizer-go` dependency** (File: `go.mod`)
   - Action: `go get github.com/pandodao/tokenizer-go`
   - Why: Estimar tokens para decidir si enviar todo junto o por lotes
   - Dependencies: None
   - Risk: Bajo — library ligera

5. **Crear helper de estimación de tokens** (File: `internal/api/runtime_glossary.go`)
   - Action: Función `estimateTokens(text string) int` que usa el tokenizer
   - Why: Necesario para decidir batching
   - Dependencies: Step 4
   - Risk: Bajo

### Phase 2: Backend — Job System & Processing

6. **Añadir operación al schema** (File: `internal/store/store_schema.go`)
   - Action: Añadir `"generate-glossary"` al array de valores del campo `operation` en la colección `jobs`
   - Why: PocketBase valida los valores del select field
   - Dependencies: None
   - Risk: Bajo

7. **Añadir prompt por defecto** (File: `internal/store/prompt_defaults.go`)
   - Action: Crear `DefaultGlossaryPrompt` como constante string
   - Prompt template:
   ```
   # Translation Glossary Extraction Assistant
   
   ## Objective
   
   Analyze the provided content and extract a structured translation glossary that will be used as the canonical terminology reference for translating the work consistently.
   
   The glossary must be based **only** on the provided content.
   
   ---
   
   ## Languages
   
   The source and target languages are provided through the following tags:
   
   <source_language>
   {{SOURCE_LANGUAGE}}
   </source_language>
   
   <target_language>
   {{TARGET_LANGUAGE}}
   </target_language>
   
   Translate every extracted term from the source language into the target language.
   
   If transliteration is more appropriate than translation, use transliteration.
   
   ---
   
   ## Existing Glossary
   
   {{EXISTING_TERMS_INSTRUCTION}}
   
   If an existing glossary is provided:
   
   - Always preserve existing approved translations.
   - Do not generate alternative translations for existing terms.
   - Only extract new terms that are not already present.
   - Maintain complete terminology consistency.
   
   ---
   
   # Extraction Rules
   
   Extract up to **40** relevant terms.
   
   If only one chapter is provided, extract every relevant term.
   
   If multiple chapters are provided, prioritize recurring and narratively important terminology.
   
   ---
   
   ## Include
   
   Extract only terminology that is important for future translation consistency, including:
   
   - Cultivation techniques
   - Martial arts
   - Skills
   - Spells
   - Powers
   - Unique world concepts
   - Energy systems
   - Magical objects
   - Artifacts
   - Special weapons
   - Titles
   - Ranks
   - Organizations
   - Factions
   - Races
   - Spiritual beasts
   - Rare materials
   - Cultivation resources
   - Unique professions
   - Philosophical concepts unique to the fictional world
   - Any recurring fictional terminology
   
   ---
   
   ## Exclude
   
   Do not extract:
   
   - Common fruits
   - Vegetables
   - Ordinary animals
   - Colors
   - Everyday objects
   - Common verbs
   - Common adjectives
   - Generic vocabulary
   - Real countries
   - Real cities
   - Real continents
   - Universally known concepts
   
   ---
   
   # Translation Rules
   
   ## Consistency
   
   A term must always receive exactly one translation.
   
   Never propose multiple translations for the same term.
   
   ---
   
   ## Natural Translation
   
   Prefer natural translations over literal translations.
   
   Translate according to narrative meaning rather than word-for-word.
   
   ---
   
   ## Proper Names
   
   Translate proper names only when their meaning is narratively significant, such as:
   
   - Techniques
   - Organizations
   - Locations
   - Objects
   - Manuals
   - Skills
   - Concepts
   
   Do not translate character names or unique personal names unless the text explicitly treats them as meaningful translated terms.
   
   ---
   
   ## Context
   
   Each extracted term may optionally include a short contextual description.
   
   The context should:
   
   - contain between 5 and 12 words
   - briefly explain the narrative role
   - not repeat the translation
   - remain objective
   
   ---
   
   # Cultivation System
   
   If the text explicitly contains cultivation levels:
   
   - Extract only the levels explicitly mentioned.
   - Preserve their original order.
   - Do not infer missing levels.
   - Do not complete known cultivation systems using outside knowledge.
   
   If no cultivation system appears, return an empty list.
   
   ---
   
   # Consistency Rules
   
   - Never invent terminology.
   - Never invent cultivation levels.
   - Never use outside knowledge.
   - Never complete incomplete systems.
   - Base every decision exclusively on the provided text.
   
   ---
   
   # Prioritization
   
   If more than 40 candidate terms exist, prioritize by:
   
   1. Frequency
   2. Narrative importance
   3. Future translation relevance
   4. World-specific uniqueness
   
   ---
   
   # Output Requirements
   
   Return only data matching the provided JSON schema.
   
   For every extracted term provide:
   
   - source: original term in `<source_language>`
   - target: translated or transliterated term in `<target_language>`
   - context: optional narrative explanation
   
   For every cultivation level provide:
   
   - source: original level
   - target: translated level
   ```
  Ejemplo JSON:
  ```json
  {
    "type": "object",
    "properties": {
      "terms": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "source": {
              "type": "string"
            },
            "target": {
              "type": "string"
            },
            "context": {
              "type": ["string", "null"]
            }
          },
          "required": [
            "source",
            "target"
          ],
          "additionalProperties": false
        }
      },
      "cultivation_system": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "source": {
              "type": "string"
            },
            "target": {
              "type": "string"
            }
          },
          "required": [
            "source",
            "target"
          ],
          "additionalProperties": false
        }
      }
    },
    "required": [
      "terms",
      "cultivation_system"
    ],
    "additionalProperties": false
  }
  ```
   - Dependencies: None
   - Risk: Bajo

8. **Sembrar prompt en EnsureSchema** (File: `internal/store/store.go`)
   - Action: Añadir entrada `"glossary"` en `seedPromptDefaults` junto a `translation`, `title`, `refine`, `check`
   - Why: El prompt necesita existir en la BD para ser editable
   - Dependencies: Step 7
   - Risk: Bajo

9. **Añadir resolución de prompt de glosario** (File: `internal/api/runtime_config.go`)
   - Action: En `GetEffectivePrompts`, añadir campo `Glossary` que resuelve por Novel override > Global > Default
   - Dependencies: Steps 7, 8
   - Risk: Bajo — sigue patrón existente

10. **Crear handler HTTP** (File: `internal/api/router_glossary.go`)
    - Action: Crear `registerGlossaryRoutes()` y endpoint `POST /api/db/novels/{novelId}/generate-glossary`
    - Request body:
    ```json
    {
      "chapterFrom": 1,
      "chapterTo": 10,
      "mode": "together" | "batch",
      "maxTokensPerBatch": 90000,
      "provider": "venice",
      "model": "deepseek-r1"
    }
    ```
    - Validation: chapterFrom <= chapterTo, chapters exist, provider configured
    - Creates job with operation `"generate-glossary"` and options JSON
    - Dependencies: Steps 6, 9
    - Risk: Medio — validación de rangos y edge cases

11. **Wiring en router.go** (File: `internal/api/router.go`)
    - Action: Llamar `registerGlossaryRoutes(api, s)` desde `registerProtectedRoutes`
    - Dependencies: Step 10
    - Risk: Bajo

12. **Crear procesador de job** (File: `internal/api/runtime_glossary.go`)
    - Action: Función `processGenerateGlossaryJob(ctx, jobContext) error`
    - Lógica:
      1. Cargar capítulos del rango especificado (solo `OriginalContent`)
      2. Estimar tokens totales
      3. Si modo `together`: enviar todos en un solo prompt
      4. Si modo `batch`: dividir en lotes por `maxTokensPerBatch` (default 90000), enviar cada lote
      5. Para cada respuesta del LLM, parsear JSON → `[]glossaryEntry`
      6. Fusionar con glosario existente (dedup por `source`, actualizar si existe)
      7. Guardar glosario actualizado en la novela
      8. Job status → done
    - Dependencies: Steps 1, 2, 3, 5, 10
    - Risk: Alto — parsing de LLM response, manejo de lotes, fusión de glosarios

13. **Añadir al worker** (File: `internal/api/runtime_worker.go`)
    - Action: En `processJob()`, añadir `case "generate-glossary":` que llame a `processGenerateGlossaryJob`
    - Dependencies: Step 12
    - Risk: Bajo

14. **Añadir tipo de prompt a router_prompts** (File: `internal/api/router_prompts.go`)
    - Action: Añadir `"glossary"` a la lista de prompts soportados en `GET /api/user/prompts` y `PUT /api/user/prompts/{key}`
    - Dependencies: Steps 7, 8
    - Risk: Bajo

### Phase 3: Frontend — UI & API Client

15. **Añadir tipo TypeScript** (File: `frontend/src/domain/project-settings.ts`)
    - Action: Añadir:
    ```typescript
    export type GlossaryGenerationOptions = {
      chapterFrom: number;
      chapterTo: number;
      mode: 'together' | 'batch';
      maxTokensPerBatch: number;
      provider: string;
      model: string;
    };
    ```
    - Dependencies: None
    - Risk: Bajo

16. **Añadir método API** (File: `frontend/src/client.ts`)
    - Action: Añadir `generateGlossary(novelId, options)` que hace `POST /api/db/novels/{novelId}/generate-glossary`
    - Dependencies: Step 15
    - Risk: Bajo

17. **Crear card de generación en el tab de Glossary** (File: `frontend/src/features/projects/ProjectSettingsDialog.vue`)
    - Action: Reemplazar el layout del tab de Glossary:
      ```
      ┌─────────────────────────────────────────┐
      │  Generar glosario con IA                │
      │                                         │
      │  Capítulos: [desde] [hasta]             │
      │  Modo: (•) Todo junto  ( ) Por lotes    │
      │  [Generar]                    [spinner] │
      └─────────────────────────────────────────┘

      ┌─────────────────────────────────────────┐
      │  dragon → dragón (criatura mítica)      │
      │  [×]                                    │
      ├─────────────────────────────────────────┤
      │  Shire → La Comarca (lugar de la obra)  │
      │  [×]                                    │
      ├─────────────────────────────────────────┤
      │  + Añadir entrada                       │
      └─────────────────────────────────────────┘
      ```
    - La card tiene estilo consistente con el resto de la UI (card elevada, padding consistente)
    - Al enviar: crea el job, muestra spinner en el botón
    - Al completar: recarga el glosario del novel
    - Dependencies: Steps 10, 15, 16
    - Risk: Medio — UX del card, estados de carga

18. **Gestión de estado del job** (File: `frontend/src/features/projects/ProjectSettingsDialog.vue`)
    - Action: Reutilizar `useActiveJobs` para pollear el estado del job de generación
    - Mostrar spinner en el botón "Generar" mientras el job está en `running`
    - Al completar: `refreshNovel()` para cargar el glosario actualizado
    - Dependencies: Step 17
    - Risk: Bajo — patrón existente

### Phase 4: Testing

19. **Tests unitarios del provider** (File: `internal/ai/openai_test.go`)
    - Action: Test de `GenerateGlossary` con respuesta mock del LLM
    - Dependencies: Step 2
    - Risk: Bajo

20. **Tests de fusión de glosario** (File: `internal/api/runtime_glossary_test.go`)
    - Action: Tests de la función de fusión:
      - Glosario vacío + entradas nuevas → todas las entradas
      - Glosario existente + duplicados → se actualiza
      - Glosario existente + nuevas → se añaden
    - Dependencies: Step 12
    - Risk: Bajo

21. **Tests de estimación de tokens** (File: `internal/api/runtime_glossary_test.go`)
    - Action: Test de `estimateTokens` con textos de diferentes tamaños
    - Dependencies: Step 5
    - Risk: Bajo

22. **Test de batching** (File: `internal/api/runtime_glossary_test.go`)
    - Action: Test de que textos largos se dividen correctamente en lotes
    - Dependencies: Step 12
    - Risk: Bajo

23. **Integration test end-to-end** (File: `internal/api/router_integration_test.go`)
    - Action: Test de `POST /api/db/novels/{id}/generate-glossary` con PocketBase real
    - Dependencies: Steps 10, 12, 13
    - Risk: Medio

24. **Build y verificación** (File: `frontend/`)
    - Action: `npm run build` para verificar TypeScript
    - Dependencies: All frontend steps
    - Risk: Bajo

## Testing Strategy

- **Unit tests**: `runtime_glossary_test.go` (fusión, tokens, batching), `openai_test.go` (provider mock)
- **Integration tests**: `router_integration_test.go` (endpoint completo con PocketBase)
- **Manual testing**: `go test -short ./...` para verificación rápida
- **Frontend**: `npm run build` (typecheck)

## Risks & Mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| LLM devuelve JSON malformado | Alto | Validación estricta del output, retry con prompt corregido, fallback a parseo flexible |
| Tokens exceden límite del modelo | Medio | Estimación con tokenizer antes de enviar, modo batch como fallback automático |
| Glosario resultante muy grande | Bajo | Límite máximo de entradas configurável, warning si se excede |
| Fusión pierde contexto de entradas existentes | Medio | Nunca sobreescribir `context` si la entrada ya existe; solo añadir nuevas |
| Provider no soporta structured output | Medio | Fallback a parseo de texto libre con regex para extraer JSON |
| Job falla a mitad de proceso | Medio | Guardar entradas parciales generadas antes del fallo, status `failed` con progreso parcial |

## Success Criteria

- [ ] Card "Generar glosario" visible en la parte superior del tab de Glossary
- [ ] Selector de rango de capítulos funciona correctamente
- [ ] Modo "todo junto" envía todos los capítulos en un solo prompt
- [ ] Modo "por lotes" divide el texto (default 90K tokens/lote) y genera glosario incremental
- [ ] Prompt de glosario es editable desde la sección de Prompts
- [ ] Glosario generado se fusiona con el existente sin duplicados
- [ ] Spinner de progreso visible durante la generación
- [ ] El job aparece en la lista de jobs activos
- [ ] Al completar, el glosario se actualiza en la UI
- [ ] Tests pasan: `go test ./...` y `npm run build`
