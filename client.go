package main

import (
	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/psyduck-etl/sdk"
)

// clientConfig is the subset of options every API-backed resource shares.
type clientConfig struct {
	BearerToken string `psy:"bearer-token"`
	UserAgent   string `psy:"user-agent"`
}

// newClient parses the shared client options and builds an authenticated
// iFunny client. It exists to collapse the identical parse-then-MakeClient
// preamble that every provider would otherwise repeat.
//
// MakeClient eagerly calls /account, so this touches the network at bind
// time — a bad token fails the pipeline before any data flows.
func newClient(parse sdk.Parser) (*ifunny.Client, *clientConfig, error) {
	config := new(clientConfig)
	if err := parse(config); err != nil {
		return nil, nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, nil, err
	}

	return client, config, nil
}
