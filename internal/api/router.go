package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/config"
	"translator-server/internal/noveldownloader"
	"translator-server/internal/store"
)

// BrowserJob is a browser worker request waiting to be dispatched.
// The Result channel is written to by the consumer goroutine and read
// by the caller that originally enqueued the job.
type BrowserJob struct {
	Request BrowserWorkerJobRequest
	UserID  string
}

// pendingBrowserJob tracks an in-flight browser job: the result channel the
// caller blocks on, the context whose cancellation stops the safety-net
// timeout, and the cancel func invoked once a real result has been delivered.
// Without the cancel, the 5-minute timeout goroutine fires for jobs that
// already succeeded, emitting misleading "browser worker job timed out" warnings.
//
// resolveOnce guarantees the result channel is sent to and closed exactly once,
// whether the real result or the timeout wins the race — preventing a panic
// from sending on a closed channel.
type pendingBrowserJob struct {
	result      chan *BrowserWorkerJobResult
	ctx         context.Context
	cancel      context.CancelFunc
	resolveOnce sync.Once
}

type Server struct {
	Store                  *store.Store
	Cfg                    *config.Config
	downloadQueue          chan string
	translateQueue         chan string
	queuedJobs             map[string]struct{}
	queueMu                sync.Mutex
	cancelMu               sync.Mutex
	jobCancels             map[string]context.CancelFunc
	DownloaderFactory      func() *noveldownloader.Downloader
	previewCacheMu         sync.RWMutex
	previewCache           map[string]previewCacheEntry
	importInfoCacheMu      sync.RWMutex
	importInfoCache        map[string]importInfoCacheEntry
	browserQueue           chan BrowserJob
	pendingBrowserJobs     map[string]*pendingBrowserJob
	pendingBrowserJobsMu   sync.Mutex
}

func New(st *store.Store, cfg *config.Config) *Server {
	s := &Server{
		Store:              st,
		Cfg:                cfg,
		queuedJobs:         map[string]struct{}{},
		jobCancels:         map[string]context.CancelFunc{},
		previewCache:       make(map[string]previewCacheEntry),
		importInfoCache:    make(map[string]importInfoCacheEntry),
		browserQueue:       make(chan BrowserJob, 64),
		pendingBrowserJobs: make(map[string]*pendingBrowserJob),
	}
	s.DownloaderFactory = func() *noveldownloader.Downloader {
		directClient := noveldownloader.NewHTTPClient()

		// Always wrap with the lazy fallback client. It checks for an
		// available browser worker per-request, so it transparently starts
		// using the proxy the moment a worker connects — even mid-job — and
		// adds no overhead when none is connected.
		checker := NewBrowserWorkerChecker(s)
		client := noveldownloader.NewLazyFallbackClient(directClient, checker)

		dl := noveldownloader.NewDownloaderWithClient(client)
		if cfg != nil {
			if cfg.DownloadMinDelayMs > 0 {
				dl.MinChapterDelay = time.Duration(cfg.DownloadMinDelayMs) * time.Millisecond
			}
			if cfg.DownloadMaxDelayMs > 0 {
				dl.MaxChapterDelay = time.Duration(cfg.DownloadMaxDelayMs) * time.Millisecond
			}
		}
		return dl
	}
	s.startJobWorker()
	go s.processBrowserJobs()
	return s
}

func (s *Server) registerJobCancel(jobID string, cancel context.CancelFunc) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.jobCancels[jobID] = cancel
}

func (s *Server) unregisterJobCancel(jobID string) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	delete(s.jobCancels, jobID)
}

func (s *Server) cancelJob(jobID string) {
	s.cancelMu.Lock()
	cancel := s.jobCancels[jobID]
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func Router(s *Server) http.Handler {
	router, err := apis.NewRouter(s.Store.App)
	if err != nil {
		panic(err)
	}
	registerRoutes(router, s)
	mux, err := router.BuildMux()
	if err != nil {
		panic(err)
	}
	return mux
}

func registerRoutes(router *pbrouter.Router[*core.RequestEvent], s *Server) {
	router.GET("/healthz", func(e *core.RequestEvent) error {
		return e.JSON(http.StatusOK, map[string]any{"ok": true})
	})

	router.GET("/api/browser-workers", func(e *core.RequestEvent) error {
		browserWorkersMu.RLock()
		workers := make([]map[string]any, 0, len(browserWorkers))
		for _, w := range browserWorkers {
			w.mu.Lock()
			workers = append(workers, map[string]any{
				"id":            w.ID,
				"browser":       w.Browser,
				"version":       w.Version,
				"state":         w.State,
				"capabilities":  w.Capabilities,
				"connectedAt":   w.ConnectedAt,
				"lastHeartbeat": w.LastHeartbeat,
			})
			w.mu.Unlock()
		}
		browserWorkersMu.RUnlock()
		return e.JSON(http.StatusOK, map[string]any{
			"count":   len(workers),
			"workers": workers,
		})
	})

	router.GET("/api/proxy/status", func(e *core.RequestEvent) error {
		browserWorkersMu.RLock()
		workers := make([]map[string]any, 0, len(browserWorkers))
		for _, w := range browserWorkers {
			w.mu.Lock()
			workers = append(workers, map[string]any{
				"id":          w.ID,
				"browser":     w.Browser,
				"state":       w.State,
				"connectedAt": w.ConnectedAt,
			})
			w.mu.Unlock()
		}
		browserWorkersMu.RUnlock()
		return e.JSON(http.StatusOK, map[string]any{
			"connected": len(workers) > 0,
			"count":     len(workers),
			"workers":   workers,
		})
	})

	router.POST("/api/proxy/fetch", func(e *core.RequestEvent) error {
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

		if !s.HasBrowserWorker() {
			return e.BadRequestError("no browser worker connected", nil)
		}

		result, err := s.fetchViaBrowserWorker(body.URL, timeout, "")
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

	router.GET("/ws/browser-worker", func(e *core.RequestEvent) error {
		s.handleBrowserWorkerWS(e.Response, e.Request)
		return nil
	})

	registerAuthRoutes(router, s)
	registerWorkerAuthPublicRoutes(router, s)
	registerProtectedRoutes(router, s)
	registerStaticHandler(router, s.Cfg.StaticDir)
}

func registerProtectedRoutes(router *pbrouter.Router[*core.RequestEvent], s *Server) {
	api := router.Group("/api")
	api.Bind(loadAuthFromCookie())
	api.Bind(apis.RequireAuth())

	registerWorkerAuthProtectedRoutes(api, s)
	registerSettingsRoutes(api, s)
	registerProviderRoutes(api, s)
	registerPromptRoutes(api, s)
	registerImportRoutes(api, s)
	registerNovelRoutes(api, s)
	registerChapterRoutes(api, s)
	registerJobRoutes(api, s)
	registerEpubRoutes(api, s)
	registerEpubExportRoutes(api, s)
	registerReadingProgressRoutes(api, s)
	registerBackupRoutes(api, s)
}
