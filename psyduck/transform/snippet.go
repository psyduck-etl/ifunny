package transform

import (
	"encoding/json"
	"errors"

	"github.com/gastrodon/psyduck/sdk"
)

type SnippetConfig struct {
	Fields []string `psy:"fields"`
}

func mustSnippetConfig(parse sdk.Parser) *SnippetConfig {
	config := new(SnippetConfig)
	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}

func Snippet(parse sdk.Parser, _ sdk.SpecParser) (sdk.Transformer, error) {
	config := mustSnippetConfig(parse)

	return func(data []byte) ([]byte, error) {
		if data == nil {
			return nil, errors.New("data is nil")
		}

		source := make(map[string]interface{})
		if err := json.Unmarshal(data, &source); err != nil {
			return nil, err
		}

		items := make(map[string]interface{}, len(config.Fields))
		for _, field := range config.Fields {
			items[field] = source[field]
		}

		dataBytes, err := json.Marshal(items)
		if err != nil {
			return nil, err
		}

		return dataBytes, nil
	}, nil
}
