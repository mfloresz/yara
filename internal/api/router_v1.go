package api

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
)

// registerV1Routes mounts the canonical /api/v1/* API. Each v1 route delegates
// to a shared handler in the corresponding router_*.go file so the underlying
// logic (store calls, queueing, etc.) is not duplicated. All responses use the
// {data,meta,links} v1 envelope; deletes return 204 No Content.
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

	// Admin panel surface. Every route requires the admin role.
	registerV1AdminRoutes(authed.Group("/admin").Bind(requireAdmin()), s)
}

func registerV1AuthRoutes(v1 *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	auth := v1.Group("/auth")
	auth.POST("/register", handleAuthRegister(s))
	auth.POST("/login", handleAuthLogin(s))
	registerV1InvitationPublicRoutes(auth, s)

	authed := auth.Group("")
	authed.Bind(loadAuthFromCookie())
	authed.Bind(apis.RequireAuth())
	authed.GET("/me", handleAuthMe(s))
	authed.POST("/refresh", handleAuthRefresh(s))
	authed.POST("/logout", handleAuthLogout(s))
}
