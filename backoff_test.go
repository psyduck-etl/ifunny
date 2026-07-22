package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// roundTripFunc adapts a function to http.RoundTripper so tests can script
// per-attempt responses without a live server.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func resp(status int) *http.Response {
	return &http.Response{StatusCode: status, Body: http.NoBody}
}

// TestBackoffFor pins the doubling schedule, the ceiling, and the jitter
// envelope. Each interval must land within ±backoffJitter of 2s<<min(n,12).
func TestBackoffFor(t *testing.T) {
	for attempt := 0; attempt <= backoffMaxShift+3; attempt++ {
		shift := min(attempt, backoffMaxShift)
		want := backoffBase << shift
		lo := time.Duration(float64(want) * (1 - backoffJitter))
		hi := time.Duration(float64(want) * (1 + backoffJitter))
		// Sample repeatedly since the jitter is random.
		for range 100 {
			got := backoffFor(attempt)
			if got < lo || got > hi {
				t.Fatalf("backoffFor(%d) = %s, want within [%s,%s] of %s", attempt, got, lo, hi, want)
			}
		}
	}

	// The ceiling: 2s << 12 = 8192s, in the "few hours" range as designed.
	if ceiling := backoffBase << backoffMaxShift; ceiling != 8192*time.Second {
		t.Fatalf("ceiling = %s, want 8192s", ceiling)
	}
}

// TestRetryTransportPassthrough: a non-429 response returns immediately with
// no backoff.
func TestRetryTransportPassthrough(t *testing.T) {
	defer restoreBackoffSleep()
	slept := 0
	backoffSleep = func(context.Context, time.Duration) error { slept++; return nil }

	calls := 0
	tr := &retryTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return resp(http.StatusOK), nil
	})}

	r, err := tr.RoundTrip(newReq(t))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if r.StatusCode != http.StatusOK || calls != 1 || slept != 0 {
		t.Fatalf("status=%d calls=%d slept=%d, want 200/1/0", r.StatusCode, calls, slept)
	}
}

// TestRetryTransportRetriesUntilSuccess: consecutive 429s are retried with a
// growing backoff until a non-429 arrives.
func TestRetryTransportRetriesUntilSuccess(t *testing.T) {
	defer restoreBackoffSleep()
	var intervals []time.Duration
	backoffSleep = func(_ context.Context, d time.Duration) error {
		intervals = append(intervals, d)
		return nil
	}

	calls := 0
	tr := &retryTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls <= 3 {
			return resp(http.StatusTooManyRequests), nil
		}
		return resp(http.StatusOK), nil
	})}

	r, err := tr.RoundTrip(newReq(t))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", r.StatusCode)
	}
	if calls != 4 {
		t.Fatalf("calls = %d, want 4 (3 x 429 + 1 x 200)", calls)
	}
	if len(intervals) != 3 {
		t.Fatalf("slept %d times, want 3", len(intervals))
	}
	// The intervals grow: attempt 0,1,2 map to 2s,4s,8s before jitter, so
	// each must strictly exceed the last even accounting for ±10%.
	if !(intervals[0] < intervals[1] && intervals[1] < intervals[2]) {
		t.Errorf("intervals not increasing: %v", intervals)
	}
}

// TestRetryTransportContextCancel: a cancelled context during backoff aborts
// the retry loop and surfaces the ctx error rather than looping forever.
func TestRetryTransportContextCancel(t *testing.T) {
	defer restoreBackoffSleep()
	backoffSleep = func(ctx context.Context, _ time.Duration) error {
		return ctx.Err() // simulate ctx already done at sleep time
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tr := &retryTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return resp(http.StatusTooManyRequests), nil
	})}

	_, err := tr.RoundTrip(newReq(t).WithContext(ctx))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestSleepCtxCancels: sleepCtx returns promptly with the ctx error when the
// context is cancelled before the (long) duration elapses.
func TestSleepCtxCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func newReq(t *testing.T) *http.Request {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, "https://api.ifunny.test/v4/x", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return r
}

func restoreBackoffSleep() { backoffSleep = sleepCtx }
