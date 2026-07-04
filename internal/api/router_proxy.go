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

		result, err := s.fetchViaBrowserWorker(body.URL, timeout)
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

func (s *Server) fetchViaBrowserWorker(url string, timeoutSec int) (*ProxyFetchResult, error) {
	browserWorkersMu.RLock()
	var worker *BrowserWorker
	for _, w := range browserWorkers {
		if w.Conn != nil {
			worker = w
			break
		}
	}
	browserWorkersMu.RUnlock()

	if worker == nil {
		return nil, ErrNoBrowserWorker
	}

	jobID := generateJobID()
	req := BrowserWorkerJobRequest{
		JobID:     jobID,
		Operation: "fetch_page",
		URL:       url,
		Params:    map[string]interface{}{"timeout": timeoutSec},
	}

	payload, _ := json.Marshal(req)
	msg := BrowserWorkerMessage{
		Type:      "job_request",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}

	worker.mu.Lock()
	err := worker.Conn.WriteJSON(msg)
	worker.mu.Unlock()

	if err != nil {
		return nil, err
	}

	slog.Info("sent fetch job to browser worker", "workerId", worker.ID, "jobId", jobID, "url", url)

	timeout := time.After(time.Duration(timeoutSec) * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return nil, ErrBrowserWorkerTimeout
		case result := <-s.browserWorkerResultCh:
			if result.JobID == jobID {
				if result.Status != "ok" {
					return nil, &BrowserWorkerError{msg: "browser worker error: " + result.Status}
				}
				return &ProxyFetchResult{
					URL:    url,
					Title:  getStringFromData(result.Data, "title"),
					HTML:   getStringFromData(result.Data, "html"),
					Text:   getStringFromData(result.Data, "text"),
					Status: "ok",
				}, nil
			}
			s.browserWorkerResultCh <- result
		case <-ticker.C:
			continue
		}
	}
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
