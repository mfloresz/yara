package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/ai"
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
	DownloaderFactory      func(userID string) *noveldownloader.Downloader
	previewCacheMu         sync.RWMutex
	previewCache           map[string]previewCacheEntry
	importInfoCacheMu      sync.RWMutex
	importInfoCache        map[string]importInfoCacheEntry
	browserQueue           chan BrowserJob
	pendingBrowserJobs     map[string]*pendingBrowserJob
	pendingBrowserJobsMu   sync.Mutex
	// redownloadLocks serializes the check+create+enqueue sequence of
	// redownload-from-url per novel, so two concurrent requests cannot both pass
	// the active-jobs check and create competing redownload jobs.
	redownloadLocks sync.Map
	// NewAIProvider allows tests to inject a mock provider.
	NewAIProvider func(store.AISettings) (ai.Provider, error)
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
	s.DownloaderFactory = func(userID string) *noveldownloader.Downloader {
		directClient := noveldownloader.NewHTTPClient()

		// Always wrap with the lazy fallback client. It checks for an
		// available browser worker per-request, so it transparently starts
		// using the proxy the moment a worker connects — even mid-job — and
		// adds no overhead when none is connected. The checker is scoped to
		// the owning user so proxy fetches only ever run on that user's own
		// connected browser worker.
		checker := NewBrowserWorkerChecker(s, userID)
		client := noveldownloader.NewLazyFallbackClient(directClient, checker)

		dl := noveldownloader.NewDownloaderWithClient(client)
		dl.MinChapterDelay = noveldownloader.DefaultMinChapterDelay
		dl.MaxChapterDelay = noveldownloader.DefaultMaxChapterDelay
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

// lockNovel returns an unlock function holding the per-novel redownload lock.
func (s *Server) lockNovel(novelID string) func() {
	mu, _ := s.redownloadLocks.LoadOrStore(novelID, &sync.Mutex{})
	lock := mu.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
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
	// Versioning middleware: adds X-API-Version on /api/v1/* responses and
	// Deprecation/Sunset/Link headers on legacy /api/db/*, /api/user/*,
	// /api/epubs/*, /api/backup/*, /api/proxy/*, /api/defaults so clients
	// can migrate at their own pace. Must run before any handler.
	router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "v1HeaderMiddleware",
		Func: func(e *core.RequestEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			path := e.Request.URL.Path
			switch {
			case hasPrefix(path, "/api/v1/"):
				e.Response.Header().Set("X-API-Version", "v1")
			case hasPrefix(path, "/api/db/"), hasPrefix(path, "/api/user/"), path == "/api/epubs" || hasPrefix(path, "/api/epubs/"),
				hasPrefix(path, "/api/backup/"), hasPrefix(path, "/api/browser-workers"), hasPrefix(path, "/api/proxy/"),
				path == "/api/defaults", hasPrefix(path, "/api/translation-jobs"):
				e.Response.Header().Set("Deprecation", "true")
				e.Response.Header().Set("Sunset", "Wed, 01 Jan 2026 00:00:00 GMT")
				e.Response.Header().Set("Link", `</api/v1>; rel="successor-version"`)
			}
			return nil
		},
	})

	router.GET("/healthz", func(e *core.RequestEvent) error {
		return e.JSON(http.StatusOK, map[string]any{"ok": true})
	})

	// The browser-worker WebSocket must be reachable before the worker has a
	// token (it authenticates in-band via a `register` message validated
	// against ValidateWorkerToken; unauthenticated workers never get dispatched
	// a job). The browser-worker status and proxy-fetch endpoints, by contrast,
	// are registered on the authenticated group in registerProtectedRoutes so
	// that anonymous callers cannot enumerate connected workers or drive them
	// to fetch arbitrary URLs (SSRF).
	router.GET("/ws/browser-worker", func(e *core.RequestEvent) error {
		s.handleBrowserWorkerWS(e.Response, e.Request)
		return nil
	})

	registerAuthRoutes(router, s)
	registerWorkerAuthPublicRoutes(router, s)
	registerProtectedRoutes(router, s)
	registerV1Routes(router, s)
	registerStaticHandler(router, s.Cfg.StaticDir)
}

func registerProtectedRoutes(router *pbrouter.Router[*core.RequestEvent], s *Server) {
	api := router.Group("/api")
	api.Bind(loadAuthFromCookie())
	api.Bind(apis.RequireAuth())

	registerWorkerAuthProtectedRoutes(api, s)
	registerProxyRoutes(api, s)
	registerSettingsRoutes(api, s)
	registerProviderRoutes(api, s)
	registerPromptRoutes(api, s)
	registerImportRoutes(api, s)
	registerNovelRoutes(api, s)
	registerChapterRoutes(api, s)
	registerJobRoutes(api, s)
	registerEpubRoutes(api, s)
	registerGlossaryRoutes(api, s)
	registerEpubExportRoutes(api, s)
	registerReadingProgressRoutes(api, s)
	registerBackupRoutes(api, s)
}
