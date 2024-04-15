package main

import (
	"encoding/json"
	"fmt"

	"github.com/open-ifunny/ifunny-go"
	"github.com/psyduck-etl/sdk"
)

func getContentAuthor(parse sdk.Parser) (sdk.Transformer, error) {
	return func(data []byte) ([]byte, error) {
		content := new(ifunny.Content)
		if err := json.Unmarshal(data, content); err != nil {
			panic(fmt.Errorf("can't unmarshal bytes %v as Content: %s", data, err))
		}

		creatorBytes, err := json.Marshal(content.Creator)
		if err != nil {
			panic(err)
		}

		return creatorBytes, nil
	}, nil
}
