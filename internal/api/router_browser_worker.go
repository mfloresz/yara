package api

import (
	"crypto/rand"
	"encoding/json"
	"encoding/hex"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type BrowserWorker struct {
	ID           string
	Conn         *websocket.Conn
	Browser      string
	Capabilities []string
	Version      string
	State        string
	UserID       string
	TokenID      string
	ConnectedAt  time.Time
	LastHeartbeat time.Time
	mu           sync.Mutex
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
	Params    map[string]interface{} `json:"params"`
}

type BrowserWorkerJobResult struct {
	JobID  string                 `json:"jobId"`
	Status string                 `json:"status"`
	Data   map[string]interface{} `json:"data"`
}

var (
	browserWorkers = make(map[string]*BrowserWorker)
	browserWorkersMu sync.RWMutex
)

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

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Error("browser worker read error", "workerId", worker.ID, "error", err)
			}
			break
		}

		var msg BrowserWorkerMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			slog.Error("browser worker invalid message", "workerId", worker.ID, "error", err)
			continue
		}

		s.handleWorkerMessage(worker, &msg)
	}
}

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
			return
		}

		if payload.Token == "" {
			slog.Warn("browser worker registered without token", "workerId", worker.ID)
			worker.mu.Lock()
			worker.State = "unauthenticated"
			worker.mu.Unlock()
			return
		}

		validated, err := s.Store.ValidateWorkerToken(payload.Token)
		if err != nil {
			slog.Warn("browser worker invalid token", "workerId", worker.ID, "error", err)
			worker.mu.Lock()
			worker.State = "unauthenticated"
			worker.mu.Unlock()
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

	case "heartbeat":
		worker.mu.Lock()
		worker.LastHeartbeat = time.Now()
		worker.mu.Unlock()

	case "job_result":
		var result BrowserWorkerJobResult
		if err := json.Unmarshal(msg.Payload, &result); err != nil {
			slog.Error("browser worker invalid job result", "workerId", worker.ID, "error", err)
			return
		}
		s.handleBrowserWorkerJobResult(worker, &result)

	case "pong":
		worker.mu.Lock()
		worker.LastHeartbeat = time.Now()
		worker.mu.Unlock()
	}
}

func (s *Server) handleBrowserWorkerJobResult(worker *BrowserWorker, result *BrowserWorkerJobResult) {
	slog.Info("browser worker job result",
		"workerId", worker.ID,
		"jobId", result.JobID,
		"status", result.Status)

	if s.browserWorkerResultCh != nil {
		select {
		case s.browserWorkerResultCh <- result:
		default:
			slog.Warn("browser worker result channel full, dropping result", "jobId", result.JobID)
		}
	}
}

func (s *Server) SendJobToBrowserWorker(operation, url string, params map[string]interface{}) (*BrowserWorkerJobResult, error) {
	browserWorkersMu.RLock()
	var worker *BrowserWorker
	for _, w := range browserWorkers {
		if w.Conn != nil {
			worker = w
			break
		}
	}
	browserWorkersMu.RUnlock()

	if worker == nil {
		return nil, ErrNoBrowserWorker
	}

	jobID := generateJobID()
	req := BrowserWorkerJobRequest{
		JobID:     jobID,
		Operation: operation,
		URL:       url,
		Params:    params,
	}

	payload, _ := json.Marshal(req)
	msg := BrowserWorkerMessage{
		Type:      "job_request",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}

	worker.mu.Lock()
	err := worker.Conn.WriteJSON(msg)
	worker.mu.Unlock()

	if err != nil {
		return nil, err
	}

	slog.Info("sent job to browser worker",
		"workerId", worker.ID,
		"jobId", jobID,
		"operation", operation,
		"url", url)

	return s.waitForBrowserWorkerResult(jobID)
}

func (s *Server) waitForBrowserWorkerResult(jobID string) (*BrowserWorkerJobResult, error) {
	timeout := time.After(120 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return nil, ErrBrowserWorkerTimeout
		case result := <-s.browserWorkerResultCh:
			if result.JobID == jobID {
				return result, nil
			}
			s.browserWorkerResultCh <- result
		case <-ticker.C:
			continue
		}
	}
}

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
