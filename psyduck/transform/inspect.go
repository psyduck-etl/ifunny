package transform

import (
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
)

type InspectConfig struct {
	BeString bool `psy:"be-string"`
}

func mustInspectConfig(parse sdk.Parser) *InspectConfig {
	config := new(InspectConfig)
	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}

func Inspect(parse sdk.Parser, _ sdk.SpecParser) (sdk.Transformer, error) {
	formatter := func(data []byte) interface{} { return data }

	config := mustInspectConfig(parse)
	if config.BeString {
		formatter = func(data []byte) interface{} { return string(data) }
	}

	return func(data []byte) ([]byte, error) {
		fmt.Println(formatter(data))

		return data, nil
	}, nil
}
