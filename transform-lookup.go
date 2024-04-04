package main

import (
	"encoding/json"

	"github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

func lookup(looker func(string) (interface{}, error)) (sdk.Transformer, error) {
	return func(data []byte) ([]byte, error) {
		identity := new(struct {
			ID string `json:"id"`
		})

		if err := json.Unmarshal(data, identity); err != nil {
			return nil, err
		}

		found, err := looker(identity.ID)
		if err != nil {
			return nil, err
		}

		foundBytes, err := json.Marshal(found)
		if err != nil {
			return nil, err
		}

		return foundBytes, nil
	}, nil
}

func lookupContent(parse sdk.Parser, _ sdk.SpecParser) (sdk.Transformer, error) {
	config := new(IFunnyConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return lookup(func(id string) (interface{}, error) {
		return client.GetContent(id)
	})
}

func lookupUser(parse sdk.Parser, _ sdk.SpecParser) (sdk.Transformer, error) {
	config := new(IFunnyConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return lookup(func(id string) (interface{}, error) {
		return client.GetUser(compose.UserByID(id))
	})
}
