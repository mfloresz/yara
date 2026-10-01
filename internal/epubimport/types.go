package epubimport

type Chapter struct {
	Title   string
	Content string
	// Images holds the blobs for the [[IMG-n]] tokens present in Content,
	// in token order. Tokens are numbered per chapter by order of first
	// appearance, so the same image referenced twice inside one chapter
	// yields one entry referenced by two occurrences.
	Images []Image
}

type Image struct {
	Token    string
	Alt      string
	MimeType string
	Blob     []byte
}

type Result struct {
	Title       string
	Author      string
	Description string
	Language    string
	Series      string
	Number      string
	CoverMime   string
	CoverBlob   []byte
	Chapters    []Chapter
}

type manifestItem struct {
	ID         string
	Href       string
	MediaType  string
	Properties string
}

type ncxNavPoint struct {
	Label    string
	FilePath string
	Anchor   string
}
