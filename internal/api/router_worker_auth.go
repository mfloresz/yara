package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
)

type pendingAuth struct {
	ExtensionID string
	UserID      string
	State       string
	CreatedAt   time.Time
}

var (
	pendingAuths   = make(map[string]*pendingAuth)
	pendingAuthsMu sync.Mutex
)

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// registerWorkerAuthPublicRoutes registers the public /api/v1/worker-auth/*
// flow used by the browser extension to authorize and connect. These routes
// are public (no auth required) because the extension opens them in a tab
// before the user has been authenticated; the user is verified via cookie
// inside the handler before the consent page is shown.
func registerWorkerAuthPublicRoutes(router *pbrouter.Router[*core.RequestEvent], s *Server) {
	router.GET("/api/v1/worker-auth/authorize", func(e *core.RequestEvent) error {
		extensionID := e.Request.URL.Query().Get("extension_id")
		if extensionID == "" {
			return e.BadRequestError("extension_id is required", nil)
		}
		// Chrome extension IDs are 32 chars. Reject anything implausibly short
		// so the label (which slices ExtensionID[:8]) can never panic.
		if len(extensionID) < 8 {
			return e.BadRequestError("invalid extension_id", nil)
		}

		cookie, err := e.Request.Cookie(authCookieName)
		if err != nil || cookie.Value == "" {
			return e.HTML(http.StatusOK, loginRequiredHTML(authorizeURL(extensionID)))
		}
		if _, err := e.App.FindAuthRecordByToken(cookie.Value, core.TokenTypeAuth); err != nil {
			return e.HTML(http.StatusOK, loginRequiredHTML(authorizeURL(extensionID)))
		}

		state := generateState()
		pendingAuthsMu.Lock()
		pendingAuths[state] = &pendingAuth{
			ExtensionID: extensionID,
			State:       state,
			CreatedAt:   time.Now(),
		}
		pendingAuthsMu.Unlock()

		page := consentPageHTML(extensionID, state)
		e.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		e.Response.WriteHeader(http.StatusOK)
		e.Response.Write([]byte(page))
		return nil
	})

	router.GET("/api/v1/worker-auth/validate", func(e *core.RequestEvent) error {
		token := e.Request.Header.Get("Authorization")
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
		if token == "" {
			token = e.Request.URL.Query().Get("token")
		}
		if token == "" {
			return e.BadRequestError("token required", nil)
		}

		validated, err := s.Store.ValidateWorkerToken(token)
		if err != nil {
			return e.UnauthorizedError("invalid token", err)
		}

		return e.JSON(http.StatusOK, map[string]any{
			"valid":       true,
			"userId":      validated.UserID,
			"extensionId": validated.ExtensionID,
			"label":       validated.Label,
		})
	})

	router.GET("/api/v1/worker-auth/callback", func(e *core.RequestEvent) error {
		token := e.Request.URL.Query().Get("token")
		userID := e.Request.URL.Query().Get("user")
		if token == "" || userID == "" {
			return e.BadRequestError("missing token or user", nil)
		}

		e.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		e.Response.WriteHeader(http.StatusOK)
		e.Response.Write([]byte(callbackSuccessHTML(token, userID)))
		return nil
	})
}

