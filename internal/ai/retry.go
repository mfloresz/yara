package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Transient-failure retries. goai wrapped every call in withRetry with
// MaxRetries=2 by default, honoring Retry-After; eino's openai component has
// no retry at all, so the migration silently dropped it. Translation, refine
// and glossary loops in internal/api have their own job-level retries, but the
// agent tool loop and GenerateGlossary do not, so a single 429 or a dropped
// connection fails the whole turn.
const (
	// defaultAgentRetries matches the goai default of 2 retries (3 attempts).
	defaultAgentRetries = 2
	// retryBaseBackoff is the first delay; subsequent delays grow
	// exponentially up to retryMaxBackoff.
	retryBaseBackoff = 500 * time.Millisecond
	retryMaxBackoff  = 8 * time.Second
)

// agentStreamIdleTimeout is how long the agent chat waits for the next stream
// chunk before giving up on the provider.
//
// A provider that accepts the request and then goes quiet — a wedged upstream,
// a proxy that stops relaying — produces no error and no output, so the chat
// would hang with nothing on screen until the whole-turn timeout expires. This
// bounds that specific case. It is a per-chunk gap, NOT a cap on the answer:
// a response that keeps arriving is never cut off, which is exactly what the
// previous fixed deadline (the provider's Timeout) got wrong.
//
// Translation and refine jobs are unaffected: they never go through
// streamWithRetry, and keep using their own Settings timeout, applied as a
// per-call context deadline in einoCallContext.
var agentStreamIdleTimeout = 60 * time.Second

// errNoRetry marks a failure that must not be retried, so the classifier below
// can distinguish "this will never succeed" from "try again".
var errNoRetry = errors.New("non-retryable")

// isRetryableModelError reports whether a provider failure is worth another
// attempt: rate limits, server-side errors, and transport hiccups. Everything
// else (auth, bad request, unsupported model) is deterministic — retrying it
// just burns the user's quota and delays the real error.
func isRetryableModelError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, errNoRetry) {
		return false
	}
	// eino's openai component surfaces HTTP failures as APIError.
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.HTTPStatusCode == 429:
			return true
		case apiErr.HTTPStatusCode >= 500:
			return true
		case apiErr.HTTPStatusCode == 408:
			return true
		default:
			return false
		}
	}
	// Transport-level failures (connection reset, DNS, EOF mid-response).
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, transient := range []string{
		"connection reset", "connection refused", "broken pipe",
		"no such host", "timeout", "temporarily unavailable", "eof",
		"server closed idle", "too many requests", "overloaded", "bad gateway",
		"service unavailable", "gateway timeout",
	} {
		if strings.Contains(msg, transient) {
			return true
		}
	}
	return false
}

// retryDelay returns the backoff for attempt n (0-based): exponential with a
// cap, so a long outage does not turn into minutes of sleeping.
func retryDelay(attempt int) time.Duration {
	delay := time.Duration(float64(retryBaseBackoff) * math.Pow(2, float64(attempt)))
	if delay > retryMaxBackoff || delay <= 0 {
		return retryMaxBackoff
	}
	return delay
}

// withRetry runs fn until it succeeds, the error is non-retryable, or the
// attempts are exhausted. Each attempt runs under the caller's context, so an
// aborted turn stops retrying immediately instead of sleeping through it.
func withRetry(ctx context.Context, attempts int, fn func(ctx context.Context) error) error {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		// Do not sleep after the final attempt.
		if attempt > 0 {
			timer := time.NewTimer(retryDelay(attempt - 1))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryableModelError(err) {
			return err
		}
	}
	return lastErr
}

// generateWithRetry runs a non-streaming Generate with transient retries.
func generateWithRetry(ctx context.Context, m model.ToolCallingChatModel, msgs []*schema.Message, opts []model.Option) (*schema.Message, error) {
	var out *schema.Message
	err := withRetry(ctx, defaultAgentRetries+1, func(ctx context.Context) error {
		var err error
		out, err = m.Generate(ctx, msgs, opts...)
		return err
	})
	return out, err
}

