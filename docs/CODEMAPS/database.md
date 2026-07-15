# Database Codemap

**Last Updated:** 2026-07-14
**Engine:** SQLite (via embedded PocketBase)
**Schema Management:** Code-defined in `internal/store/store_schema.go` with idempotent `ensureField` migrations

## Collections (11 total)

### `users` — Multi-user auth

| Field | Type | Notes |
|-------|------|-------|
| `id` | text | PK, auto |
| `email` | text | Unique, login |
| `password` | text | Hashed by PB |
| `name` | text | Max 120 |
| `theme` | select | `light`, `dark`, `system` |
| `active_provider` | text | Max 120 |
| `title_provider` | text | Max 120 |
| `title_model` | text | Max 200 |
| `created` | autodate | Auto |
| `updated` | autodate | Auto |

### `providers` — AI provider catalog (seeded data)

| Field | Type | Notes |
|-------|------|-------|
| `id` | text | PK |
| `key` | text | Unique ID (`venice`, `opencode-go`, etc.) |
| `label` | text | Display name |
| `base_url` | text | API base URL |
| `default_model` | text | Default model |
| `kind` | text | `openai` |
| `models_json` | text | JSON array of models |
| `enabled` | bool | Active |
| `owner` | relation → users | Admin owner |

### `user_provider_settings` — Per-user provider config

| Field | Type | Notes |
|-------|------|-------|
| `owner` | relation → users | Owner |
| `provider` | relation → providers | Provider |
| `model` | text | Override |
| `base_url` | text | Override |
| `api_key_encrypted` | text | AES-GCM encrypted |
| `api_key_configured` | bool | Flag (never exposes secret) |
| `api_key_updated_at` | date | Last key update |
| `timeout_ms` | number | Request timeout |

### `user_prompt_settings` — Custom prompts

| Field | Type | Notes |
|-------|------|-------|
| `owner` | relation → users | Owner |
| `key` | text | `translation`, `refine`, `check` |
| `label` | text | Display name |
| `description` | text | Description |
| `system_prompt` | editor | System prompt |
| `user_prompt` | editor | User prompt |
| `active` | bool | Enabled |

### `user_translation_settings` — Default translation options

| Field | Type | Notes |
|-------|------|-------|
| `owner` | relation → users | Owner |
| `auto_segment` | bool | Auto-segment long chapters |
| `threshold_chars` | number | Threshold to split |
| `max_chars` | number | Max per segment |
| `min_chars` | number | Min per segment |
| `max_retries` | number | Max retries on failure |
| `enable_check` | bool | Post-translation check |
| `include_previous_title_hints` | bool | Cross-chapter context |
| `concurrency` | number | Reserved for future use |

### `novels` — Novels

| Field | Type | Notes |
|-------|------|-------|
| `owner` | relation → users | Owner |
| `source_language` | text | Source language |
| `target_language` | text | Target language |
| `source_title` | text | Original title |
| `source_author` | text | Original author |
| `source_description` | editor | Original description |
| `source_series` | text | Series name |
| `source_number` | text | Number in series |
| `target_title` | text | Translated title |
| `target_author` | text | Translated author |
| `target_description` | editor | Translated description |
| `target_series` | text | Series (translated) |
| `target_number` | text | Number (translated) |
| `glossary` | text | JSON array of entries |
| `translation_system_prompt` | editor | Novel-level prompt override |
| `translation_user_prompt` | editor | Novel-level prompt override |
| `refine_system_prompt` | editor | Novel-level prompt override |
| `refine_user_prompt` | editor | Novel-level prompt override |
| `check_system_prompt` | editor | Novel-level prompt override |
| `check_user_prompt` | editor | Novel-level prompt override |
| `notes` | editor | Translator notes |
| `ai_options` | text | JSON: provider, model, timeout |
| `translation_options` | text | JSON: segment settings, etc. |
| `cleanup_rules` | text | JSON: cleanup rules |
| `url` | text | Source URL |
| `custom_commands` | editor | Custom commands |
| `status` | select | `ongoing`, `completed`, `hiatus`, `cancelled` |
| `tags` | text | JSON array |
| `cover` | file | Cover image |
| `thumbnail` | file | Thumbnail |
| `is_public` | bool | Visible to other users |
| `chapter_count` | number | Cache |
| `translated_count` | number | Cache |
| `completed_count` | number | Cache |
| `original_char_count` | number | Cache |
| `translated_char_count` | number | Cache |
| `refined_char_count` | number | Cache |
| `total_char_count` | number | Cache |
| `max_chapter_order` | number | Cache |
| `last_checked_at` | text | Last URL update check |
| `last_check_new_chapters` | number | New chapters found |

