package config

import "testing"

func TestDelayFromEnv(t *testing.T) {
	// Unset/empty/zero fall back to the old noveldownloader defaults.
	t.Setenv("DOWNLOAD_MIN_DELAY_MS", "")
	t.Setenv("DOWNLOAD_MAX_DELAY_MS", "")
	min, max := delayFromEnv()
	if min != 5000 || max != 10000 {
		t.Fatalf("unset env: got min=%d max=%d, want 5000/10000", min, max)
	}

	// A set value wins; a zero sibling keeps its default.
	t.Setenv("DOWNLOAD_MIN_DELAY_MS", "1200")
	min, max = delayFromEnv()
	if min != 1200 || max != 10000 {
		t.Fatalf("partial env: got min=%d max=%d, want 1200/10000", min, max)
	}

	t.Setenv("DOWNLOAD_MIN_DELAY_MS", "0")
	t.Setenv("DOWNLOAD_MAX_DELAY_MS", "9000")
	min, max = delayFromEnv()
	if min != 5000 || max != 9000 {
		t.Fatalf("zero min: got min=%d max=%d, want 5000/9000", min, max)
	}
}
