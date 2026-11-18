package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A real echo socket proves eviction closes the transport, not just its map entry.
func registerRevocationSocket(t *testing.T, id, userID, tokenID string) *websocket.Conn {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(kind, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	browserWorkersMu.Lock()
	browserWorkers[id] = &BrowserWorker{ID: id, UserID: userID, TokenID: tokenID, State: "authenticated", Conn: conn}
	browserWorkersMu.Unlock()
	t.Cleanup(func() {
		conn.Close()
		browserWorkersMu.Lock()
		delete(browserWorkers, id)
		browserWorkersMu.Unlock()
	})
	return conn
}

func revocationSocketUsable(conn *websocket.Conn) bool {
	conn.SetWriteDeadline(time.Now().Add(time.Second))
	if conn.WriteMessage(websocket.TextMessage, []byte("ping")) != nil {
		return false
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := conn.ReadMessage()
	return err == nil && string(data) == "ping"
}

func TestWorkerCredentialRevocationClosesEverySocket(t *testing.T) {
	for _, action := range []string{"revoke", "delete", "reset", "block"} {
		t.Run(action, func(t *testing.T) {
			env := newAPITestEnv(t)
			admin := bootstrapAdmin(t, env, "worker-admin@example.com")
			user := registerUser(t, env, "worker-user@example.com", "secret123", "User")
			token, raw, err := env.store.CreateWorkerToken(user.User.ID, "revocation", "Test")
			if err != nil {
				t.Fatal(err)
			}
			sockets := []*websocket.Conn{
				registerRevocationSocket(t, "revocation-1", user.User.ID, token.ID),
				registerRevocationSocket(t, "revocation-2", user.User.ID, token.ID),
			}
			other := registerRevocationSocket(t, "revocation-other", admin.User.ID, "unrelated-token")
			for _, conn := range sockets {
				if !revocationSocketUsable(conn) {
					t.Fatal("fixture socket unusable before action")
				}
			}
			switch action {
			case "revoke", "delete":
				assertStatus(t, doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/worker-auth/"+action+"/"+token.ID, user.Token, map[string]any{}), http.StatusOK)
			case "reset":
				_, reset, err := env.store.CreatePasswordReset(admin.User.ID, user.User.ID)
				if err != nil {
					t.Fatal(err)
				}
				assertStatus(t, doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/password-reset/accept", "", map[string]string{"token": reset, "password": "newsecret123"}), http.StatusOK)
			case "block":
				assertStatus(t, doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/admin/users/"+user.User.ID+"/block", admin.Token, map[string]any{}), http.StatusOK)
			}
			if _, err := env.store.ValidateWorkerToken(raw); err == nil {
				t.Fatal("credential remains valid")
			}
			for i, conn := range sockets {
				if revocationSocketUsable(conn) {
					t.Errorf("socket %d remains usable", i)
				}
			}
			if env.server.HasBrowserWorkerForUser(user.User.ID) {
				t.Error("revoked worker remains registered")
			}
			if !revocationSocketUsable(other) {
				t.Error("unrelated socket was closed")
			}
		})
	}
}

func TestCloseWorkerByTokenIDClosesEverySocket(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			tokenID := "direct-token"
			clients := make([]*websocket.Conn, 0, count)
			for i := 0; i < count; i++ {
				clients = append(clients, registerRevocationSocket(t, fmt.Sprintf("direct-%d", i), "direct-user", tokenID))
			}
			other := registerRevocationSocket(t, "direct-other", "direct-user", "different-token")
			CloseWorkerByTokenID(tokenID)
			for _, conn := range clients {
				if revocationSocketUsable(conn) {
					t.Error("revoked socket remains usable")
				}
			}
			browserWorkersMu.RLock()
			for _, worker := range browserWorkers {
				if worker.TokenID == tokenID {
					t.Error("revoked socket remains registered")
				}
			}
			browserWorkersMu.RUnlock()
			if !revocationSocketUsable(other) {
				t.Error("unrelated token socket closed")
			}
		})
	}
}
