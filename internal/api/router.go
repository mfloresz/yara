package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
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
	Store                *store.Store
	Cfg                  *config.Config
	Version              string
	downloadQueue        chan string
	translateQueue       chan string
	workerWG             sync.WaitGroup
	queuedJobs           map[string]struct{}
	queueMu              sync.Mutex
	cancelMu             sync.Mutex
	jobCancels           map[string]context.CancelFunc
	DownloaderFactory    func(userID string) *noveldownloader.Downloader
	previewCacheMu       sync.RWMutex
	previewCache         map[string]previewCacheEntry
	importInfoCacheMu    sync.RWMutex
	importInfoCache      map[string]importInfoCacheEntry
	browserQueue         chan BrowserJob
	pendingBrowserJobs   map[string]*pendingBrowserJob
	pendingBrowserJobsMu sync.Mutex
	// redownloadLocks serializes the check+create+enqueue sequence of
	// redownload-from-url per novel, so two concurrent requests cannot both pass
	// the active-jobs check and create competing redownload jobs. Reused for
	// chapter reorder/visibility/bulk-exclude which must also not race
	// translate/download jobs.
	redownloadLocks sync.Map
	// bootstrapMu serializes the first-registration check+promote sequence so
	// two concurrent registers on a fresh install cannot both observe an empty
	// users table and both become admin (there is nothing to demote-guard
	// against at that point, but exactly one admin is the invariant).
	bootstrapMu sync.Mutex
	// invitationMu serializes invitation redemption: the check-then-create
	// sequence in accept must not race a second accept of the same token.
	invitationMu sync.Mutex
	// Rate limiters for the internet-exposed auth surface.
	loginLimiter      *rateLimiter
	invitationLimiter *rateLimiter
	// wsLimiter caps WebSocket upgrade attempts per client IP so one peer
	// cannot reconnect in a loop and starve the maxUnauthenticatedWorkers
	// slots that legitimate browser workers need.
	wsLimiter *rateLimiter
	// globalLimiter is the backstop under the endpoint-specific limiters: it
	// caps total requests per client IP across every route, covering the
	// authenticated surface and PocketBase's native record CRUD, which have
	// no per-endpoint limit of their own.
	globalLimiter *rateLimiter
	// NewAIProvider allows tests to inject a mock provider.
	NewAIProvider func(store.AISettings, string) (ai.Provider, error)
}

