package comicdownloader

import (
	"bytes"
)

// isLikelyImage reports whether data begins with a known image file signature.
// Used to reject HTML error/placeholder pages that the proxy may return for
// image URLs instead of the binary image.
func isLikelyImage(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return true
	}
	// PNG: 89 50 4E 47
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return true
	}
	// GIF: 47 49 46 38 ("GIF8")
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 {
		return true
	}
	// WEBP: RIFF....WEBP
	if len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return true
	}
	// AVIF: ....ftypavif
	if len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")) &&
		(bytes.Equal(data[8:12], []byte("avif")) || bytes.Equal(data[8:12], []byte("avis"))) {
		return true
	}
	return false
}
