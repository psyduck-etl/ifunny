package main

import "github.com/psyduck-etl/sdk"

// Every resource that talks to the iFunny API takes a device-profile
// user-agent plus exactly one auth mode. clientSpecs returns the shared
// specs so each resource can splice them into its own spec slice.
// clientFor (client.go) enforces exactly-one-auth-mode at bind time.
//
// Auth surface:
//
//   - auth-bearer: a logged-in user's OAuth token — full access, and the
//     only mode chat resources support.
//   - auth-basic: either a literal, already-primed basic token, "generate"
//     (mint and prime one at startup — a one-time ~15s handshake), or
//     "generate-cache" (same as generate but persist the primed token to
//     $XDG_CACHE_HOME/psyduck-ifunny/basic-token so subsequent runs skip
//     the handshake).
//
// The user-agent block is required for every auth mode: iFunny expects a
// mobile UA string and the block renders one identical in shape to the
// UAs ifunny-go's Android{} / IOS{} types produce.
//
// Rate limiting (per-minute) and item cutoffs (stop-after) are host-owned
// BlockMeta attributes under the SDK v0.5.0 plugin API — the host decodes
// and enforces them out of band, so resources here never declare them. The
// one exception is ifunny-chat-listen (and ifunny-chat-invites), which
// declare their own stop-after to terminate their websocket subscription
// cleanly (see produce-chat.go).
func clientSpecs() []*sdk.Spec {
	return []*sdk.Spec{
		{
			Name:        "auth-basic",
			Description: `anonymous access — one of: a literal primed basic token, "generate" to mint one at startup, or "generate-cache" to mint once and reuse across runs. Mutually exclusive with auth-bearer.`,
			Type:        sdk.TypeString,
			Default:     "",
		},
		{
			Name:        "auth-bearer",
			Description: "logged-in user's bearer token — full access; required by the chat resources. Mutually exclusive with auth-basic.",
			Type:        sdk.TypeString,
			Default:     "",
		},
		{
			Name:        "user-agent",
			Description: "device profile that renders the request user-agent",
			Type:        sdk.TypeObject,
			Required:    true,
			Fields: []*sdk.Spec{
				{
					Name:        "device",
					Description: "device platform to impersonate, one of: android, ios",
					Type:        sdk.TypeString,
					Required:    true,
				},
				{
					Name:        "device-version",
					Description: "device OS version, e.g. 14 (android) or 17.5.1 (ios)",
					Type:        sdk.TypeString,
					Required:    true,
				},
				{
					Name:        "app-version",
					Description: "iFunny app version to render in the user-agent; empty falls back to ifunny-go's pinned APP_VERSION",
					Type:        sdk.TypeString,
					Default:     "",
				},
				{
					Name:        "app-build",
					Description: "iFunny app build number to render in the user-agent; empty falls back to ifunny-go's pinned APP_BUILD",
					Type:        sdk.TypeString,
					Default:     "",
				},
			},
		},
	}
}

// specs concatenates the shared client specs with resource-specific ones.
func specs(extra ...*sdk.Spec) []*sdk.Spec {
	return append(clientSpecs(), extra...)
}

// encodingSpec returns the shared encoding spec used by producers and transformers.
func encodingSpec() *sdk.Spec {
	return &sdk.Spec{
		Name:        "encoding",
		Description: "encoding for output, e.g. json",
		Type:        sdk.TypeString,
		Default:     "json",
	}
}
