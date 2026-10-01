package epubimport

import (
	"html"
	"regexp"
	"strings"
)

var (
	reNCXNavPointOpen = regexp.MustCompile(`<navPoint\b[^>]*>`)
	reNCXLabel        = regexp.MustCompile(`<navLabel>\s*<text>(.*?)</text>\s*</navLabel>`)
	reNCXContent      = regexp.MustCompile(`<content[^>]*src=["']([^"']+)["']`)
	reSpineToc        = regexp.MustCompile(`<spine[^>]*toc=["']([^"']+)["']`)
)

func parseNCXNavPoints(zb *zipBudget, opfPath, opfXML string, manifestMap map[string]manifestItem) []ncxNavPoint {
	tocID := extractFirstMatch(reSpineToc, opfXML)
	if tocID == "" {
		return nil
	}

	ncxItem, ok := manifestMap[tocID]
	if !ok {
		return nil
	}

	ncxPath := resolveZipPath(opfPath, ncxItem.Href)
	ncxBlob, err := zb.readFile(ncxPath)
	if err != nil {
		return nil
	}
	ncx := string(ncxBlob)

	// Scan per opening <navPoint> tag instead of matching whole blocks: NCX
	// navPoints nest (chapter navPoints contain their sections), and a flat
	// non-greedy block regex swallows nested siblings and drops entries. Per
	// spec each navPoint carries its navLabel before its content, so from
	// every opening tag the first label/content pair belongs to it.
	var navPoints []ncxNavPoint
	for _, open := range reNCXNavPointOpen.FindAllStringIndex(ncx, -1) {
		rest := ncx[open[1]:]
		labelMatch := reNCXLabel.FindStringSubmatchIndex(rest)
		if labelMatch == nil {
			continue
		}
		label := normalizeInlineText(rest[labelMatch[2]:labelMatch[3]])
		if label == "" {
			continue
		}
		contentMatch := reNCXContent.FindStringSubmatch(rest[labelMatch[1]:])
		if contentMatch == nil {
			continue
		}
		src := html.UnescapeString(strings.TrimSpace(contentMatch[1]))

		var filePath, anchor string
		if idx := strings.IndexByte(src, '#'); idx >= 0 {
			filePath = resolveZipPath(opfPath, src[:idx])
			anchor = src[idx+1:]
		} else {
			filePath = resolveZipPath(opfPath, src)
		}

		navPoints = append(navPoints, ncxNavPoint{
			Label:    label,
			FilePath: filePath,
			Anchor:   anchor,
		})
	}

	return navPoints
}
