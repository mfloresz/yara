package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// Byte-level compatibility shims the parser engine cannot express itself.
// ctx.get(url) takes a URL and nothing else, so anything a site needs at the
// HTTP level — a required header, a non-UTF-8 body — has to be handled here,
// keyed off the target host or off the response bytes.

// skyNovelsRefererHost is the Referer the SkyNovels API demands. Its endpoints
// live under api.skynovels.net and reject requests without it, so the Referer
// is supplied for that host and the site's own pages.
const skyNovelsReferer = "https://www.skynovels.net/"

// applySiteHeaders sets request headers a site requires but that a script
// cannot express through the ctx.get(url) contract. This is the one place
// site-specific HTTP policy may live; a binding for per-request headers would
// be the better long-term home.
func applySiteHeaders(req *http.Request) {
	if req.URL == nil {
		return
	}
	host := strings.ToLower(req.URL.Host)
	if strings.TrimPrefix(host, "www.") == "skynovels.net" || host == "api.skynovels.net" {
		req.Header.Set("Referer", skyNovelsReferer)
		req.Header.Set("Accept", "application/json")
	}
}

// validateSiteFetchURL is the SSRF guard for every URL a parser script (or
// site-supplied metadata such as a cover URL) asks the server to fetch
// directly. Scripts are user-editable and auto-updated from the network, so
// the URLs they request are untrusted: without this, a malicious or buggy
// script could make the server probe its own loopback, cloud metadata
// endpoints (169.254.169.254) or other hosts on the private network, and the
// response body would land in the library database.
//
// Only http/https URLs whose host resolves exclusively to public IPs pass.
// Set PARSERS_ALLOW_PRIVATE_NETS=1 to bypass (local development and tests,
// where site hosts are rewritten onto 127.0.0.1 mocks). The browser-worker
// path needs no guard: those requests run in the user's own browser, not on
// the server. Manifest/script downloads are also exempt: their URLs are
// admin-configured, and their protection is the ed25519 signature.
func (s *Server) validateSiteFetchURL(ctx context.Context, rawURL string) error {
	if s != nil && s.Cfg != nil && s.Cfg.ParsersAllowPrivateNets {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q", rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("refusing to fetch non-http(s) URL %q", rawURL)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("refusing to fetch URL without a host %q", rawURL)
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return fmt.Errorf("resolving %q: %w", host, err)
		}
		for _, addr := range addrs {
			ips = append(ips, addr.IP)
		}
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q resolves to no addresses", host)
	}
	for _, ip := range ips {
		if !isPublicFetchIP(ip) {
			return fmt.Errorf("refusing to fetch %q: host resolves to non-public IP %s", rawURL, ip.String())
		}
	}
	return nil
}

// isPublicFetchIP reports whether an IP is a plausible public fetch target.
// Everything else (loopback, private ranges, link-local, multicast,
// unspecified, benchmarking/reserved) is refused. Best-effort against DNS
// rebinding (resolution happens once, right before use), not a proof.
func isPublicFetchIP(ip net.IP) bool {
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return false
	}
	// Ranges Go's IsPrivate does not cover but that are never public fetch
	// targets: benchmarking (198.18/15), TEST-NETs, reserved 240/4, CGNAT
	// (100.64/10), the NAT64 well-known prefix (64:ff9b::/96) and
	// documentation v6 (2001:db8::/32).
	_, bench, _ := net.ParseCIDR("198.18.0.0/15")
	_, test1, _ := net.ParseCIDR("192.0.2.0/24")
	_, test2, _ := net.ParseCIDR("198.51.100.0/24")
	_, test3, _ := net.ParseCIDR("203.0.113.0/24")
	_, reserved240, _ := net.ParseCIDR("240.0.0.0/4")
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	_, nat64, _ := net.ParseCIDR("64:ff9b::/96")
	_, docv6, _ := net.ParseCIDR("2001:db8::/32")
	for _, blocked := range []*net.IPNet{bench, test1, test2, test3, reserved240, cgnat, nat64, docv6} {
		if blocked.Contains(ip) {
			return false
		}
	}
	return true
}

