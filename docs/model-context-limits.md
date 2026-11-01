# Límites de contexto por modelo

Rellena la columna `Max context` con el tamaño de ventana de cada modelo **en tokens** (input total que acepta el provider, no solo la salida). Cuando termines, pega el contenido de vuelta en el chat o dime qué cambiaste.

Formato: número entero en tokens. Ejemplos: `128000`, `200000`, `1000000`. Si un modelo tiene variantes de razonamiento con distinto límite, pon una fila por variante. Si no conoces el dato, deja `?`.

> Ojo: algunos providers cuentan caché/razonamiento dentro de la misma ventana; aquí solo necesitamos el máximo de contexto de entrada.

## venice — `venice`

| Modelo | Max context (tokens) |
|---|---|
| deepseek-v4-flash-0731 | 262144 |
| google-gemma-4-31b-it:disable_thinking=true | 200000 |
| aion-labs-aion-3-0-mini | 262000 |
| google-gemma-4-26b-a4b-it:disable_thinking=true | 200000 |

## openrouter — `openrouter`

| Modelo | Max context (tokens) |
|---|---|
| openai/gpt-5.6-luna (reasoning: none) | 262000 |
| openai/gpt-5.6-luna (reasoning: low) | 262000 |
| openai/gpt-5.6-luna (reasoning: medium) | 262000 |
| deepseek/deepseek-v4-flash-0731 | 262144 |
| google/gemini-3.5-flash-lite | 262144 |
| tencent/hy-mt2-30b-a3b | 8000 |
| mistralai/mistral-small-2603 | 262000 |
| mistralai/ministral-14b-2512 | 262000 |
| inclusionai/ling-3.0-flash | 262144 |
| xiaomi/mimo-v2.6-flash | 524288 |

## meta — `meta`

(eliminar provider)

## opencode-go — `opencode-go`

| Modelo | Max context (tokens) |
|---|---|
| deepseek-v4.1-flash | 262144 |

## opencode-zen — `opencode-zen`

(eliminar provider)

## inferx — `inferx`

| Modelo | Max context (tokens) |
|---|---|
| deepseek-v4-flash-0731 | 262144 |
| deepseek-v4.1-flash-1017-basic | 262144 |
| glm-5.3-flash | 262144 |
| gemma-4-31B-it-fp8 | 262144 |
| Qwen3.6-35B-A3B-FP8 | 262000 |

## lmstudio — `lmstudio`

(eliminar provider)

## google — `google`

(eliminar estw provider)

---

Fuente: `internal/ai/registry.go` (`knownProviders`). Si añades un modelo al registry, añade su fila aquí también.
