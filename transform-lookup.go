package main

import (
	"encoding/json"

	"github.com/psyduck-etl/sdk"
)

func lookup(looker func(string) (interface{}, error)) (sdk.Transformer, error) {
	return func(data []byte) ([]byte, error) {
		who := new(Identity)
		if err := json.Unmarshal(data, who); err != nil {
			return nil, err
		}

		found, err := looker(who.ID)
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
	config := mustConfig(parse)

	return lookup(func(id string) (interface{}, error) {
		return getContent(config, id)
	})

}

func lookupUser(parse sdk.Parser, _ sdk.SpecParser) (sdk.Transformer, error) {
	config := mustConfig(parse)

	return lookup(func(id string) (interface{}, error) {
		return getUser(config, id)
	})
}
