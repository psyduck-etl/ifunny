package main

import (
	"context"

	"github.com/psyduck-etl/sdk/data"
)

// acceptConfig is a backward-compatibility alias for data.InputCodec.
// See data.InputCodec for the canonical documentation and usage examples.
type acceptConfig = data.InputCodec

// emitConfig is a backward-compatibility alias for data.OutputCodec.
// See data.OutputCodec for the canonical documentation and usage examples.
type emitConfig = data.OutputCodec

// sendErr forwards err onto errs, giving up if ctx is cancelled first.
// Callers use this instead of a bare `errs <- err` to avoid blocking
// indefinitely on an errs channel the host has stopped reading after
// cancellation. Reports whether the error was delivered — false means
// the caller should return without retrying.
//
// This replaces the check-then-send pattern `if ctx.Err() == nil { errs
// <- err }`, which races: ctx can be cancelled between the check and the
// send, blocking forever on the send.
func sendErr(ctx context.Context, errs chan<- error, err error) bool {
	select {
	case errs <- err:
		return true
	case <-ctx.Done():
		return false
	}
}
