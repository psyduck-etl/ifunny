package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/psyduck-etl/ifunny/internal/ifunnymock"
)

// runInteractionsAsync drives interactionsTransformer in a background goroutine
// and returns channels the caller can drain plus a done signal. Caller owns
// closing in.
func runInteractionsAsync(
	t testing.TB,
	tr func(context.Context, <-chan []byte, chan<- []byte, chan<- error),
	ctx context.Context,
) (chan<- []byte, <-chan []byte, <-chan error, <-chan struct{}) {
	t.Helper()
	in := make(chan []byte)
	out := make(chan []byte, 64)
	errs := make(chan error, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tr(ctx, in, out, errs)
	}()
	return in, out, errs, done
}

// drainConcurrently reads out and errs until out is closed, returning
// collected outputs and errors. Note: transformer does NOT close errs,
// so we track it via a separate stop signal driven by out's close.
func drainConcurrently(out <-chan []byte, errs <-chan error) ([][]byte, []error) {
	var (
		wg      sync.WaitGroup
		outs    [][]byte
		gotErrs []error
		mu      sync.Mutex
	)

	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for b := range out {
			mu.Lock()
			outs = append(outs, b)
			mu.Unlock()
		}
		close(stop)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case e, ok := <-errs:
				if !ok {
					return
				}
				mu.Lock()
				gotErrs = append(gotErrs, e)
				mu.Unlock()
			case <-stop:
				// Drain any errors already sitting on the buffer.
				for {
					select {
					case e := <-errs:
						mu.Lock()
						gotErrs = append(gotErrs, e)
						mu.Unlock()
					default:
						return
					}
				}
			}
		}
	}()

	wg.Wait()
	return outs, gotErrs
}

// TestInteractionsCtxCancelPropagates verifies that cancelling the transformer's
// context causes it to return promptly even when sub-iterators are mid-flight,
// and that out is closed so downstream readers unblock. ctx now threads all the
// way through ifunny-go's HTTP calls, so cancellation aborts the in-flight
// request itself rather than only firing the emit/sendErr guards afterward.
func TestInteractionsCtxCancelPropagates(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	content := srv.AddContent(alice)
	srv.AddSmiler(content, bob)
	srv.AddRepublisher(content, bob)
	srv.AddComment(content, bob, "hi")

	// 50ms latency per HTTP call — enough to guarantee the cancel lands
	// before workers finish, small enough not to make the test slow.
	srv.SetLatency(50 * time.Millisecond)

	tr, err := interactionsTransformer(testParser(withAuth(map[string]any{
		"interactions": []string{"author", "smiles", "republishes", "comments"},
		"accept":       "string",
		"emit":         "string",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	in, out, errs, done := runInteractionsAsync(t, tr, ctx)

	in <- []byte(content.ID)
	close(in) // let outer for-loop drain and reach final wg.Wait

	// Cancel before the workers can finish their HTTP calls.
	time.AfterFunc(10*time.Millisecond, cancel)

	// Drain concurrently so out never blocks the transformer's producers.
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		drainConcurrently(out, errs)
	}()

	// Bounded wait: latency + generous slack. If we exceed this, ctx.Done
	// guards in emitUser/sendErr are not working.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("transformer did not return within 2s of ctx cancel")
	}
	<-drainDone
}

// TestInteractionsUpstreamCloseMidflight verifies the final wg.Wait at the
// bottom of interactionsTransformer: closing `in` while sub-iterators are
// still running must not truncate their output, and out must close only
// after all workers finish.
func TestInteractionsUpstreamCloseMidflight(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	carol := srv.AddUser("carol")
	dave := srv.AddUser("dave")
	content := srv.AddContent(alice)
	srv.AddSmiler(content, bob)
	srv.AddSmiler(content, carol)
	srv.AddRepublisher(content, dave)

	// Small latency so the "in closed" happens while workers are running.
	srv.SetLatency(20 * time.Millisecond)

	tr, err := interactionsTransformer(testParser(withAuth(map[string]any{
		"interactions": []string{"smiles", "republishes"},
		"accept":       "string",
		"emit":         "string",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	ctx := context.Background()
	in, out, errs, done := runInteractionsAsync(t, tr, ctx)

	in <- []byte(content.ID)
	close(in) // immediately close; workers are still running

	outs, gotErrs := drainConcurrently(out, errs)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("transformer did not return within 3s")
	}

	if len(gotErrs) != 0 {
		t.Errorf("unexpected errors: %v", gotErrs)
	}
	// 2 smiles + 1 republisher = 3 outputs
	if len(outs) != 3 {
		t.Errorf("got %d outputs, want 3 (2 smiles + 1 republisher); last-input workers may have been truncated", len(outs))
	}
}

// TestInteractionsSubIteratorErrorIsolation verifies that an error in one
// sub-iterator (e.g. /smiles returning 500) does not cancel the sibling
// iterators (republishes, comments): the error surfaces on errs, but
// unaffected outputs still arrive.
func TestInteractionsSubIteratorErrorIsolation(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	carol := srv.AddUser("carol")
	dave := srv.AddUser("dave")
	content := srv.AddContent(alice)
	srv.AddSmiler(content, bob)          // will fail
	srv.AddRepublisher(content, carol)   // should succeed
	srv.AddComment(content, dave, "hi")  // should succeed

	// Permanent 500 on any /smiles request.
	srv.SetError("/smiles", 500, -1)

	tr, err := interactionsTransformer(testParser(withAuth(map[string]any{
		"interactions": []string{"smiles", "republishes", "comments"},
		"accept":       "string",
		"emit":         "string",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	ctx := context.Background()
	in, out, errs, done := runInteractionsAsync(t, tr, ctx)

	in <- []byte(content.ID)
	close(in)

	outs, gotErrs := drainConcurrently(out, errs)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("transformer did not return within 3s")
	}

	if len(gotErrs) == 0 {
		t.Fatal("expected an error from the smiles iterator, got none")
	}
	// Check at least one error mentions HTTP 500 or a related signal.
	sawSmilesErr := false
	for _, e := range gotErrs {
		if strings.Contains(e.Error(), "500") || strings.Contains(e.Error(), "injected") {
			sawSmilesErr = true
			break
		}
	}
	if !sawSmilesErr {
		t.Errorf("no error mentions 500/injected: %v", gotErrs)
	}

	// Republisher + comment author = 2 outputs from sibling iterators.
	gotIDs := make(map[string]bool)
	for _, b := range outs {
		gotIDs[string(b)] = true
	}
	if !gotIDs[carol.ID] {
		t.Errorf("missing republisher %q; sibling iterator canceled? outs=%v", carol.ID, gotIDs)
	}
	if !gotIDs[dave.ID] {
		t.Errorf("missing comment author %q; sibling iterator canceled? outs=%v", dave.ID, gotIDs)
	}
}
