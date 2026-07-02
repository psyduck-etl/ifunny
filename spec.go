package main

import "github.com/psyduck-etl/sdk"

// Every resource that talks to the iFunny API needs a user agent plus one of
// three authentication modes. clientSpecs returns those shared specs so each
// resource can splice them into its own spec slice without repeating the
// declarations. clientFor (client.go) enforces that exactly one auth mode is
// supplied.
//
// Rate limiting (per-minute) and item cutoffs (stop-after) are host-owned
// BlockMeta attributes under the SDK v0.5.0 plugin API — the host decodes
// and enforces them out of band, so resources here never declare them. The
// one exception is ifunny-chat-listen, which declares its own stop-after to
// terminate its websocket subscription cleanly (see produce-chat.go).
func clientSpecs() []*sdk.Spec {
	return []*sdk.Spec{
		{
			Name:        "bearer-token",
			Description: "logged-in user's bearer token (full access); required for the chat resources",
			Type:        sdk.TypeString,
			Default:     "",
		},
		{
			Name:        "basic-token",
			Description: "already-primed anonymous basic token for read-only REST access",
			Type:        sdk.TypeString,
			Default:     "",
		},
		{
			Name:        "generate-basic",
			Description: "mint and prime a fresh basic token at startup (~15s) instead of supplying one",
			Type:        sdk.TypeBool,
			Default:     false,
		},
		{
			Name:        "user-agent",
			Description: "user agent to make requests as",
			Type:        sdk.TypeString,
			Required:    true,
		},
	}
}

// specs concatenates the shared client specs with resource-specific ones.
func specs(extra ...*sdk.Spec) []*sdk.Spec {
	return append(clientSpecs(), extra...)
}
