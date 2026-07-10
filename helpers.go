package main

import (
	"context"

	"github.com/psyduck-etl/sdk"
)

// codecFor resolves a codec spec via the sdk-registered codec factory.
// The host binary (psyduck) installs a factory at startup; standalone
// tests register a stub in TestMain. Spec strings are matched exactly —
// codec names are lowercase, and a config value like "JSON" is rejected
// at bind time rather than silently normalized.
func codecFor(spec string) (sdk.Codec, error) {
	return sdk.GetCodec(spec)
}

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

// acceptConfig is the embeddable input-codec half of a resource config.
// Transformers (and any future consumers) embed it to gain the `accept`
// field plus a bound Decode. Call bind once after parse; it resolves the
// codec and fails fast on an unknown spec.
type acceptConfig struct {
	Accept string `psy:"accept"`
	codec  sdk.Codec
}

func (c *acceptConfig) bind() (err error) {
	c.codec, err = codecFor(c.Accept)
	return err
}

// Decode decodes one record via the bound accept codec.
func (c *acceptConfig) Decode(data []byte) (any, error) {
	return c.codec.Decode(data)
}

// sparse reports whether the accept side carries bare terminal refs.
func (c *acceptConfig) sparse() bool {
	return stringy(c.Accept)
}

// emitConfig is the embeddable output-codec half of a resource config.
// Producers and transformers embed it to gain the `emit` field plus a
// bound Encode. Call bind once after parse.
type emitConfig struct {
	Emit  string `psy:"emit"`
	codec sdk.Codec
}

func (c *emitConfig) bind() (err error) {
	c.codec, err = codecFor(c.Emit)
	return err
}

// Encode encodes one record via the bound emit codec.
func (c *emitConfig) Encode(v any) ([]byte, error) {
	return c.codec.Encode(v)
}

// sparse reports whether the emit side carries bare terminal refs.
func (c *emitConfig) sparse() bool {
	return stringy(c.Emit)
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
