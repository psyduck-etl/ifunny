package transform

import (
	"encoding/json"
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
)

type SnippetConfig struct {
	Fields []string `yaml:"fields"`
}

func mustSnippetConfig(parse func(interface{}) error) *SnippetConfig {
	config := &SnippetConfig{
		Fields: make([]string, 0),
	}

	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}

func Snippet(parse func(interface{}) error) sdk.Transformer {
	config := mustSnippetConfig(parse)

	return func(data []byte) []byte {
		if data == nil {
			panic(fmt.Errorf("data is nil"))
		}

		source := map[string]interface{}{}
		if err := json.Unmarshal(data, &source); err != nil {
			panic(err)
		}

		items := make(map[string]interface{}, len(config.Fields))
		for _, field := range config.Fields {
			items[field] = source[field]
		}

		dataBytes, err := json.Marshal(items)
		if err != nil {
			panic(err)
		}

		return dataBytes
	}
}
