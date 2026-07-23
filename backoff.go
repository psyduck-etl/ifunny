package main

import (
	"context"
	"io"
	"math/rand"
	"net/http"
	"time"
)

const (
	// backoffBase is the first backoff interval after a 429; each
	// subsequent consecutive 429 doubles it.
	backoffBase = 2 * time.Second

	// backoffMaxShift caps the doubling. The ceiling is
	// backoffBase << backoffMaxShift = 2s << 12 = 8192s (~2.3h); past
	// that the interval holds at the ceiling and we keep retrying there
	// forever. 12 is the shift that lifts a 2s base into the "few hours"
	// order of magnitude without overshooting into days.
	backoffMaxShift = 12

	// backoffJitter scales each sleep by a uniform random factor in
	// [1-backoffJitter, 1+backoffJitter] — i.e. ±10%. Independent jobs
	// that hit a 429 in lockstep drift apart instead of re-colliding on
	// every retry, so their load staggers rather than pulsing together.
	backoffJitter = 0.10
)

// retryTransport wraps an http.RoundTripper, retrying HTTP 429 (Too Many
// Requests) responses indefinitely with jittered exponential backoff. The
// interval doubles from backoffBase up to the backoffMaxShift ceiling, then
// holds there forever. Everything else — success, other status codes, and
// the transport's own errors — passes straight through untouched; this only
// addresses upstream rate limiting.
//
// giveUpAfter bounds the retry: after that many consecutive 429 tries the
// transport stops and returns the last 429 response so the caller sees the
// rate-limit error. 0 keeps the retry unbounded.
//
// RoundTrip holds no mutable state, so a single retryTransport is safe to
// share across the concurrent goroutines the iterator/fan-out transformers
// spin up.
type retryTransport struct {
	base        http.RoundTripper
	giveUpAfter uint
}

// backoffSleep is the sleep the retry loop uses between attempts. It is a
// package var (like clientFor) so tests can swap in a no-op that records the
// requested intervals instead of blocking on real, hours-long backoffs.
var backoffSleep = sleepCtx

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		// Rewind the body on every retry. http.NewRequestWithContext
		// populates GetBody for the in-memory bodies iFunny's login/prime
		// POSTs use, so a consumed body can be replayed; GET requests
		// carry no body and skip this.
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}

		resp, err := t.base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		// attempt+1 tries have now returned 429. If a give-up bound is set
		// and we've reached it, hand the 429 back to the caller (body
		// intact) instead of retrying further.
		if t.giveUpAfter != 0 && uint(attempt+1) >= t.giveUpAfter {
			return resp, nil
		}

		// Drain and close so the connection returns to the pool before we
		// sleep, then back off and retry the same request.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		if err := backoffSleep(req.Context(), backoffFor(attempt)); err != nil {
			return nil, err
		}
	}
}

// backoffFor returns the (jittered) sleep before the attempt-th retry.
// attempt 0 yields backoffBase, doubling each step until the shift saturates
// at backoffMaxShift, after which every attempt sits at the ceiling.
func backoffFor(attempt int) time.Duration {
	shift := min(attempt, backoffMaxShift)
	d := backoffBase << shift
	factor := 1 + (rand.Float64()*2-1)*backoffJitter
	return time.Duration(float64(d) * factor)
}

// sleepCtx sleeps for d, returning early with ctx.Err() if ctx is cancelled
// first so a cancelled stage aborts an in-flight backoff instead of waiting
// out the full (potentially hours-long) interval.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryingHTTPClient clones http.DefaultClient and wraps its transport in a
// retryTransport, preserving the standard connection pool and timeouts while
// adding transparent 429 backoff. giveUpAfter bounds the retry (0 =
// unbounded). Passed to ifunny client constructors via ifunny.WithHTTPClient.
func retryingHTTPClient(giveUpAfter uint) *http.Client {
	base := http.DefaultClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c := *http.DefaultClient
	c.Transport = &retryTransport{base: base, giveUpAfter: giveUpAfter}
	return &c
}
