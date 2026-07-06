package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:    func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type BrowserWorker struct {
	ID            string
	Conn          *websocket.Conn
	Browser       string
	Capabilities  []string
	Version       string
	State         string
	UserID        string
	TokenID       string
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	mu            sync.Mutex
}

type BrowserWorkerMessage struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"timestamp"`
}

type BrowserWorkerJobRequest struct {
	JobID     string                 `json:"jobId"`
	Operation string                 `json:"operation"`
	URL       string                 `json:"url"`
	Timeout   int                    `json:"timeout,omitempty"`
	Params    map[string]interface{} `json:"params"`
}

type BrowserWorkerJobResult struct {
	JobID  string                 `json:"jobId"`
	Status string                 `json:"status"`
	Data   map[string]interface{} `json:"data"`
}

var (
	browserWorkers   = make(map[string]*BrowserWorker)
	browserWorkersMu sync.RWMutex
)

// ── WebSocket handler ──────────────────────────────────────────────────────

func (s *Server) handleBrowserWorkerWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("browser worker websocket upgrade", "error", err)
		return
	}

	worker := &BrowserWorker{
		ID:          generateWorkerID(),
		Conn:        conn,
		State:       "connected",
		ConnectedAt: time.Now(),
	}

	browserWorkersMu.Lock()
	browserWorkers[worker.ID] = worker
	browserWorkersMu.Unlock()

	slog.Info("browser worker connected", "workerId", worker.ID, "remote", r.RemoteAddr)

	defer func() {
		browserWorkersMu.Lock()
		delete(browserWorkers, worker.ID)
		browserWorkersMu.Unlock()
		conn.Close()
		slog.Info("browser worker disconnected", "workerId", worker.ID)
	}()

	conn.SetReadLimit(10 * 1024 * 1024)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		worker.mu.Lock()
		worker.LastHeartbeat = time.Now()
		worker.mu.Unlock()
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				worker.mu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				worker.mu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Error("browser worker read error", "workerId", worker.ID, "error", err)
			}
			break
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var msg BrowserWorkerMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			slog.Error("browser worker invalid message", "workerId", worker.ID, "error", err)
			continue
		}

		s.handleWorkerMessage(worker, &msg)
	}
}

// ── Message dispatch ───────────────────────────────────────────────────────

func (s *Server) handleWorkerMessage(worker *BrowserWorker, msg *BrowserWorkerMessage) {
	switch msg.Type {
	case "register":
		var payload struct {
			Browser      map[string]string `json:"browser"`
			Capabilities []string          `json:"capabilities"`
			Version      string            `json:"version"`
			Token        string            `json:"token"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			slog.Error("browser worker invalid register payload", "workerId", worker.ID, "error", err)
			s.sendWorkerMessage(worker, "register_response", map[string]interface{}{
				"ok":    false,
				"error": "invalid payload",
			})
			return
		}

		if payload.Token == "" {
			slog.Warn("browser worker registered without token", "workerId", worker.ID)
			worker.mu.Lock()
			worker.State = "unauthenticated"
			worker.mu.Unlock()
			s.sendWorkerMessage(worker, "register_response", map[string]interface{}{
				"ok":    false,
				"error": "no token provided",
			})
			return
		}

		validated, err := s.Store.ValidateWorkerToken(payload.Token)
		if err != nil {
			slog.Warn("browser worker invalid token", "workerId", worker.ID, "error", err)
			worker.mu.Lock()
			worker.State = "unauthenticated"
			worker.mu.Unlock()
			s.sendWorkerMessage(worker, "register_response", map[string]interface{}{
				"ok":    false,
				"error": "invalid token",
			})
			return
		}

		worker.mu.Lock()
		if payload.Browser != nil {
			worker.Browser = payload.Browser["name"]
		}
		worker.Capabilities = payload.Capabilities
		worker.Version = payload.Version
		worker.UserID = validated.UserID
		worker.TokenID = validated.ID
		worker.State = "authenticated"
		worker.mu.Unlock()

		slog.Info("browser worker registered and authenticated",
			"workerId", worker.ID,
			"browser", worker.Browser,
			"version", worker.Version,
			"userId", validated.UserID)

		s.sendWorkerMessage(worker, "register_response", map[string]interface{}{
			"ok":     true,
			"userId": validated.UserID,
		})

	case "job_result":
		var result BrowserWorkerJobResult
		if err := json.Unmarshal(msg.Payload, &result); err != nil {
			slog.Error("browser worker invalid job result", "workerId", worker.ID, "error", err)
			return
		}
		s.deliverBrowserJobResult(worker, &result)
	}
}

