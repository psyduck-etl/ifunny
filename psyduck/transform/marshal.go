package transform

import (
	"encoding/json"

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

func MarshalJSON(parse func(interface{}) error) sdk.Transformer {
	return func(data interface{}) interface{} {
		dataBytes, err := json.Marshal(data)
		if err != nil {
			panic(err)
		}

		return string(dataBytes)
	}
}

func JSONSnippet(parse func(interface{}) error) sdk.Transformer {
	config := mustSnippetConfig(parse)

	return func(data interface{}) interface{} {
		items := make(map[string]interface{}, len(config.Fields))
		if err := json.Unmarshal([]byte(data.(string)), &items); err != nil {
			panic(err)
		}

		dataBytes, err := json.Marshal(items)
		if err != nil {
			panic(err)
		}

		return dataBytes
	}
}
