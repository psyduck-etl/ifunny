package main

import (
	"fmt"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/psyduck-etl/sdk"
)

// authConfig is the shared authentication surface every API-backed resource
// accepts. Exactly one mode is selected, in priority order:
//
//   - bearer-token: a logged-in user's token — full access, and the only
//     mode the chat (WAMP) resources support.
//   - basic-token: an already-generated-and-primed anonymous basic token —
//     read-only access to public REST endpoints.
//   - generate-basic: mint and prime a fresh basic token at bind time
//     (priming costs a one-time ~15s wait against the API). Its fields
//     render the user-agent, so no top-level user-agent is required.
type authConfig struct {
	BearerToken   string               `psy:"bearer-token"`
	BasicToken    string               `psy:"basic-token"`
	GenerateBasic *generateBasicConfig `psy:"generate-basic"`
	UserAgent     string               `psy:"user-agent"`
}

// generateBasicConfig is the block configuring the generate-basic auth
// mode. Its fields double as the user-agent's device profile: they render
// into a mobile UA string ifunny-go's own Android / IOS types would
// produce. app-version and app-build fall back to ifunny-go's pinned
// constants when empty; platform-name and platform-version are required.
type generateBasicConfig struct {
	AppVersion      string `psy:"app-version"`
	AppBuild        string `psy:"app-build"`
	PlatformName    string `psy:"platform-name"`
	PlatformVersion string `psy:"platform-version"`
}

// renderUA renders the block into a user-agent string matching ifunny-go's
// renderPhoneUA template so a caller can't tell a psyduck-generated basic
// client from a native ifunny-go Android{} / IOS{} client. Brand and
// model are hard-coded to the same values ifunny-go uses (google/Pixel 8,
// Apple/iPhone 15 Pro), so only OS name/version and app version/build are
// caller-controllable.
func (g *generateBasicConfig) renderUA() (ifunny.UserAgent, error) {
	appVersion := g.AppVersion
	if appVersion == "" {
		appVersion = ifunny.APP_VERSION
	}
	appBuild := g.AppBuild
	if appBuild == "" {
		appBuild = ifunny.APP_BUILD
	}

	var os, brand, model string
	switch g.PlatformName {
	case "Android":
		os, brand, model = "Android", "google", "Pixel 8"
	case "iOS", "IOS":
		os, brand, model = "iOS", "Apple", "iPhone 15 Pro"
	default:
		return nil, fmt.Errorf("unknown platform-name %q, want one of: Android, iOS", g.PlatformName)
	}

	return ifunny.RawUserAgent(fmt.Sprintf("iFunny/%s(%s) %s/%s (%s; %s; %s)",
		appVersion, appBuild,
		os, g.PlatformVersion,
		brand, model, brand,
	)), nil
}

// clientFor builds an authenticated iFunny client for the chosen auth mode.
// bearer-token and basic-token modes take the top-level user-agent as a
// plain string; generate-basic renders its own UA from the block's fields.
func clientFor(config *authConfig) (*ifunny.Client, error) {
	switch {
	case config.BearerToken != "":
		return ifunny.MakeClient(config.BearerToken, ifunny.RawUserAgent(config.UserAgent))
	case config.BasicToken != "":
		// A basic-token supplied in config is assumed already primed.
		return ifunny.MakeClientBasic(config.BasicToken, ifunny.RawUserAgent(config.UserAgent))
	case config.GenerateBasic != nil:
		ua, err := config.GenerateBasic.renderUA()
		if err != nil {
			return nil, err
		}
		basic, err := ifunny.GenerateBasic()
		if err != nil {
			return nil, fmt.Errorf("generate basic token: %w", err)
		}
		client, err := ifunny.MakeClientBasic(basic, ua)
		if err != nil {
			return nil, err
		}
		// A freshly generated token must be primed once before use.
		if err := client.PrimeBasic(); err != nil {
			return nil, fmt.Errorf("prime basic token: %w", err)
		}
		return client, nil
	default:
		return nil, fmt.Errorf("one of bearer-token, basic-token, or generate-basic is required")
	}
}

// newClient parses the shared auth options and builds a client. It collapses
// the parse-then-build preamble the transformer providers share.
func newClient(parse sdk.Parser) (*ifunny.Client, *authConfig, error) {
	config := new(authConfig)
	if err := parse(config); err != nil {
		return nil, nil, err
	}
	client, err := clientFor(config)
	return client, config, err
}
