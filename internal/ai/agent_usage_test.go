package ai

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func usageMessage(prompt, completion, total, cached, reasoning int) *schema.Message {
	return &schema.Message{
		Role: schema.Assistant,
		ResponseMeta: &schema.ResponseMeta{
			Usage: &schema.TokenUsage{
				PromptTokens:            prompt,
				CompletionTokens:        completion,
				TotalTokens:             total,
				PromptTokenDetails:      schema.PromptTokenDetails{CachedTokens: cached},
				CompletionTokensDetails: schema.CompletionTokensDetails{ReasoningTokens: reasoning},
			},
		},
	}
}

func TestAccumulateAgentUsage(t *testing.T) {
	var total AgentUsage
	accumulateAgentUsage(&total, usageMessage(100, 10, 110, 20, 3))
	accumulateAgentUsage(&total, usageMessage(150, 30, 180, 40, 7))
	// Prompt keeps the latest step's context size; the rest sums.
	if total.PromptTokens != 150 {
		t.Fatalf("PromptTokens = %d, want 150", total.PromptTokens)
	}
	if total.CompletionTokens != 40 {
		t.Fatalf("CompletionTokens = %d, want 40", total.CompletionTokens)
	}
	if total.TotalTokens != 290 {
		t.Fatalf("TotalTokens = %d, want 290", total.TotalTokens)
	}
	if total.CachedTokens != 60 {
		t.Fatalf("CachedTokens = %d, want 60", total.CachedTokens)
	}
	if total.ReasoningTokens != 10 {
		t.Fatalf("ReasoningTokens = %d, want 10", total.ReasoningTokens)
	}
	if agentUsageOrNil(AgentUsage{}) != nil {
		t.Fatal("empty usage must map to nil")
	}
	// Nil-safety: providers without usage must not panic.
	accumulateAgentUsage(&total, nil)
	accumulateAgentUsage(&total, &schema.Message{Role: schema.Assistant})
}