// registerV1WorkerAuthRoutes registers the authenticated /api/v1/worker-auth/*
// routes (approve, revoke, delete, tokens). The public flow lives in
// registerWorkerAuthPublicRoutes above because it must be reachable before
// the extension has a token.
func registerV1WorkerAuthRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	authGroup := api.Group("/worker-auth")
	authGroup.Bind(apis.RequireAuth())

	authGroup.POST("/approve", func(e *core.RequestEvent) error {
		state := e.Request.FormValue("state")
		if state == "" {
			return e.BadRequestError("state is required", nil)
		}

		pendingAuthsMu.Lock()
		pending, exists := pendingAuths[state]
		if exists {
			delete(pendingAuths, state)
		}
		pendingAuthsMu.Unlock()

		if !exists || time.Since(pending.CreatedAt) > 10*time.Minute {
			return e.BadRequestError("invalid or expired authorization request", nil)
		}

		if e.Auth == nil {
			return e.BadRequestError("authentication required", nil)
		}

		shortID := pending.ExtensionID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		label := fmt.Sprintf("Browser Extension (%s)", shortID)
		_, plaintext, err := s.Store.CreateWorkerToken(e.Auth.Id, pending.ExtensionID, label)
		if err != nil {
			return e.InternalServerError("failed to create token", err)
		}

		callbackURL := fmt.Sprintf("/api/v1/worker-auth/callback?token=%s&user=%s", plaintext, e.Auth.Id)
		e.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		e.Response.WriteHeader(http.StatusOK)
		page := approvalSuccessHTML(label, callbackURL)
		e.Response.Write([]byte(page))
		return nil
	})

	authGroup.POST("/revoke/{id}", func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.BadRequestError("authentication required", nil)
		}
		tokenID := e.Request.PathValue("id")
		if err := s.Store.RevokeWorkerToken(e.Auth.Id, tokenID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		CloseWorkerByTokenID(tokenID)
		return v1Respond(e, http.StatusOK, map[string]any{"ok": true}, nil, nil)
	})

	authGroup.POST("/delete/{id}", func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.BadRequestError("authentication required", nil)
		}
		tokenID := e.Request.PathValue("id")
		if err := s.Store.DeleteWorkerToken(e.Auth.Id, tokenID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		CloseWorkerByTokenID(tokenID)
		return v1Respond(e, http.StatusOK, map[string]any{"ok": true}, nil, nil)
	})

	authGroup.GET("/tokens", func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.BadRequestError("authentication required", nil)
		}
		tokens, err := s.Store.ListWorkerTokens(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to list tokens", err)
		}
		body := map[string]any{
			"tokens": tokens,
			"count":  len(tokens),
		}
		return v1Respond(e, http.StatusOK, body, nil, nil)
	})
}

