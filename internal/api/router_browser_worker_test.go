package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// registerParkedWorker installs a connected worker whose WebSocket is real
// but never answers a job_request, mirroring a browser parked on a Cloudflare
// challenge nobody is solving.
func registerParkedWorker(t *testing.T, userID string) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			// Read and discard every message; never respond.
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial test worker: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	browserWorkersMu.Lock()
	browserWorkers["test-worker-parked"] = &BrowserWorker{ID: "test-worker-parked", Conn: client, State: "authenticated", UserID: userID}
	browserWorkersMu.Unlock()
	t.Cleanup(func() {
		browserWorkersMu.Lock()
		delete(browserWorkers, "test-worker-parked")
		browserWorkersMu.Unlock()
	})
}

// A worker parked without ever answering must not pin the caller past
// cancellation: EnqueueBrowserJob returns context.Canceled as soon as ctx
// fires instead of waiting out the 5-minute safety net. This is what stops an
// update whose client already received an error from queuing page after page
// into the user's browser.
func TestEnqueueBrowserJobReturnsWhenContextCanceled(t *testing.T) {
	env := newAPITestEnv(t)
	registerParkedWorker(t, "user-1")

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	start := time.Now()
	_, err := env.server.EnqueueBrowserJob(ctx, "fetch_page", "https://example.com/", nil, "user-1")
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("wait took %s; cancellation should return promptly", elapsed)
	}
}

// A ctx already done before the enqueue never reaches the queue.
func TestEnqueueBrowserJobFailsFastWhenContextDone(t *testing.T) {
	env := newAPITestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := env.server.EnqueueBrowserJob(ctx, "fetch_page", "https://example.com/", nil, "user-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled without dispatching, got %v", err)
	}
}
