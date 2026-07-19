package main

import (
	"context"

	"github.com/psyduck-etl/sdk/data"
)

// stringy reports whether spec names the "string" codec — the one that
// carries a bare terminal reference per record (a content id, user id,
// channel name) rather than a discrete structured object. Only the literal
// "string" spec qualifies; discrete codecs (json, yaml, ...) do not.
//
// The enrich transformers branch on stringy to decide whether to emit the
// full object or just its terminal reference, and whether to interpret
// input as a bare id or as a decoded map from which rich fields can
// short-circuit the API.
func stringy(spec string) bool {
	return spec == "string"
}

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
