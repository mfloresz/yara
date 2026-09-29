package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/pocketbase/dbx"
	"golang.org/x/text/unicode/norm"

	"translator-server/internal/ai"
)

func providerKind(info ai.ProviderInfo) string {
	_ = info
	return "openai-compatible"
}

func normalizeTranslation(cfg TranslationDefaults) TranslationDefaults {
	if cfg.ThresholdChars <= 0 {
		cfg.ThresholdChars = DefaultTranslationDefaults.ThresholdChars
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = DefaultTranslationDefaults.MaxChars
	}
	if cfg.MinChars <= 0 {
		cfg.MinChars = DefaultTranslationDefaults.MinChars
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = DefaultTranslationDefaults.MaxRetries
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = DefaultTranslationDefaults.Concurrency
	}
	return cfg
}

// novelCoverURL points at the authenticated cover handler instead of
// PocketBase's native /api/files route, because cover/thumbnail files are
// Protected (see ensureNovelsCollection) and require a file token there.
// The PocketBase-generated file name doubles as a cache version (?v=): it
// changes on every re-upload, so clients can cache the image long-term and
// a cover swap invalidates the cache by producing a different URL.
// Novels without a stored file get "": the frontend renders its bundled
// default (/no_cover.jpg, shared immutable URL downloaded once) so no
// authenticated request is needed for cover-less novels.
func novelCoverURL(novelID, fileName string) string {
	if strings.TrimSpace(fileName) == "" {
		return ""
	}
	return fmt.Sprintf("/api/v1/novels/%s/cover?v=%s", novelID, fileName)
}

func jsonString(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	b, err := json.Marshal(value)
	if err != nil || string(b) == "null" {
		return fallback
	}
	return string(b)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// narrowSet builds the SET clause of a targeted UPDATE statement so hot paths
// can persist individual columns without hydrating a record or rewriting the
// whole row (chapter rows carry full content; novel rows carry up to MBs of
// glossary/prompts). Column names come from fixed call sites, values are bound.
type narrowSet struct {
	sets   []string
	params dbx.Params
}

func newNarrowSet() *narrowSet {
	return &narrowSet{params: dbx.Params{}}
}

func (n *narrowSet) add(column string, value any) {
	name := fmt.Sprintf("v%d", len(n.sets))
	n.sets = append(n.sets, column+" = {:"+name+"}")
	n.params[name] = value
}

func (n *narrowSet) bind(key string, value any) {
	n.params[key] = value
}

// exec runs "UPDATE table SET ... WHERE where" and returns the affected rows.
func (n *narrowSet) exec(s *Store, table, where string) (int64, error) {
	sql := "UPDATE " + table + " SET " + strings.Join(n.sets, ", ") + " WHERE " + where
	res, err := s.App.DB().NewQuery(sql).Bind(n.params).Execute()
	if err != nil {
		return 0, err
	}
	rows, _ := res.RowsAffected()
	return rows, nil
}

func clampText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}

func firstString(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func asInt(value float64, fallback int) int {
	if value == 0 {
		return fallback
	}
	return int(value)
}

// normalizeLanguageCode canonicalizes a language identifier so stored values
// compare predictably. The `progress=translated` filter matches
// `source_language = target_language` in SQL, which is case-sensitive, so
// "EN" vs "en" would otherwise make a same-language novel look translatable.
func normalizeLanguageCode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeNovelStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "completed":
		return "completed"
	case "hiatus":
		return "hiatus"
	case "cancelled":
		return "cancelled"
	default:
		return "ongoing"
	}
}

// stripAccents removes diacritics: decompose to NFD and drop combining marks.
// Without the prior decomposition, "á" (U+00E1) is a single non-Mn rune and the
// loop over the raw string would never remove it.
// "Fantasía" -> "fantasia", "Ação" -> "acao".
func stripAccents(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// normalizeTagKey returns the canonical form used to compare tags:
// lowercase, trimmed, without diacritics.
func normalizeTagKey(tag string) string {
	return stripAccents(strings.ToLower(strings.TrimSpace(tag)))
}

func normalizeNovelTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.Join(strings.Fields(strings.TrimSpace(tag)), " ")
		if tag == "" {
			continue
		}
		key := normalizeTagKey(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return normalizeTagKey(out[i]) < normalizeTagKey(out[j])
	})
	return out
}

func normalizeNovelTagsValue(value any) []string {
	switch v := value.(type) {
	case nil:
		return []string{}
	case []string:
		return normalizeNovelTags(v)
	case []any:
		tags := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			tags = append(tags, s)
		}
		return normalizeNovelTags(tags)
	default:
		return []string{}
	}
}

func parseNovelTagsJSON(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return []string{}
	}
	return normalizeNovelTags(tags)
}

func camelToSnake(key string) string {
	switch key {
	case "errorMessage":
		return "error_message"
	case "completedChapters":
		return "completed_chapters"
	case "failedChapters":
		return "failed_chapters"
	case "totalChapters":
		return "total_chapters"
	case "autoSegmentEnabled":
		return "auto_segment_enabled"
	case "autoSegmentActive":
		return "auto_segment_active"
	case "autoSegmentCount":
		return "auto_segment_count"
	case "autoSegmentCurrentIndex":
		return "auto_segment_current_index"
	case "autoSegmentCompletedCount":
		return "auto_segment_completed_count"
	case "autoSegmentChapterId":
		return "auto_segment_chapter_id"
	case "autoSegmentChapterTitle":
		return "auto_segment_chapter_title"
	default:
		return key
	}
}

func normalizeTheme(theme string) string {
	switch theme {
	case "light", "dark", "system":
		return theme
	default:
		return "system"
	}
}
