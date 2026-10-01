package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/ai"
	"translator-server/internal/config"
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
	Store   *store.Store
	Cfg     *config.Config
	Version string
	// Job scheduler state, guarded by jobMu: one admission point
	// (enqueueJob), a FIFO pending queue per class (pendingAI/pendingWeb),
	// exclusive resource keys (reservedKeys, one holder per key) and per-class
	// capacity (runningAI/runningWeb). queuedJobs dedups job IDs across
	// waiting AND running. PocketBase access never happens under jobMu — see
	// dispatchJobs/buildJobRunPlan in runtime_scheduler.go.
	jobMu        sync.Mutex
	pendingAI    []pendingJob
	pendingWeb   []pendingJob
	queuedJobs   map[string]struct{}
	reservedKeys map[string]string
	runningJobs  map[string]struct{}
	runningAI    int
	runningWeb   int
	stopping     bool
	// dispatchDisabled makes enqueueJob park jobs without launching them and
	// StopJobWorker drop pending jobs instead of draining. Tests set it to
	// exercise admission/queueing without executing downloads; production
	// code never touches it.
	dispatchDisabled bool
	workerWG         sync.WaitGroup
	cancelMu         sync.Mutex
	jobCancels       map[string]context.CancelFunc
	// ParserHTTPClientFactory overrides the direct HTTP client parser scripts
	// fetch through. Tests set it to rewrite site hosts onto an httptest
	// server; production leaves it nil and gets a plain client.
	ParserHTTPClientFactory func(userID string) *http.Client
	// BrowserJobEnqueuer overrides EnqueueBrowserJob for the parser fetcher's
	// worker relay. Tests set it to stub the connected browser worker;
	// production leaves it nil and jobs go through the real queue.
	BrowserJobEnqueuer func(operation, url string, params map[string]interface{}, userID string) (*BrowserWorkerJobResult, error)
	// parserThrottle spaces parser fetches per site (URL host). It is shared
	// rather than per-job because each script reload builds a new fetcher.
	// Bounds are set once here from the config and never mutated afterwards
	// (see setDelays, tests only), so concurrent fetchers share it race-free.
	parserThrottle *parseThrottle
	// parserUpdates caches the release manifest for parser auto-update and
	// serializes parser file replacement (see parser_update.go). Value field:
	// the zero value is ready to use.
	parserUpdates parserUpdateCache
	// probeCache memoizes read-only probe outcomes (canUpdate,
	// requiresBrowser) per URL host so the novel list does not recompile
	// every script per row. Lazily initialized; see cachedProbe/storeProbe.
	// Execution paths always resolve fresh and never consult it.
	probeCacheMu         sync.Mutex
	probeCache           map[string]probeCacheEntry
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
	// agentLimiter caps agent chat turns per client IP: each turn spends
	// multiple model calls plus tool executions.
	agentLimiter *rateLimiter
	// agentTurnLocks serializes agent chat turns per user so two concurrent
	// chats cannot interleave history reads/writes on the same session.
	// Entries are reference-counted and removed once idle: keyed by user id
	// the map would otherwise grow for the process lifetime, one entry per
	// account that ever chatted, on a long-lived self-hosted instance.
	agentTurnLocks sync.Map // userID -> *agentTurnLock
	// NewAIProvider allows tests to inject a mock provider.
	NewAIProvider func(store.AISettings, string) (ai.Provider, error)
}

func New(st *store.Store, cfg *config.Config) *Server {
	minDelayMs, maxDelayMs := 0, 0
	if cfg != nil {
		minDelayMs, maxDelayMs = cfg.DownloadMinDelayMs, cfg.DownloadMaxDelayMs
	}
	s := &Server{
		Store:              st,
		Cfg:                cfg,
		Version:            "dev",
		queuedJobs:         map[string]struct{}{},
		reservedKeys:       map[string]string{},
		runningJobs:        map[string]struct{}{},
		jobCancels:         map[string]context.CancelFunc{},
		previewCache:       make(map[string]previewCacheEntry),
		importInfoCache:    make(map[string]importInfoCacheEntry),
		browserQueue:       make(chan BrowserJob, 64),
		pendingBrowserJobs: make(map[string]*pendingBrowserJob),
		loginLimiter:       newRateLimiter(5, 5),   // 5 attempts per minute per IP
		invitationLimiter:  newRateLimiter(10, 10), // 10 redemptions per minute per IP
		wsLimiter:          newRateLimiter(16, 16), // 16 WS upgrades per minute per IP
		parserThrottle:     newParseThrottle(minDelayMs, maxDelayMs),
		globalLimiter:      newRateLimiter(600, 600), // global backstop: 600 requests per minute per IP
		agentLimiter:       newRateLimiter(10, 20),   // agent chat: burst 10, 20 turns per minute per IP
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
