package api

import (
	"fmt"
	"strings"
)

func fillPrompt(template string, values map[string]string) string {
	result := template
	for key, value := range values {
		result = strings.ReplaceAll(result, key, value)
	}
	return strings.TrimSpace(result)
}

func formatGlossary(glossary []glossaryEntry) string {
	lines := make([]string, 0, len(glossary))
	for _, entry := range glossary {
		if !glossaryEnabled(entry) {
			continue
		}
		if entry.Context != "" {
			lines = append(lines, fmt.Sprintf("- %s → %s (%s)", entry.Source, entry.Target, entry.Context))
		} else {
			lines = append(lines, fmt.Sprintf("- %s → %s", entry.Source, entry.Target))
		}
	}
	if len(lines) == 0 {
		return "(sin glosario)"
	}
	return strings.Join(lines, "\n")
}
