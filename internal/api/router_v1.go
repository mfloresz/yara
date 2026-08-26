package api

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
)

// registerV1Routes mounts the canonical /api/v1/* API. Each v1 route delegates
// to a shared handler in the corresponding router_*.go file so the underlying
// logic (store calls, queueing, etc.) is not duplicated. The only per-route
// changes are: response envelope (legacy uses {items,hasMore} or bare arrays;
// v1 uses {data,meta,links}) and status code (legacy returns 200 for deletes,
// v1 returns 204).
//
// Legacy paths under /api/db/*, /api/user/*, /api/epubs/*, /api/backup/*,
// /api/browser-workers, /api/proxy/*, /api/defaults continue to work and
// receive Deprecation / Sunset / Link headers via v1HeaderMiddleware so
// clients can migrate at their own pace.
func registerV1Routes(router *pbrouter.Router[*core.RequestEvent], s *Server) {
	v1 := router.Group("/api/v1")

	// Public auth (no /api/v1/auth requires the cookie to be a valid token;
	// these match the public /api/auth/* layout).
	registerV1AuthRoutes(v1, s)

	// Everything else requires an authenticated user.
	authed := v1.Group("")
	authed.Bind(loadAuthFromCookie())
	authed.Bind(apis.RequireAuth())

	registerV1NovelRoutes(authed, s)
	registerV1ChapterRoutes(authed, s)
	registerV1JobRoutes(authed, s)
	registerV1ImportRoutes(authed, s)
	registerV1EpubRoutes(authed, s)
	registerV1EpubExportRoutes(authed, s)
	registerV1GlossaryRoutes(authed, s)
	registerV1ReadingProgressRoutes(authed, s)
	registerV1PromptRoutes(authed, s)
	registerV1ProviderRoutes(authed, s)
	registerV1SettingsRoutes(authed, s)
	registerV1BackupRoutes(authed, s)
	registerV1ProxyRoutes(authed, s)
	registerV1WorkerAuthRoutes(authed, s)
}

func registerV1AuthRoutes(v1 *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	auth := v1.Group("/auth")
	auth.POST("/register", handleAuthRegister(s))
	auth.POST("/login", handleAuthLogin(s))

	authed := auth.Group("")
	authed.Bind(loadAuthFromCookie())
	authed.Bind(apis.RequireAuth())
	authed.GET("/me", handleAuthMe(s))
	authed.POST("/refresh", handleAuthRefresh(s))
	authed.POST("/logout", handleAuthLogout(s))
}
