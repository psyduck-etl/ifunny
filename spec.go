package main

import "github.com/psyduck-etl/sdk"

// Every resource that talks to the iFunny API needs a user agent plus one of
// three authentication modes. clientSpecs returns those shared specs so each
// resource can splice them into its own spec slice without repeating the
// declarations. clientFor (client.go) enforces that exactly one auth mode is
// supplied.
//
// generate-basic is an object block; its fields configure the device profile
// used to render the anonymous client's user-agent, so the top-level
// user-agent isn't needed in that mode. bearer-token and basic-token modes
// still require the top-level user-agent.
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
			Description: "mint and prime a fresh basic token at startup (~15s); its fields render the user-agent so no top-level user-agent is required in this mode",
			Type:        sdk.TypeObject,
			Fields: []*sdk.Spec{
				{
					Name:        "app-version",
					Description: "iFunny app version to render in the user-agent; defaults to ifunny-go's pinned value if empty",
					Type:        sdk.TypeString,
					Default:     "",
				},
				{
					Name:        "app-build",
					Description: "iFunny app build number to render in the user-agent; defaults to ifunny-go's pinned value if empty",
					Type:        sdk.TypeString,
					Default:     "",
				},
				{
					Name:        "platform-name",
					Description: "device platform to impersonate, one of: Android, iOS",
					Type:        sdk.TypeString,
					Required:    true,
				},
				{
					Name:        "platform-version",
					Description: "device OS version, e.g. 14 (Android) or 17.5.1 (iOS)",
					Type:        sdk.TypeString,
					Required:    true,
				},
			},
		},
		{
			Name:        "user-agent",
			Description: "user agent to make requests as; required for bearer-token and basic-token modes, unused when generate-basic is set",
			Type:        sdk.TypeString,
			Default:     "",
		},
	}
}

// specs concatenates the shared client specs with resource-specific ones.
func specs(extra ...*sdk.Spec) []*sdk.Spec {
	return append(clientSpecs(), extra...)
}
