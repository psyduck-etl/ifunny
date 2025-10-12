package main

import (
	"encoding/json"

	"github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

// Lookup Content Transformer
type lookupContentTransformer struct {
	client *ifunny.Client
}

func (t *lookupContentTransformer) Transform(data []byte) ([]byte, error) {
	identity := new(struct {
		ID string `json:"id"`
	})

	if err := json.Unmarshal(data, identity); err != nil {
		return nil, err
	}

	found, err := t.client.GetContent(identity.ID)
	if err != nil {
		return nil, err
	}

	foundBytes, err := json.Marshal(found)
	if err != nil {
		return nil, err
	}

	return foundBytes, nil
}

// Lookup User Transformer
type lookupUserTransformer struct {
	client *ifunny.Client
}

func (t *lookupUserTransformer) Transform(data []byte) ([]byte, error) {
	identity := new(struct {
		ID string `json:"id"`
	})

	if err := json.Unmarshal(data, identity); err != nil {
		return nil, err
	}

	user, err := t.client.GetUser(compose.UserByID(identity.ID))
	if err != nil {
		if apierr, ok := err.(ifunny.APIError); ok && apierr.Kind == "not_found" {
			return nil, nil
		}
		return nil, err
	}

	foundBytes, err := json.Marshal(user)
	if err != nil {
		return nil, err
	}

	return foundBytes, nil
}

// Provider types
type lookupContentProvider struct{}

func (lookupContentProvider) ProvideTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(struct {
		BearerToken string `psy:"bearer-token"`
		UserAgent   string `psy:"user-agent"`
	})
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return &lookupContentTransformer{client: client}, nil
}

type lookupUserProvider struct{}

func (lookupUserProvider) ProvideTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(struct {
		BearerToken string `psy:"bearer-token"`
		UserAgent   string `psy:"user-agent"`
	})
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return &lookupUserTransformer{client: client}, nil
}

var LookupContentTransformer = lookupContentProvider{}
var LookupUserTransformer = lookupUserProvider{}
