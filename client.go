package main

import (
	"fmt"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/psyduck-etl/sdk"
)

// authConfig is the shared authentication surface every API-backed resource
// accepts. Exactly one of AuthBasic / AuthBearer must be set; UserAgent is
// required in every mode.
//
// AuthBasic is a multiplexed string: a literal already-primed basic token,
// the string "generate" (mint and prime a fresh one at bind time), or
// "generate-cache" (mint once and cache in $XDG_CACHE_HOME so subsequent
// runs skip the ~15s priming handshake).
type authConfig struct {
	AuthBasic  string           `psy:"auth-basic"`
	AuthBearer string           `psy:"auth-bearer"`
	UserAgent  *userAgentConfig `psy:"user-agent"`
}

// userAgentConfig is the block that renders every request's user-agent.
// Device / DeviceVersion identify the mobile OS the client impersonates;
// AppVersion / AppBuild identify the iFunny app release. The rendered UA
// matches ifunny-go's renderPhoneUA template so a caller can't tell a
// psyduck-plugin-issued request from one an Android{} / IOS{} client would
// make.
type userAgentConfig struct {
	Device        string `psy:"device"`
	DeviceVersion string `psy:"device-version"`
	AppVersion    string `psy:"app-version"`
	AppBuild      string `psy:"app-build"`
}

// renderUA renders the block into an ifunny.UserAgent. Empty AppVersion /
// AppBuild fall back to ifunny-go's pinned constants so most operators
// only need to set device + device-version.
func (u *userAgentConfig) renderUA() (ifunny.UserAgent, error) {
	appVersion := u.AppVersion
	if appVersion == "" {
		appVersion = ifunny.APP_VERSION
	}
	appBuild := u.AppBuild
	if appBuild == "" {
		appBuild = ifunny.APP_BUILD
	}

	// OS / brand / model are hard-coded to what ifunny-go's own
	// Android{} / IOS{} types emit so rendered UAs are indistinguishable
	// from library-native ones.
	var os, brand, model string
	switch u.Device {
	case "android":
		os, brand, model = "Android", "google", "Pixel 8"
	case "ios":
		os, brand, model = "iOS", "Apple", "iPhone 15 Pro"
	default:
		return nil, fmt.Errorf("unknown user-agent device %q, want one of: android, ios", u.Device)
	}

	return ifunny.RawUserAgent(fmt.Sprintf("iFunny/%s(%s) %s/%s (%s; %s; %s)",
		appVersion, appBuild,
		os, u.DeviceVersion,
		brand, model, brand,
	)), nil
}

// clientFor builds an authenticated iFunny client for the chosen auth mode.
// Exactly one of auth-basic / auth-bearer must be set; user-agent is
// mandatory. auth-basic multiplexes on its value — literal token, "generate",
// or "generate-cache".
func clientFor(config *authConfig) (*ifunny.Client, error) {
	if (config.AuthBasic == "") == (config.AuthBearer == "") {
		return nil, fmt.Errorf("exactly one of auth-basic or auth-bearer is required")
	}
	if config.UserAgent == nil {
		return nil, fmt.Errorf("user-agent block is required")
	}

	ua, err := config.UserAgent.renderUA()
	if err != nil {
		return nil, err
	}

	if config.AuthBearer != "" {
		return ifunny.MakeClient(config.AuthBearer, ua)
	}
	return basicClient(config.AuthBasic, ua)
}

// basicClient resolves the auth-basic value into a client:
//   - "generate":       mint + prime a fresh token every bind.
//   - "generate-cache": try the on-disk cache first; on miss mint + prime,
//     then persist. A cached token is assumed still valid — invalidate by
//     removing the cache file (see basicCachePath).
//   - anything else:    treat the value as an already-primed literal token.
func basicClient(auth string, ua ifunny.UserAgent) (*ifunny.Client, error) {
	switch auth {
	case "generate":
		return mintPrimedClient(ua)
	case "generate-cache":
		if token, ok, err := loadCachedBasic(); err != nil {
			return nil, fmt.Errorf("read basic-token cache: %w", err)
		} else if ok {
			return ifunny.MakeClientBasic(token, ua)
		}

		client, token, err := mintPrimedClientWithToken(ua)
		if err != nil {
			return nil, err
		}
		if err := storeCachedBasic(token); err != nil {
			return nil, fmt.Errorf("write basic-token cache: %w", err)
		}
		return client, nil
	default:
		return ifunny.MakeClientBasic(auth, ua)
	}
}

// mintPrimedClient mints a fresh basic token, builds a client, and primes it.
func mintPrimedClient(ua ifunny.UserAgent) (*ifunny.Client, error) {
	client, _, err := mintPrimedClientWithToken(ua)
	return client, err
}

// mintPrimedClientWithToken is mintPrimedClient plus the raw token, so the
// cache path can persist it after priming.
func mintPrimedClientWithToken(ua ifunny.UserAgent) (*ifunny.Client, string, error) {
	basic, err := ifunny.GenerateBasic()
	if err != nil {
		return nil, "", fmt.Errorf("generate basic token: %w", err)
	}
	client, err := ifunny.MakeClientBasic(basic, ua)
	if err != nil {
		return nil, "", err
	}
	if err := client.PrimeBasic(); err != nil {
		return nil, "", fmt.Errorf("prime basic token: %w", err)
	}
	return client, basic, nil
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
