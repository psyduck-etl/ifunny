package main

import (
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