var consentPageTmpl = template.Must(template.New("consent").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Autorizar Conexión</title>
    <meta name="color-scheme" content="light dark">
    <style>
        /* Quiet Shelf tokens — mirrors extensions/*/popup/popup.css */
        :root {
            color-scheme: light dark;
            --page: #f5f4f2;
            --surface: #fafaf9;
            --surface-alt: #e8e6e2;
            --border: #ddd9d3;
            --text: #141413;
            --text-muted: #57544c;
            --text-faint: #6f6b64;
            --success: #16a34a;
            --warn: #a16207;
            --danger: #dc2626;
            --radius-sm: 8px;
            --radius-md: 12px;
            --radius-lg: 16px;
            --radius-pill: 999px;
            --ease: 0.16s cubic-bezier(0.4, 0, 0.2, 1);
        }
        @media (prefers-color-scheme: dark) {
            :root {
                --page: #121110;
                --surface: #1b1a19;
                --surface-alt: #262523;
                --border: #3d3b35;
                --text: #f5f4f2;
                --text-muted: #b0aca4;
                --text-faint: #918d85;
                --success: #4ade80;
                --warn: #fbbf24;
                --danger: #f87171;
            }
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        ::selection { background: color-mix(in oklab, var(--text) 16%, transparent); }
        body {
            font-family: Geist, Inter, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            background: var(--page);
            color: var(--text);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 24px 16px;
            -webkit-font-smoothing: antialiased;
        }
        .card {
            background: var(--surface);
            border: 1px solid var(--border);
            border-radius: var(--radius-lg);
            padding: 28px;
            max-width: 420px;
            width: 100%;
        }
        h1 {
            font-size: 18px;
            font-weight: 600;
            letter-spacing: -0.01em;
            margin-bottom: 16px;
        }
        .info {
            background: var(--surface-alt);
            border: 1px solid var(--border);
            border-radius: var(--radius-sm);
            padding: 12px 14px;
            margin-bottom: 16px;
        }
        .info-row {
            display: flex;
            justify-content: space-between;
            align-items: baseline;
            gap: 12px;
            margin-bottom: 8px;
        }
        .info-row:last-child { margin-bottom: 0; }
        .info-label { color: var(--text-faint); font-size: 12px; font-weight: 500; }
        .info-value {
            color: var(--text-muted);
            font-size: 12px;
            font-family: 'SFMono-Regular', ui-monospace, SFMono, Menlo, monospace;
            word-break: break-all;
            text-align: right;
        }
        .permissions {
            margin-bottom: 20px;
            font-size: 13px;
            color: var(--text-muted);
            line-height: 1.55;
        }
        .permissions ul {
            margin-top: 8px;
            padding-left: 20px;
        }
        .buttons {
            display: flex;
            gap: 8px;
        }
        .btn {
            flex: 1;
            display: inline-flex;
            align-items: center;
            justify-content: center;
            min-height: 44px;
            padding: 0 16px;
            border: 1px solid transparent;
            border-radius: var(--radius-pill);
            font-family: inherit;
            font-size: 13px;
            font-weight: 600;
            cursor: pointer;
            text-decoration: none;
            transition: background var(--ease);
        }
        .btn:focus-visible {
            outline: 2px solid var(--text-muted);
            outline-offset: 2px;
        }
        .btn-cancel {
            background: var(--surface);
            border-color: var(--border);
            color: var(--text);
        }
        .btn-cancel:hover { background: var(--surface-alt); }
        .btn-approve {
            background: var(--text);
            color: var(--page);
            border: none;
        }
        .btn-approve:hover { background: color-mix(in oklab, var(--text) 85%, var(--page)); }
        @media (prefers-reduced-motion: reduce) {
            *, *::before, *::after { transition-duration: 0.01ms !important; }
        }
    </style>
</head>
<body>
    <div class="card">
        <h1>Autorizar Conexión</h1>
        <div class="info">
            <div class="info-row">
                <span class="info-label">Extensión</span>
                <span class="info-value">{{.ExtensionID}}</span>
            </div>
        </div>
        <div class="permissions">
            Esto permitirá que la extensión:
            <ul>
                <li>Descargue páginas web por ti</li>
                <li>Acceda a tu sesión de usuario</li>
            </ul>
        </div>
        <form method="POST" action="/api/v1/worker-auth/approve">
            <input type="hidden" name="state" value="{{.State}}">
            <div class="buttons">
                <button type="button" class="btn btn-cancel" onclick="window.close()">Cancelar</button>
                <button type="submit" class="btn btn-approve">Autorizar</button>
            </div>
        </form>
    </div>
</body>
</html>`))

func consentPageHTML(extensionID, state string) string {
	var buf bytes.Buffer
	consentPageTmpl.Execute(&buf, map[string]string{
		"ExtensionID": extensionID,
		"State":       state,
	})
	return buf.String()
}

var approvalSuccessTmpl = template.Must(template.New("success").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Conexión Autorizada</title>
    <meta http-equiv="refresh" content="1;url={{.CallbackURL}}">
    <meta name="color-scheme" content="light dark">
    <style>
        /* Quiet Shelf tokens — mirrors extensions/*/popup/popup.css */
        :root {
            color-scheme: light dark;
            --page: #f5f4f2;
            --surface: #fafaf9;
            --surface-alt: #e8e6e2;
            --border: #ddd9d3;
            --text: #141413;
            --text-muted: #57544c;
            --text-faint: #6f6b64;
            --success: #16a34a;
            --ease: 0.16s cubic-bezier(0.4, 0, 0.2, 1);
        }
        @media (prefers-color-scheme: dark) {
            :root {
                --page: #121110;
                --surface: #1b1a19;
                --surface-alt: #262523;
                --border: #3d3b35;
                --text: #f5f4f2;
                --text-muted: #b0aca4;
                --text-faint: #918d85;
                --success: #4ade80;
            }
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        ::selection { background: color-mix(in oklab, var(--text) 16%, transparent); }
        body {
            font-family: Geist, Inter, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            background: var(--page);
            color: var(--text);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 24px 16px;
            -webkit-font-smoothing: antialiased;
        }
        .card {
            background: var(--surface);
            border: 1px solid var(--border);
            border-radius: 16px;
            padding: 28px;
            max-width: 420px;
            width: 100%;
            text-align: center;
        }
        .icon {
            width: 56px;
            height: 56px;
            background: color-mix(in oklab, var(--success) 14%, var(--surface));
            border: 1px solid color-mix(in oklab, var(--success) 32%, var(--border));
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 16px;
            color: var(--success);
        }
        .icon svg {
            width: 26px;
            height: 26px;
            stroke: currentColor;
        }
        h1 {
            font-size: 18px;
            font-weight: 600;
            letter-spacing: -0.01em;
            margin-bottom: 8px;
        }
        p {
            font-size: 13px;
            color: var(--text-muted);
            line-height: 1.55;
            margin-bottom: 16px;
        }
        .label {
            background: var(--surface-alt);
            border: 1px solid var(--border);
            border-radius: 8px;
            padding: 12px;
            font-size: 12.5px;
            color: var(--text-muted);
            line-height: 1.55;
            margin-bottom: 0;
        }
        .label a {
            color: var(--text);
            text-underline-offset: 3px;
        }
        .btn {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            min-height: 44px;
            padding: 0 24px;
            background: var(--text);
            color: var(--page);
            border: none;
            border-radius: 999px;
            font-family: inherit;
            font-size: 13px;
            font-weight: 600;
            cursor: pointer;
            text-decoration: none;
            transition: background var(--ease);
        }
        .btn:hover { background: color-mix(in oklab, var(--text) 85%, var(--page)); }
        .btn:focus-visible {
            outline: 2px solid var(--text-muted);
            outline-offset: 2px;
        }
        @media (prefers-reduced-motion: reduce) {
            *, *::before, *::after { transition-duration: 0.01ms !important; }
        }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon">
            <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="20 6 9 17 4 12"></polyline>
            </svg>
        </div>
        <h1>Conexión Autorizada</h1>
        <p>{{.Label}} está conectada a tu cuenta.</p>
        <div class="label">Redirigiendo… si no avanza, <a class="btn" href="{{.CallbackURL}}">continuar</a>.</div>
    </div>
    <script>
        setTimeout(function() { window.location.href = "{{.CallbackURL}}"; }, 1000);
    </script>
</body>
</html>`))