// ── Per-job result delivery ────────────────────────────────────────────────

// deliverBrowserWorkerResult writes a job result to the per-job channel
// registered in pendingBrowserJobs. If the caller has already timed out
// (channel closed), the result is silently dropped.
func (s *Server) deliverBrowserJobResult(worker *BrowserWorker, result *BrowserWorkerJobResult) {
	attrs := []any{
		"workerId", worker.ID,
		"jobId", result.JobID,
		"status", result.Status,
	}

	// Log diagnostic info about result.Data — keys present and html length
	keys := make([]string, 0, len(result.Data))
	for k := range result.Data {
		keys = append(keys, k)
	}
	attrs = append(attrs, "dataKeys", len(keys))

	if result.Status != "ok" {
		if e, ok := result.Data["error"].(string); ok {
			attrs = append(attrs, "error", e)
		}
	}

	if html, ok := result.Data["html"].(string); ok {
		attrs = append(attrs, "htmlLen", len(html))
	} else if result.Data["html"] != nil {
		attrs = append(attrs, "htmlType", fmt.Sprintf("%T", result.Data["html"]))
	} else {
		attrs = append(attrs, "htmlPresent", false)
	}
	if title, ok := result.Data["title"].(string); ok {
		attrs = append(attrs, "title", title)
	}
	if u, ok := result.Data["url"].(string); ok {
		attrs = append(attrs, "resultUrl", u)
	}

	slog.Info("browser worker job result", attrs...)

	s.pendingBrowserJobsMu.Lock()
	ch, ok := s.pendingBrowserJobs[result.JobID]
	s.pendingBrowserJobsMu.Unlock()

	if !ok {
		slog.Warn("browser worker result for unknown job, dropping", "jobId", result.JobID)
		return
	}

	// Non-blocking send: if the caller timed out and closed the channel,
	// this drops the result silently instead of blocking.
	select {
	case ch <- result:
	default:
		slog.Warn("browser worker result channel closed (caller timed out), dropping", "jobId", result.JobID)
	}
}

// ── FIFO consumer goroutine ────────────────────────────────────────────────

// processBrowserJobs runs in its own goroutine and is the sole writer to
// browser WebSocket connections. This guarantees strict sequentiality:
// all requests go through one goroutine, one at a time.
//
// Per-job result channels are stored in pendingBrowserJobs so that
// callers receive only their own result. Each job is given a 5-minute
// timeout (covers Cloudflare challenge resolution).
func (s *Server) processBrowserJobs() {
	slog.Info("browser job worker started")
	for job := range s.browserQueue {
		s.dispatchBrowserJob(job)
	}
	slog.Info("browser job worker stopped")
}

func (s *Server) dispatchBrowserJob(job BrowserJob) {
	browserWorkersMu.RLock()
	var worker *BrowserWorker
	for _, w := range browserWorkers {
		if w.Conn == nil || w.State != "authenticated" {
			continue
		}
		if job.UserID != "" && w.UserID != job.UserID {
			continue
		}
		worker = w
		break
	}
	browserWorkersMu.RUnlock()

	if worker == nil {
		slog.Warn("no browser worker available for job", "jobId", job.Request.JobID)
		job.Result <- &BrowserWorkerJobResult{
			JobID:  job.Request.JobID,
			Status: "error",
			Data:   map[string]interface{}{"error": "no browser worker connected"},
		}
		close(job.Result)
		return
	}

	payload, _ := json.Marshal(job.Request)
	msg := BrowserWorkerMessage{
		Type:      "job_request",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}

	worker.mu.Lock()
	err := worker.Conn.WriteJSON(msg)
	worker.mu.Unlock()

	if err != nil {
		slog.Error("failed to send job to browser worker",
			"workerId", worker.ID, "jobId", job.Request.JobID, "error", err)
		job.Result <- &BrowserWorkerJobResult{
			JobID:  job.Request.JobID,
			Status: "error",
			Data:   map[string]interface{}{"error": err.Error()},
		}
		close(job.Result)
		return
	}

	slog.Info("dispatched job to browser worker",
		"workerId", worker.ID,
		"jobId", job.Request.JobID,
		"operation", job.Request.Operation,
		"url", job.Request.URL)

	// Timeout handler runs in background — processBrowserJobs must not block
	// waiting for the timeout so it can process the next job in the queue.
	//
	// EnqueueBrowserJob reads the result from job.Result and returns once
	// deliverBrowserJobResult sends it. The timeout handler closes the channel
	// after 5 minutes as a safety net if the worker never responds.
	// deliverBrowserJobResult uses non-blocking send so closed-channel panics
	// cannot happen.
	go func() {
		select {
		case <-time.After(5 * time.Minute):
			slog.Warn("browser worker job timed out", "jobId", job.Request.JobID)
			select {
			case job.Result <- &BrowserWorkerJobResult{
				JobID:  job.Request.JobID,
				Status: "error",
				Data:   map[string]interface{}{"error": "browser worker job timed out (5m)"},
			}:
			default:
			}
		}
		close(job.Result)
	}()
}

