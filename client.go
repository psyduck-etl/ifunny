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
//     (priming costs a one-time ~15s wait against the API).
type authConfig struct {
	BearerToken   string `psy:"bearer-token"`
	BasicToken    string `psy:"basic-token"`
	GenerateBasic bool   `psy:"generate-basic"`
	UserAgent     string `psy:"user-agent"`
}

// clientFor builds an authenticated iFunny client for the chosen auth mode.
// UserAgent is a plain string in psyduck config; ifunny-go takes a UserAgent
// interface, so we wrap through RawUserAgent at the boundary.
func clientFor(config *authConfig) (*ifunny.Client, error) {
	ua := ifunny.RawUserAgent(config.UserAgent)
	switch {
	case config.BearerToken != "":
		return ifunny.MakeClient(config.BearerToken, ua)
	case config.BasicToken != "":
		// A basic-token supplied in config is assumed already primed.
		return ifunny.MakeClientBasic(config.BasicToken, ua)
	case config.GenerateBasic:
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
