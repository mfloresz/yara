package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

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
func novelCoverURL(novelID, fileName string) string {
	if strings.TrimSpace(fileName) == "" {
		return ""
	}
	return fmt.Sprintf("/api/v1/novels/%s/cover", novelID)
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
