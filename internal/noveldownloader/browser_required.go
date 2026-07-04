package noveldownloader

import (
	"net/url"
	"strings"
)

var BrowserRequiredSites = map[string]bool{
	// 69shuba.com — Cloudflare-protected chapter pages, catalog requires login
	"69shuba.com": true,
}

func IsBrowserRequiredSite(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	return BrowserRequiredSites[host]
}
