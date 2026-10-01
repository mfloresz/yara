package epubimport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"

	nhtml "golang.org/x/net/html"
)

// ImageTokenRe matches the [[IMG-n]] placeholders that replace inline images
// in chapter content. It is the canonical definition; the translate/refine
// validation and the EPUB exporter rely on the same shape.
var ImageTokenRe = regexp.MustCompile(`\[\[IMG-\d+\]\]`)

// imageRefMarkerRe matches the intermediate markers planted in the HTML
// before markdown conversion. They are plain alphanumeric words so html2md
// passes them through untouched; the per-chapter post-pass renumbers them
// into [[IMG-n]] tokens.
var imageRefMarkerRe = regexp.MustCompile(`IMGREF-[0-9a-f]{16}`)

// imgRefPrefix is prepended to the hex key of a resolved image while the
// content is still HTML/markdown-in-progress.
const imgRefPrefix = "IMGREF-"

type imageRef struct {
	ZipPath   string
	Alt       string
	MediaType string
}

// imageManifestIndex keys image manifest items by their resolved zip path so
// an <img src> inside a chapter file can be looked up by what it points at.
func imageManifestIndex(opfPath string, manifest []manifestItem) map[string]manifestItem {
	index := make(map[string]manifestItem, len(manifest))
	for _, item := range manifest {
		if !strings.HasPrefix(strings.ToLower(item.MediaType), "image/") {
			continue
		}
		index[resolveZipPath(opfPath, item.Href)] = item
	}
	return index
}

func imageRefKey(zipPath string) string {
	sum := sha256.Sum256([]byte(zipPath))
	return hex.EncodeToString(sum[:8])
}

// replaceImagesWithRefs rewrites every <img> whose src resolves to an image
// manifest item into an IMGREF marker in place, preserving position. Images
// that cannot be resolved are left as-is (the historical dead-reference
// behavior). The returned map holds one ref per distinct image zip path.
func replaceImagesWithRefs(htmlString, htmlPath string, imgIndex map[string]manifestItem) (string, map[string]imageRef) {
	if len(imgIndex) == 0 || !strings.Contains(htmlString, "<img") {
		return htmlString, nil
	}

	doc, err := nhtml.Parse(strings.NewReader(htmlString))
	if err != nil {
		return htmlString, nil
	}

	refs := make(map[string]imageRef)
	var imgs []*nhtml.Node
	var walk func(*nhtml.Node)
	walk = func(n *nhtml.Node) {
		if n.Type == nhtml.ElementNode && n.DataAtom.String() == "img" {
			imgs = append(imgs, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	replaced := false
	for _, node := range imgs {
		src, alt := "", ""
		for _, a := range node.Attr {
			switch a.Key {
			case "src":
				src = strings.TrimSpace(a.Val)
			case "alt":
				alt = strings.TrimSpace(a.Val)
			}
		}
		if src == "" {
			continue
		}
		zipPath := resolveZipPath(htmlPath, src)
		item, ok := imgIndex[zipPath]
		if !ok {
			continue
		}
		key := imageRefKey(zipPath)
		if _, exists := refs[key]; !exists {
			refs[key] = imageRef{ZipPath: zipPath, Alt: alt, MediaType: item.MediaType}
		}
		marker := nhtml.Node{Type: nhtml.TextNode, Data: imgRefPrefix + key}
		if node.Parent != nil {
			node.Parent.InsertBefore(&marker, node)
			node.Parent.RemoveChild(node)
			replaced = true
		}
	}
	if !replaced {
		return htmlString, nil
	}

	var out strings.Builder
	if err := nhtml.Render(&out, doc); err != nil {
		return htmlString, nil
	}
	return out.String(), refs
}

// finalizeChapterImages renumbers the IMGREF markers in each chapter's
// content into per-chapter [[IMG-n]] tokens (n by order of first distinct
// appearance) and attaches the resolved image blobs to Chapter.Images.
// Blob reads run under the image budget; a shared cache avoids re-reading
// an image referenced from several chapters.
func finalizeChapterImages(budget *imageBudget, chapters []Chapter, refs map[string]imageRef) error {
	if len(refs) == 0 {
		return nil
	}
	blobs := make(map[string][]byte)
	for i := range chapters {
		matches := imageRefMarkerRe.FindAllString(chapters[i].Content, -1)
		if len(matches) == 0 {
			continue
		}
		tokens := make(map[string]string)
		var images []Image
		for _, marker := range matches {
			key := strings.TrimPrefix(marker, imgRefPrefix)
			token, ok := tokens[key]
			if !ok {
				ref, known := refs[key]
				if !known {
					continue
				}
				blob, cached := blobs[key]
				if !cached {
					var readErr error
					blob, readErr = budget.read(ref.ZipPath)
					if readErr != nil {
						return fmt.Errorf("chapter %q image: %w", chapters[i].Title, readErr)
					}
					blobs[key] = blob
				}
				token = fmt.Sprintf("[[IMG-%d]]", len(tokens)+1)
				tokens[key] = token
				images = append(images, Image{
					Token:    token,
					Alt:      ref.Alt,
					MimeType: imageMime(ref.MediaType, ref.ZipPath, blob),
					Blob:     blob,
				})
			}
			chapters[i].Content = strings.Replace(chapters[i].Content, marker, token, 1)
		}
		chapters[i].Images = images
	}
	return nil
}

func imageMime(manifestType, zipPath string, blob []byte) string {
	if mt := strings.TrimSpace(manifestType); mt != "" {
		return mt
	}
	if mt := mime.TypeByExtension(path.Ext(zipPath)); mt != "" {
		return mt
	}
	return http.DetectContentType(blob)
}
