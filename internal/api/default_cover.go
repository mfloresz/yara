package api

import (
	_ "embed"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// DefaultCoverMime is the content type of the bundled fallback cover.
//
//go:embed assets/no_cover.jpg
var DefaultCoverBytes []byte

const DefaultCoverMime = "image/jpeg"

// serveDefaultCover writes the bundled fallback cover. The caller must have
// already verified the novel exists and is visible to the requester — the
// bytes are identical for every novel, but the route stays authenticated so
// anonymous callers cannot probe novel IDs.
func serveDefaultCover(e *core.RequestEvent) error {
	e.Response.Header().Set("Content-Type", DefaultCoverMime)
	e.Response.Header().Set("Cache-Control", "private, max-age=3600")
	e.Response.WriteHeader(http.StatusOK)
	_, err := e.Response.Write(DefaultCoverBytes)
	return err
}