func approvalSuccessHTML(label, callbackURL string) string {
	var buf bytes.Buffer
	approvalSuccessTmpl.Execute(&buf, map[string]string{
		"Label":       label,
		"CallbackURL": callbackURL,
	})
	return buf.String()
}

// authorizeURL rebuilds the authorize entry point so the login page can send
// the user back to the consent screen instead of dropping the extension_id.
func authorizeURL(extensionID string) string {
	return "/api/v1/worker-auth/authorize?extension_id=" + url.QueryEscape(extensionID)
}

var loginRequiredTmpl = template.Must(template.New("loginRequired").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Sesión Requerida</title>
    <meta name="color-scheme" content="light dark">
    <style>
        /* Quiet Shelf tokens — mirrors extensions/*/popup/popup.css */
        :root {
            color-scheme: light dark;
            --page: #f5f4f2;
            --surface: #fafaf9;
            --border: #ddd9d3;
            --text: #141413;
            --text-muted: #57544c;
            --warn: #a16207;
            --ease: 0.16s cubic-bezier(0.4, 0, 0.2, 1);
        }
        @media (prefers-color-scheme: dark) {
            :root {
                --page: #121110;
                --surface: #1b1a19;
                --border: #3d3b35;
                --text: #f5f4f2;
                --text-muted: #b0aca4;
                --warn: #fbbf24;
            }
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        ::selection { background: color-mix(in oklab, var(--text) 16%, transparent); }
        body {
            font-family: Geist, Inter, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            background: var(--page);
            color: var(--text);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 24px 16px;
            -webkit-font-smoothing: antialiased;
        }
        .card {
            background: var(--surface);
            border: 1px solid var(--border);
            border-radius: 16px;
            padding: 28px;
            max-width: 420px;
            width: 100%;
            text-align: center;
        }
        .icon {
            width: 56px;
            height: 56px;
            background: color-mix(in oklab, var(--warn) 14%, var(--surface));
            border: 1px solid color-mix(in oklab, var(--warn) 32%, var(--border));
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 16px;
            color: var(--warn);
        }
        .icon svg {
            width: 26px;
            height: 26px;
            stroke: currentColor;
        }
        h1 {
            font-size: 18px;
            font-weight: 600;
            letter-spacing: -0.01em;
            margin-bottom: 8px;
        }
        p {
            font-size: 13px;
            color: var(--text-muted);
            margin-bottom: 12px;
            line-height: 1.55;
        }
        p:last-of-type { margin-bottom: 20px; }
        .btn {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            min-height: 44px;
            padding: 0 24px;
            background: var(--text);
            color: var(--page);
            border: none;
            border-radius: 999px;
            font-family: inherit;
            font-size: 13px;
            font-weight: 600;
            cursor: pointer;
            text-decoration: none;
            transition: background var(--ease);
        }
        .btn:hover { background: color-mix(in oklab, var(--text) 85%, var(--page)); }
        .btn:focus-visible {
            outline: 2px solid var(--text-muted);
            outline-offset: 2px;
        }
        @media (prefers-reduced-motion: reduce) {
            *, *::before, *::after { transition-duration: 0.01ms !important; }
        }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon">
            <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
                <path d="M7 11V7a5 5 0 0 1 10 0v4"></path>
            </svg>
        </div>
        <h1>Sesión Requerida</h1>
        <p>Debes iniciar sesión en Yara primero para autorizar la extensión del navegador.</p>
        <p>Después de iniciar sesión volverás automáticamente a la pantalla de autorización.</p>
        <a href="{{.LoginURL}}" class="btn">Iniciar Sesión</a>
    </div>
</body>
</html>`))

func loginRequiredHTML(authorizePath string) string {
	var buf bytes.Buffer
	loginURL := "/login?redirect=" + url.QueryEscape(authorizePath)
	loginRequiredTmpl.Execute(&buf, map[string]string{
		"LoginURL": loginURL,
	})
	return buf.String()
}

func callbackSuccessHTML(token, userID string) string {
	return `<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Autenticación Completa</title>
    <meta name="color-scheme" content="light dark">
    <style>
        /* Quiet Shelf tokens — mirrors extensions/*/popup/popup.css */
        :root {
            color-scheme: light dark;
            --page: #f5f4f2;
            --surface: #fafaf9;
            --border: #ddd9d3;
            --text: #141413;
            --text-muted: #57544c;
            --success: #16a34a;
        }
        @media (prefers-color-scheme: dark) {
            :root {
                --page: #121110;
                --surface: #1b1a19;
                --border: #3d3b35;
                --text: #f5f4f2;
                --text-muted: #b0aca4;
                --success: #4ade80;
            }
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        ::selection { background: color-mix(in oklab, var(--text) 16%, transparent); }
        body {
            font-family: Geist, Inter, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            background: var(--page);
            color: var(--text);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 24px 16px;
            -webkit-font-smoothing: antialiased;
        }
        .card {
            background: var(--surface);
            border: 1px solid var(--border);
            border-radius: 16px;
            padding: 28px;
            max-width: 420px;
            width: 100%;
            text-align: center;
        }
        .icon {
            width: 56px;
            height: 56px;
            background: color-mix(in oklab, var(--success) 14%, var(--surface));
            border: 1px solid color-mix(in oklab, var(--success) 32%, var(--border));
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 16px;
            color: var(--success);
        }
        .icon svg {
            width: 26px;
            height: 26px;
            stroke: currentColor;
        }
        h1 {
            font-size: 18px;
            font-weight: 600;
            letter-spacing: -0.01em;
            margin-bottom: 8px;
        }
        p {
            font-size: 13px;
            color: var(--text-muted);
            line-height: 1.55;
            margin-bottom: 0;
        }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon">
            <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="20 6 9 17 4 12"></polyline>
            </svg>
        </div>
        <h1>Autenticación Completa</h1>
        <p>La extensión del navegador está conectada. Puedes cerrar esta pestaña.</p>
    </div>
    <script>setTimeout(function() { window.close(); }, 2000);</script>
</body>
</html>`
}

func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			pendingAuthsMu.Lock()
			for state, auth := range pendingAuths {
				if time.Since(auth.CreatedAt) > 10*time.Minute {
					delete(pendingAuths, state)
				}
			}
			pendingAuthsMu.Unlock()
		}
	}()
}