// ── Public enqueue API ─────────────────────────────────────────────────────

// EnqueueBrowserJob sends a job request to the browser worker queue and
// waits for the result. All callers are serialized through a single
// goroutine, which guarantees sequential execution — critical for
// Cloudflare challenge handling where only one page can be loaded at a time.
//
// The caller blocks until a result arrives or the consumer goroutine
// times out (5 minutes). The channel is always closed after dispatch.
func (s *Server) EnqueueBrowserJob(operation, url string, params map[string]interface{}, userID string) (*BrowserWorkerJobResult, error) {
	s.pendingBrowserJobsMu.Lock()
	if len(s.pendingBrowserJobs) == 0 && len(s.browserQueue) == 0 {
		s.pendingBrowserJobsMu.Unlock()
		if !s.HasBrowserWorker() {
			return nil, ErrNoBrowserWorker
		}
	} else {
		s.pendingBrowserJobsMu.Unlock()
	}

	jobID := generateJobID()
	resultCh := make(chan *BrowserWorkerJobResult, 1)

	if params == nil {
		params = map[string]interface{}{}
	}
	req := BrowserWorkerJobRequest{
		JobID:     jobID,
		Operation: operation,
		URL:       url,
		Params:    params,
	}

	s.pendingBrowserJobsMu.Lock()
	s.pendingBrowserJobs[jobID] = resultCh
	s.pendingBrowserJobsMu.Unlock()

	defer func() {
		s.pendingBrowserJobsMu.Lock()
		delete(s.pendingBrowserJobs, jobID)
		s.pendingBrowserJobsMu.Unlock()
	}()

	s.browserQueue <- BrowserJob{Request: req, Result: resultCh, UserID: userID}

	result, ok := <-resultCh
	if !ok {
		return nil, ErrBrowserWorkerTimeout
	}

	if result.Status != "ok" {
		errMsg := "browser worker error: " + result.Status
		if e, ok := result.Data["error"].(string); ok {
			errMsg = e
		}
		return nil, &BrowserWorkerError{msg: errMsg}
	}

	return result, nil
}

// ── Helpers ────────────────────────────────────────────────────────────────

func (s *Server) HasBrowserWorker() bool {
	browserWorkersMu.RLock()
	defer browserWorkersMu.RUnlock()
	for _, w := range browserWorkers {
		if w.State == "authenticated" {
			return true
		}
	}
	return false
}

func GetBrowserWorkerCount() int {
	browserWorkersMu.RLock()
	defer browserWorkersMu.RUnlock()
	return len(browserWorkers)
}

func CloseWorkerByTokenID(tokenID string) {
	browserWorkersMu.Lock()
	defer browserWorkersMu.Unlock()
	for _, w := range browserWorkers {
		if w.TokenID == tokenID {
			w.Conn.Close()
			delete(browserWorkers, w.ID)
			slog.Info("closed worker after token revocation", "workerId", w.ID, "tokenID", tokenID)
			return
		}
	}
}

func (s *Server) sendWorkerMessage(worker *BrowserWorker, msgType string, payload interface{}) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.Conn == nil {
		return
	}
	msg := BrowserWorkerMessage{
		Type:      msgType,
		Timestamp: time.Now().UnixMilli(),
	}
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			slog.Error("failed to marshal worker message payload", "error", err)
			return
		}
		msg.Payload = data
	}
	if err := worker.Conn.WriteJSON(msg); err != nil {
		slog.Error("failed to send message to worker", "workerId", worker.ID, "type", msgType, "error", err)
	}
}

func generateWorkerID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "bw-" + hex.EncodeToString(b)
}

func generateJobID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "bj-" + hex.EncodeToString(b)
}

var (
	ErrNoBrowserWorker    = &BrowserWorkerError{"no browser worker connected"}
	ErrBrowserWorkerTimeout = &BrowserWorkerError{"browser worker response timeout"}
)

type BrowserWorkerError struct {
	msg string
}

func (e *BrowserWorkerError) Error() string {
	return e.msg
}
