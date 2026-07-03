package ai

// Shared test constants for real API integration tests.
// Tests that hit a live provider should use these to ensure consistency.
const (
	testBaseURL = "https://opencode.ai/zen/go/v1"
	testModel   = "deepseek-v4-flash"
	testTimeout = 300
)
