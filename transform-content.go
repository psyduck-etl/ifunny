package main

import (
	"encoding/json"
	"fmt"

	"github.com/open-ifunny/ifunny-go"
	"github.com/psyduck-etl/sdk"
)

// Content Author Transformer
type contentAuthorTransformer struct{}

func (t *contentAuthorTransformer) Transform(data []byte) ([]byte, error) {
	content := new(ifunny.Content)
	if err := json.Unmarshal(data, content); err != nil {
		return nil, fmt.Errorf("can't unmarshal bytes %v as Content: %s", data, err)
	}

	creatorBytes, err := json.Marshal(content.Creator)
	if err != nil {
		return nil, err
	}

	return creatorBytes, nil
}

// Provider type
type contentAuthorProvider struct{}

func (contentAuthorProvider) ProvideTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	return &contentAuthorTransformer{}, nil
}

var ContentAuthorTransformer = contentAuthorProvider{}