func New(st *store.Store, cfg *config.Config) *Server {
	s := &Server{
		Store:              st,
		Cfg:                cfg,
		Version:            "dev",
		queuedJobs:         map[string]struct{}{},
		jobCancels:         map[string]context.CancelFunc{},
		previewCache:       make(map[string]previewCacheEntry),
		importInfoCache:    make(map[string]importInfoCacheEntry),
		browserQueue:       make(chan BrowserJob, 64),
		pendingBrowserJobs: make(map[string]*pendingBrowserJob),
		loginLimiter:       newRateLimiter(5, 5),     // 5 attempts per minute per IP
		invitationLimiter:  newRateLimiter(10, 10),   // 10 redemptions per minute per IP
		wsLimiter:          newRateLimiter(16, 16),   // 16 WS upgrades per minute per IP
		globalLimiter:      newRateLimiter(600, 600), // global backstop: 600 requests per minute per IP
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
	// Security headers for every response. PocketBase already sets
	// nosniff/X-Frame-Options; this overrides X-Frame-Options to DENY and adds
	// CSP, Referrer-Policy, Permissions-Policy and HSTS (only over HTTPS, as
	// detected from X-Forwarded-Proto behind a reverse proxy / tunnel).
	router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "securityHeaders",
		Func: func(e *core.RequestEvent) error {
			header := e.Response.Header()
			header.Set("X-Frame-Options", "DENY")
			header.Set("Referrer-Policy", "no-referrer")
			header.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			header.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; worker-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
			if strings.HasPrefix(e.Request.Header.Get("X-Forwarded-Proto"), "https") || e.Request.TLS != nil {
				header.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			return e.Next()
		},
	})

	// Block the PocketBase superuser dashboard. The embedded PB app registers
	// its management routes for any request carrying superuser auth; when the
	// server is exposed through a tunnel those must never be reachable, so the
	// UI path always answers 404. Superuser API calls are additionally
	// IP-restricted to loopback at startup (see cmd/server/main.go).
	router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "blockSuperuserUI",
		Func: func(e *core.RequestEvent) error {
			if hasPrefix(e.Request.URL.Path, "/_/") || e.Request.URL.Path == "/_" {
				return e.NotFoundError("", nil)
			}
			return e.Next()
		},
	})

	// Block PocketBase's native record-auth API. The app ships its own
	// rate-limited /api/v1/auth/* flow, and PocketBase's built-in rate limits
	// ship disabled (this app never enables them), so these routes would
	// otherwise be an unthrottled password brute-force path that bypasses
	// loginLimiter. Closing them also makes a superuser token unobtainable
	// over HTTP, which is the real guard for PocketBase's superuser-only
	// management routes (/api/settings, /api/backups, /api/logs, /api/crons,
	// collection management): the loopback SuperuserIPs whitelist set in
	// cmd/server/main.go is satisfied by every request that arrives through
	// cloudflared, so behind the tunnel it cannot be relied on by itself.
	router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "blockNativePocketBaseAuth",
		Func: func(e *core.RequestEvent) error {
			if isNativeAuthPath(e.Request.URL.Path) {
				return e.NotFoundError("", nil)
			}
			return e.Next()
		},
	})

	// Versioning middleware: sets X-API-Version on /api/v1/* responses.
	// Must run before any handler.
	router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "v1HeaderMiddleware",
		Func: func(e *core.RequestEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			if hasPrefix(e.Request.URL.Path, "/api/v1/") {
				e.Response.Header().Set("X-API-Version", "v1")
			}
			return nil
		},
	})

	// Global per-IP rate limit: the backstop under the endpoint-specific
	// auth limiters, covering every other route (authenticated API, native
	// record CRUD, static assets). Keys follow the same trust rules as the
	// auth limiter (clientKeyForRateLimit): behind cloudflared every real
	// client gets its own bucket; on a direct connection only the socket
	// address is trusted, so the key cannot be spoofed.
	router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "globalRateLimit",
		Func: func(e *core.RequestEvent) error {
			if !s.globalLimiter.allow(clientKeyForRateLimit(e.Request)) {
				slog.Warn("global rate limit exceeded", "path", e.Request.URL.Path)
				e.Response.Header().Set("Retry-After", "60")
				return writeV1Error(e, http.StatusTooManyRequests, "rate_limited", "too many requests, try again later")
			}
			return e.Next()
		},
	})

	router.GET("/healthz", func(e *core.RequestEvent) error {
		version := s.Version
		if version == "" {
			version = "dev"
		}
		return e.JSON(http.StatusOK, map[string]any{"ok": true, "version": version})
	})

	// The browser-worker WebSocket must be reachable before the worker has a
	// token (it authenticates in-band via a `register` message validated
	// against ValidateWorkerToken; unauthenticated workers never get dispatched
	// a job). The browser-worker status and proxy-fetch endpoints, by contrast,
	// are registered on the authenticated v1 group so that anonymous callers
	// cannot enumerate connected workers or drive them to fetch arbitrary URLs
	// (SSRF).
	router.GET("/ws/browser-worker", func(e *core.RequestEvent) error {
		s.handleBrowserWorkerWS(e.Response, e.Request)
		return nil
	})

	registerWorkerAuthPublicRoutes(router, s)
	registerV1Routes(router, s)
	registerStaticHandler(router, s.Cfg.StaticDir)
}

// isNativeAuthPath reports whether path belongs to PocketBase's native
// record-auth API (/api/collections/{collection}/{action} plus the global
// oauth2 redirect). Those routes are disabled by blockNativePocketBaseAuth:
// the app only authenticates through the rate-limited /api/v1/auth/* flow.
// The prefix match on "auth-" future-proofs new auth actions PocketBase may
// add; the explicit list covers the non-"auth-" members of the family
// (otp/password-reset/verification/email-change/impersonate).
func isNativeAuthPath(path string) bool {
	if path == "/api/oauth2-redirect" {
		return true
	}
	rest, ok := strings.CutPrefix(path, "/api/collections/")
	if !ok {
		return false
	}
	slash := strings.Index(rest, "/")
	if slash < 0 || slash == len(rest)-1 {
		return false
	}
	action := rest[slash+1:]
	if i := strings.Index(action, "/"); i >= 0 {
		action = action[:i] // strip any trailing /{id} segment (impersonate/{id})
	}
	switch action {
	case "auth-methods", "request-otp", "request-password-reset", "confirm-password-reset",
		"request-verification", "confirm-verification", "request-email-change",
		"confirm-email-change", "impersonate":
		return true
	}
	return strings.HasPrefix(action, "auth-")
}
