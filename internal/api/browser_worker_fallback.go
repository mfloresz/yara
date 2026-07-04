package api

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"translator-server/internal/noveldownloader"
)

// getNovelInfoWithFallback tries Go parsers first (with normal HTTP).
// If the site requires a browser (Cloudflare), it falls back to fetching
// the page through the Browser Worker proxy and then parsing with Go.
func (s *Server) getNovelInfoWithFallback(ctx context.Context, url string) (*noveldownloader.NovelInfo, error) {
	dl := s.DownloaderFactory()

	// 1. Try the Go parser with normal HTTP first
	parser := dl.FindParser(url)
	if parser != nil {
		slog.Info("found HTTP parser, trying direct fetch", "parser", parser.Name(), "url", url)
		info, err := dl.GetNovelInfo(ctx, url)
		if err == nil {
			return info, nil
		}
		slog.Info("direct HTTP failed, will try browser proxy", "error", err)
	}

	// 2. If no parser or HTTP failed, try via browser proxy
	if !s.HasBrowserWorker() {
		if parser != nil {
			return nil, fmt.Errorf("HTTP fetch failed and no browser worker connected")
		}
		return nil, fmt.Errorf("unsupported URL: no parser found and no browser worker connected")
	}

	slog.Info("fetching via browser proxy", "url", url)
	return s.getNovelInfoViaProxy(ctx, url, parser)
}

// getNovelInfoViaProxy fetches the page HTML through the browser worker,
// then parses it with the same Go parsers used for direct HTTP.
func (s *Server) getNovelInfoViaProxy(ctx context.Context, url string, parser noveldownloader.Parser) (*noveldownloader.NovelInfo, error) {
	proxyClient := NewProxyHTTPClient(s)
	dl := s.DownloaderFactoryWithClient(proxyClient)

	// If we have a parser, use it
	if parser != nil {
		info, err := parser.GetNovelInfo(ctx, proxyClient, url)
		if err != nil {
			return nil, fmt.Errorf("parser %s failed via proxy: %w", parser.Name(), err)
		}
		slog.Info("proxy fetch + parse succeeded", "parser", parser.Name(), "title", info.Title, "chapters", len(info.Chapters))
		return info, nil
	}

	// No parser known - try all parsers with the proxy HTML
	info, err := dl.GetNovelInfo(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("no parser could handle %s via proxy: %w", url, err)
	}
	return info, nil
}

// DownloaderFactoryWithClient creates a Downloader with a custom HTTP client.
func (s *Server) DownloaderFactoryWithClient(client noveldownloader.HTTPClient) *noveldownloader.Downloader {
	dl := noveldownloader.NewDownloaderWithClient(client)
	if s.Cfg != nil {
		if s.Cfg.DownloadMinDelayMs > 0 {
			dl.MinChapterDelay = time.Duration(s.Cfg.DownloadMinDelayMs) * time.Millisecond
		}
		if s.Cfg.DownloadMaxDelayMs > 0 {
			dl.MaxChapterDelay = time.Duration(s.Cfg.DownloadMaxDelayMs) * time.Millisecond
		}
	}
	return dl
}