// decodeChineseCharset decodes GBK-family responses (GBK, GB2312, GB18030) to
// UTF-8. The encoding is taken from the Content-Type header when it names a
// GBK charset, otherwise from the document's <meta charset> declaration; if
// neither does, the bytes are returned unchanged.
//
// Already-valid UTF-8 short-circuits the whole thing. That matters because a
// browser-worker relay decodes to UTF-8 before returning, while the relayed HTML
// may still declare charset=gbk — re-decoding it would corrupt the text.
func decodeChineseCharset(raw []byte, contentType string) []byte {
	if utf8.Valid(raw) {
		return raw
	}
	if !declaresChineseCharset(contentType) && !metaDeclaresChineseCharset(raw) {
		return raw
	}
	// Try GBK first, fall back to GB18030 (a superset), and accept a decoder
	// only if its output actually looks like valid UTF-8.
	for _, decoder := range []transform.Transformer{
		simplifiedchinese.GBK.NewDecoder(),
		simplifiedchinese.GB18030.NewDecoder(),
	} {
		decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(raw), decoder))
		if err == nil && isLikelyUTF8(decoded) {
			return decoded
		}
	}
	// Decoding failed; the raw bytes are more useful than mojibake.
	return raw
}

var chineseCharsets = []string{"gbk", "gb2312", "gb18030", "gb_2312", "x-gbk"}

// declaresChineseCharset reports whether a Content-Type header names a
// GBK-family charset.
func declaresChineseCharset(contentType string) bool {
	lower := strings.ToLower(contentType)
	for _, cs := range chineseCharsets {
		if strings.Contains(lower, cs) {
			return true
		}
	}
	return false
}

// metaDeclaresChineseCharset reports whether the document head declares a
// GBK-family charset, covering both <meta charset="gbk"> and
// <meta http-equiv content="text/html; charset=gbk">.
func metaDeclaresChineseCharset(raw []byte) bool {
	peekLen := 4096
	if len(raw) < peekLen {
		peekLen = len(raw)
	}
	lower := strings.ToLower(string(raw[:peekLen]))
	for _, cs := range chineseCharsets {
		if strings.Contains(lower, `charset="`+cs+`"`) ||
			strings.Contains(lower, `charset=`+cs+`"`) ||
			strings.Contains(lower, `content="text/html; charset=`+cs+`"`) {
			return true
		}
	}
	return false
}

// isLikelyUTF8 samples the head of a decoded buffer and checks that its
// multi-byte sequences are well-formed, so a wrong decoder is rejected rather
// than silently producing a second layer of mojibake.
func isLikelyUTF8(b []byte) bool {
	nonASCII := 0
	valid := 0
	i := 0
	sample := len(b)
	if sample > 5000 {
		sample = 5000
	}
	for i < sample {
		if b[i] < 0x80 {
			i++
			continue
		}
		nonASCII++
		switch {
		case b[i] >= 0xC0 && b[i] <= 0xDF && i+1 < sample && b[i+1]&0xC0 == 0x80:
			valid++
			i += 2
		case b[i] >= 0xE0 && b[i] <= 0xEF && i+2 < sample && b[i+1]&0xC0 == 0x80 && b[i+2]&0xC0 == 0x80:
			valid++
			i += 3
		case b[i] >= 0xF0 && b[i] <= 0xF4 && i+3 < sample && b[i+1]&0xC0 == 0x80 && b[i+2]&0xC0 == 0x80 && b[i+3]&0xC0 == 0x80:
			valid++
			i += 4
		default:
			i++
		}
	}
	if nonASCII == 0 {
		return true
	}
	return float64(valid)/float64(nonASCII) > 0.8
}
