package main

import (
	"context"
	"strings"

	"github.com/psyduck-etl/sdk"
)

// codecFor resolves an encoding spec via the sdk-registered codec factory.
// The host binary (psyduck) installs a factory at startup; standalone
// tests register a stub in TestMain. Spec strings are normalized to
// lowercase so config values like "JSON" keep working against the stdlib's
// lowercase codec names.
func codecFor(spec string) (sdk.Codec, error) {
	return sdk.GetCodec(strings.ToLower(spec))
}

// stringy reports whether spec names the "string" codec — the one that
// carries a bare terminal reference per record (a content id, user id,
// channel name) rather than a discrete structured object. Only the literal
// "string" spec qualifies; discrete codecs (json, yaml, ...) do not.
//
// The lookup transformers and producers branch on stringy to decide
// whether to emit the full object or just its terminal reference, and
// (for the lookup transformers) whether to interpret input as a bare id
// or as a decoded map from which rich fields can short-circuit the API.
func stringy(spec string) bool {
	return strings.ToLower(spec) == "string"
}

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
