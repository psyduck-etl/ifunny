package main

import "github.com/psyduck-etl/sdk"

// Every resource that talks to the iFunny API needs a bearer token and a
// user agent to authenticate. clientSpecs returns those two shared specs so
// each resource can splice them into its own spec slice without repeating
// the declarations.
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
			Description: "bearer token to authenticate with",
			Type:        sdk.TypeString,
			Required:    true,
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
