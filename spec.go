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
// BlockMeta attributes under the SDK v0.5.2 plugin API — the host decodes
// and enforces them out of band, so resources here never declare them.
// This includes the live-subscription chat resources (ifunny-chat-listen,
// ifunny-chat-invites): the host's flow.Producer wrapper cancels their
// ctx at the cutoff and their loops unsubscribe cleanly via ctx.Done.
// Requires a psyduck host with gastrodon/psyduck#29 (flow: cancel inner
// ctx on cutoff).
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

// acceptSpec is a resource's input encoding — transformers take one, and
// so would any future consumer. Producers only emit and take emitSpec
// alone. For transformers it decides the front half of the matrix:
// "string" means the input is a bare terminal ref
// (sparse — a fetch will be needed to obtain any intermediates);
// "json" means the input is an object (rich — we trust it insofar as
// we find it useful, falling back to a fetch keyed by the input's own
// terminal ref when the field we need isn't present).
func acceptSpec() *sdk.Spec {
	return &sdk.Spec{
		Name:        "accept",
		Description: "encoding of records the transformer accepts on input, e.g. json (a rich object) or string (a bare terminal reference)",
		Type:        sdk.TypeString,
		Default:     "json",
	}
}

// emitSpec is a resource's output encoding, shared by producers and
// transformers. For transformers it decides the back half of the matrix:
// "string" means emit the target's terminal ref (no hydration — a bare
// id or name); "json" means emit the fully-hydrated target (always
// fetched — an incoming object is never trusted as _fully_ rich for
// emission). Producers emit whatever their iterator yields, encoded via
// this codec.
func emitSpec() *sdk.Spec {
	return &sdk.Spec{
		Name:        "emit",
		Description: "encoding of records the resource emits, e.g. json (a structured object) or string (a bare terminal reference)",
		Type:        sdk.TypeString,
		Default:     "json",
	}
}
