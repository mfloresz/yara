package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultDecisionEndpoint = "https://openrouter.ai/api/alpha/decisions"

// DecisionQuestion is one typed question sent to a decision model. Criteria
// keys are the option labels for a "choice" question and the literals "true"
// and "false" for a "noul" question.
type DecisionQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

type DecisionRequest struct {
	Model     string
	State     map[string]any
	Questions map[string]DecisionQuestion
}

type decisionRequest struct {
	Model     string                      `json:"model"`
	State     map[string]any              `json:"state"`
	Questions map[string]DecisionQuestion `json:"questions"`
}

type DecisionAnswer struct {
	Type          string             `json:"type"`
	Noul          float64            `json:"noul"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type DecisionUsage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

type DecisionResponse struct {
	Answers map[string]DecisionAnswer `json:"answers"`
	Usage   DecisionUsage             `json:"usage"`
}

type DecisionChecker interface {
	Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error)
}

// DecisionClient calls the OpenRouter decisions endpoint. It is deliberately
// separate from OpenAIProvider: decisions is not chat-completions compatible
// and produces probabilities instead of generated text.
type DecisionClient struct {
	APIKey  string
	BaseURL string
	Timeout time.Duration
	HTTP    *http.Client
}

func (c *DecisionClient) Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return DecisionResponse{}, fmt.Errorf("decision model is required")
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return DecisionResponse{}, fmt.Errorf("decision API key is not configured")
	}
	payload, err := json.Marshal(decisionRequest{
		Model:     model,
		State:     req.State,
		Questions: req.Questions,
	})
	if err != nil {
		return DecisionResponse{}, fmt.Errorf("encode decision request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return DecisionResponse{}, fmt.Errorf("build decision request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", yaraUserAgent)
	httpReq.Header.Set("HTTP-Referer", openRouterReferer)
	httpReq.Header.Set("X-Title", yaraUserAgent)

	client := c.HTTP
	if client == nil {
		timeout := c.Timeout
		if timeout <= 0 {
			timeout = 90 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return DecisionResponse{}, fmt.Errorf("decision request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return DecisionResponse{}, fmt.Errorf("read decision response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DecisionResponse{}, fmt.Errorf("decision request failed: HTTP %d: %s", resp.StatusCode, truncateString(string(body), 300))
	}
	var out DecisionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return DecisionResponse{}, fmt.Errorf("parse decision response: %w (raw: %s)", err, truncateString(string(body), 300))
	}
	if len(out.Answers) == 0 {
		return DecisionResponse{}, fmt.Errorf("decision response has no answers")
	}
	return out, nil
}

func (c *DecisionClient) endpoint() string {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return defaultDecisionEndpoint
	}
	if strings.HasSuffix(base, "/v1") {
		base = strings.TrimSuffix(base, "/v1")
	}
	if strings.HasSuffix(base, "/alpha/decisions") {
		return base
	}
	return base + "/alpha/decisions"
}
