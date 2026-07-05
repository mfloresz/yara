package noveldownloader

import (
	"net/url"
	"strings"
)

type NovelfireParser struct{}

func NewNovelfireParser() *NovelfireParser {
	return &NovelfireParser{}
}

func (p *NovelfireParser) Name() string {
	return "novelfire"
}

func (p *NovelfireParser) CanHandle(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	// Both domains are mirrors; accept either.
	return host == "novelfire.net" || host == "novelphoenix.com"
}

// isNovelPhoenix returns true if the URL points to the novelphoenix.com domain.
func isNovelPhoenix(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	return host == "novelphoenix.com"
}