### `chapters` — Chapters

| Field | Type | Notes |
|-------|------|-------|
| `novel` | relation → novels | Cascade delete |
| `chapter_order` | number | Order number |
| `title` | text | Original title |
| `translated_title` | text | Translated title |
| `original_content` | editor | Original (markdown) |
| `translated_content` | editor | Translated |
| `refined_content` | editor | Refined |
| `status` | select | `pending`, `processing`, `translated`, `refined`, `done`, `failed` |
| `error_message` | editor | Error details |
| `original_char_count` | number | Cache |
| `translated_char_count` | number | Cache |
| `refined_char_count` | number | Cache |

### `translation_jobs` — Processing jobs

| Field | Type | Notes |
|-------|------|-------|
| `novel` | relation → novels | Cascade delete |
| `owner` | relation → users | Owner |
| `status` | select | `pending`, `running`, `done`, `cancelled`, `failed` |
| `operation` | select | `translate`, `refine`, `download`, `check` |
| `provider` | text | Provider used |
| `model` | text | Model used |
| `chapter_ids` | text | JSON array of chapter IDs |
| `options_json` | text | Serialized options |
| `error_message` | editor | Global error |
| `total_chapters` | number | Total to process |
| `completed_chapters` | number | Completed count |
| `failed_chapters` | number | Failed count |
| `auto_segment_enabled` | bool | Segmentation active |
| `auto_segment_active` | bool | Currently segmenting |
| `auto_segment_count` | number | Total segments |
| `auto_segment_current_index` | number | Current segment index |
| `auto_segment_completed_count` | number | Completed segments |
| `auto_segment_chapter_id` | text | Chapter being segmented |
| `auto_segment_chapter_title` | text | Chapter title |
| `new_chapters` | number | New chapters found |

### `epubs` — Generated EPUBs

| Field | Type | Notes |
|-------|------|-------|
| `novel` | relation → novels | Cascade delete |
| `file_kind` | select | `original`, `translated` |
| `source_variant` | text | `original`, `translated`, `refined` |
| `label` | text | User label |
| `file` | file | EPUB file (max 200MB) |

### `reading_progress` — Reading progress

| Field | Type | Notes |
|-------|------|-------|
| `user` | relation → users | Reader |
| `novel` | relation → novels | Novel |
| `chapter_id` | text | Current chapter ID |
| `scroll_percent` | number | Scroll position |

### `worker_tokens` — Browser worker auth tokens

| Field | Type | Notes |
|-------|------|-------|
| `owner` | relation → users | Owner |
| `extension_id` | text | Extension identifier |
| `token_hash` | text | Hashed token |
| `label` | text | User label |
| `last_used_at` | date | Last usage |
| `revoked` | bool | Revoked flag |

## Schema migrations

- All collections created in `EnsureSchema()` (`internal/store/store.go`)
- `ensureField()` helper for idempotent field additions
- `ensureCollectionIndex()` for idempotent index creation
- Cascade deletes configured for chapters, jobs, epubs, reading_progress
- Novel stats kept current per-operation via `RecalculateNovelStats()` — no boot-time backfill

## Related codemaps

- [Backend](backend.md) — Store layer consuming these collections
- [Integrations](integrations.md) — Provider data seeded from registry
