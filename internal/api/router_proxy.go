package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
)

// fetchViaBrowserWorker sends a fetch_page job through the browser job queue.
// All requests are serialized (FIFO) through a single goroutine, which
// guarantees sequential page loads — critical for Cloudflare session handling.
func (s *Server) fetchViaBrowserWorker(url string, timeoutSec int, userID string) (*ProxyFetchResult, error) {
	params := map[string]interface{}{
		"timeout": timeoutSec,
	}
	result, err := s.EnqueueBrowserJob("fetch_page", url, params, userID)
	if err != nil {
		return nil, err
	}
	return &ProxyFetchResult{
		URL:    url,
		Title:  getStringFromData(result.Data, "title"),
		HTML:   getStringFromData(result.Data, "html"),
		Text:   getStringFromData(result.Data, "text"),
		Status: "ok",
	}, nil
}

// registerProxyRoutes registers the raw HTML proxy endpoint.
func registerProxyRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/proxy/fetch", func(e *core.RequestEvent) error {
		body := struct {
			URL     string `json:"url"`
			Timeout int    `json:"timeout"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if strings.TrimSpace(body.URL) == "" {
			return e.BadRequestError("url is required", nil)
		}
		timeout := body.Timeout
		if timeout <= 0 {
			timeout = 120
		}
		if timeout > 300 {
			timeout = 300
		}

		slog.Info("proxy fetch request", "url", body.URL, "timeout", timeout)

		if !s.HasBrowserWorker() {
			return e.BadRequestError("no browser worker connected", nil)
		}

		result, err := s.fetchViaBrowserWorker(body.URL, timeout, e.Auth.Id)
		if err != nil {
			if err == ErrBrowserWorkerTimeout {
				return e.BadRequestError("timeout waiting for browser worker", nil)
			}
			return e.InternalServerError("fetch failed", err)
		}

		return e.JSON(http.StatusOK, map[string]any{
			"url":    result.URL,
			"title":  result.Title,
			"html":   result.HTML,
			"text":   result.Text,
			"status": result.Status,
		})
	})
}

type ProxyFetchResult struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	HTML   string `json:"html"`
	Text   string `json:"text"`
	Status string `json:"status"`
}

func getStringFromData(data map[string]interface{}, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

// Ensure unused imports are referenced.
var _ = json.Marshal
var _ = time.Now