// streamWithRetry runs one streaming step with retries, but only while nothing
// has been delivered to the caller yet.
//
// Retrying a stream that already emitted deltas would duplicate text in the UI
// and duplicate content in the trail, so once a chunk has been forwarded the
// failure is returned as-is (marked non-retryable). Before that point nothing
// has been observed, so a silent retry is invisible and correct.
//
// idleTimeout bounds the wait for the NEXT chunk, not the whole step. A
// provider that accepts the request and then goes silent — the failure mode
// that leaves a chat hanging with no error and no output — trips it. A step
// that keeps delivering is never cut off, however long it takes, which is what
// a fixed per-call deadline would wrongly do to a long answer.
func streamWithRetry(
	ctx context.Context,
	m model.ToolCallingChatModel,
	msgs []*schema.Message,
	opts []model.Option,
	deliver func(chunk *schema.Message),
) ([]*schema.Message, error) {
	var chunks []*schema.Message
	err := withRetry(ctx, defaultAgentRetries+1, func(ctx context.Context) error {
		chunks = chunks[:0]
		delivered := 0
		stream, err := m.Stream(ctx, msgs, opts...)
		if err != nil {
			return err
		}
		defer stream.Close()
		for {
			var chunk *schema.Message
			var recvErr error
			if agentStreamIdleTimeout > 0 {
				chunk, recvErr = recvWithIdleTimeout(ctx, stream, agentStreamIdleTimeout)
			} else {
				chunk, recvErr = stream.Recv()
			}
			if recvErr == io.EOF {
				return nil
			}
			if recvErr != nil {
				if delivered > 0 {
					// The caller already saw this step's text; replaying it
					// would duplicate content. Surface the error instead.
					// Both sentinels are wrapped so errors.Is still matches
					// errNoRetry and any specific cause underneath.
					return fmt.Errorf("%w: %w", errNoRetry, recvErr)
				}
				return recvErr
			}
			chunks = append(chunks, chunk)
			delivered++
			deliver(chunk)
		}
	})
	if err != nil {
		return chunks, err
	}
	return chunks, nil
}

// recvWithIdleTimeout wraps stream.Recv so a provider that stops sending
// without erroring is detected. It cancels only the in-flight read: the
// stream is left for the caller to Close, so no goroutine is stranded.
//
// A cancellation caused by the caller's own context is passed through
// unchanged so it is not misreported as a provider stall.
func recvWithIdleTimeout(
	ctx context.Context,
	stream *schema.StreamReader[*schema.Message],
	timeout time.Duration,
) (*schema.Message, error) {
	// The cancel is not passed to Recv (eino's StreamReader takes no context);
	// it exists so a read still in flight when the timer fires is released when
	// the caller Closes the stream, instead of pinning the goroutine.
	_, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		chunk *schema.Message
		err   error
	}
	// Buffered: the goroutine must never block writing after the timeout
	// fired, or it would leak.
	ch := make(chan result, 1)
	go func() {
		chunk, err := stream.Recv()
		ch <- result{chunk, err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		// Caller cancelled: the read is already unwinding.
		return nil, ctx.Err()
	case <-timer.C:
		cancel()
		return nil, fmt.Errorf("%w: no stream chunk received in %s", errStreamStalled, timeout)
	case r := <-ch:
		return r.chunk, r.err
	}
}

// errStreamStalled marks a provider that stopped streaming without closing
// the response. Not retryable: the turn is already partially delivered to the
// user, so failing fast with a clear message beats silently restarting.
var errStreamStalled = errors.New("stream stalled")

// IsStreamStalled reports whether err was caused by the idle guard, including
// after it was wrapped as non-retryable on the way out of the step loop. The
// API layer uses this to tell the user "the provider stopped responding"
// instead of a generic failure.
func IsStreamStalled(err error) bool {
	return errors.Is(err, errStreamStalled)
}
