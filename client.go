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
//
// NOTE: the basic paths call ifunny-go APIs that the released client does
// not expose yet — ifunny.GenerateBasic, ifunny.PrimeBasic, and
// ifunny.MakeClientBasic. They are written here to the intended interface;
// the upstream work is tracked in the ifunny-go TODO comment on PR #2. Until
// that ships, only the bearer path compiles — this is deliberate: the plugin
// expresses the target shape and ifunny-go catches up.
func clientFor(config *authConfig) (*ifunny.Client, error) {
	switch {
	case config.BearerToken != "":
		return ifunny.MakeClient(config.BearerToken, config.UserAgent)
	case config.BasicToken != "":
		return ifunny.MakeClientBasic(config.BasicToken, config.UserAgent)
	case config.GenerateBasic:
		basic, err := ifunny.GenerateBasic()
		if err != nil {
			return nil, fmt.Errorf("generate basic token: %w", err)
		}
		if err := ifunny.PrimeBasic(basic, config.UserAgent); err != nil {
			return nil, fmt.Errorf("prime basic token: %w", err)
		}
		return ifunny.MakeClientBasic(basic, config.UserAgent)
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
