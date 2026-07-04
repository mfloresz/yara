package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
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

func registerWorkerAuthPublicRoutes(router *pbrouter.Router[*core.RequestEvent], s *Server) {
	router.GET("/api/worker-auth/authorize", func(e *core.RequestEvent) error {
		extensionID := e.Request.URL.Query().Get("extension_id")
		if extensionID == "" {
			return e.BadRequestError("extension_id is required", nil)
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

	router.GET("/api/worker-auth/validate", func(e *core.RequestEvent) error {
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
}

func registerWorkerAuthProtectedRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
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

		label := fmt.Sprintf("Chrome Extension (%s)", pending.ExtensionID[:8])
		_, plaintext, err := s.Store.CreateWorkerToken(e.Auth.Id, pending.ExtensionID, label)
		if err != nil {
			return e.InternalServerError("failed to create token", err)
		}

		callbackURL := fmt.Sprintf("chrome-extension://%s/auth.html?token=%s&user=%s", pending.ExtensionID, plaintext, e.Auth.Id)
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
		return e.JSON(http.StatusOK, map[string]any{"ok": true})
	})

	authGroup.POST("/delete/{id}", func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.BadRequestError("authentication required", nil)
		}
		tokenID := e.Request.PathValue("id")
		if err := s.Store.DeleteWorkerToken(e.Auth.Id, tokenID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		return e.JSON(http.StatusOK, map[string]any{"ok": true})
	})

	authGroup.GET("/tokens", func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.BadRequestError("authentication required", nil)
		}
		tokens, err := s.Store.ListWorkerTokens(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to list tokens", err)
		}
		return e.JSON(http.StatusOK, map[string]any{
			"tokens": tokens,
			"count":  len(tokens),
		})
	})
}

var consentPageTmpl = template.Must(template.New("consent").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Autorizar Conexión</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #0f0f0f;
            color: #e0e0e0;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .card {
            background: #1a1a1a;
            border: 1px solid #2a2a2a;
            border-radius: 12px;
            padding: 32px;
            max-width: 420px;
            width: 90%;
        }
        h1 {
            font-size: 20px;
            font-weight: 600;
            margin-bottom: 16px;
            color: #fff;
        }
        .info {
            background: #252525;
            border-radius: 8px;
            padding: 16px;
            margin-bottom: 20px;
        }
        .info-row {
            display: flex;
            justify-content: space-between;
            margin-bottom: 8px;
        }
        .info-row:last-child { margin-bottom: 0; }
        .info-label { color: #888; font-size: 13px; }
        .info-value { color: #fff; font-size: 13px; font-family: monospace; }
        .permissions {
            margin-bottom: 24px;
            font-size: 14px;
            color: #aaa;
            line-height: 1.6;
        }
        .permissions ul {
            margin-top: 8px;
            padding-left: 20px;
        }
        .buttons {
            display: flex;
            gap: 12px;
        }
        .btn {
            flex: 1;
            padding: 10px 16px;
            border-radius: 8px;
            border: none;
            font-size: 14px;
            font-weight: 500;
            cursor: pointer;
            transition: background 0.2s;
        }
        .btn-cancel {
            background: #2a2a2a;
            color: #aaa;
        }
        .btn-cancel:hover { background: #333; }
        .btn-approve {
            background: #3b82f6;
            color: #fff;
        }
        .btn-approve:hover { background: #2563eb; }
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
        <form method="POST" action="/api/worker-auth/approve">
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
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #0f0f0f;
            color: #e0e0e0;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .card {
            background: #1a1a1a;
            border: 1px solid #2a2a2a;
            border-radius: 12px;
            padding: 32px;
            max-width: 420px;
            width: 90%;
            text-align: center;
        }
        .icon {
            width: 64px;
            height: 64px;
            background: #166534;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 20px;
        }
        .icon svg {
            width: 32px;
            height: 32px;
            stroke: #4ade80;
        }
        h1 {
            font-size: 20px;
            font-weight: 600;
            margin-bottom: 8px;
            color: #fff;
        }
        p {
            font-size: 14px;
            color: #888;
            margin-bottom: 20px;
        }
        .label {
            background: #252525;
            border-radius: 8px;
            padding: 12px;
            font-size: 13px;
            color: #aaa;
            margin-bottom: 20px;
        }
        .btn {
            display: inline-block;
            padding: 10px 24px;
            background: #3b82f6;
            color: #fff;
            border: none;
            border-radius: 8px;
            font-size: 14px;
            font-weight: 500;
            cursor: pointer;
            text-decoration: none;
        }
        .btn:hover { background: #2563eb; }
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
        <div class="label">Puedes cerrar esta pestaña.</div>
        <a href="{{.CallbackURL}}" class="btn">Volver a la Extensión</a>
    </div>
    <script>
        setTimeout(function() { window.location.href = "{{.CallbackURL}}"; }, 1500);
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
