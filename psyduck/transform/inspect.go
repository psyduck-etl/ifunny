package transform

import (
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
)

type InspectConfig struct {
	BeString bool `psy:"be-string"`
}

func mustInspectConfig(parse func(interface{}) error) *InspectConfig {
	config := &InspectConfig{
		BeString: true,
	}

	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}

func Inspect(parse func(interface{}) error) sdk.Transformer {
	config := mustInspectConfig(parse)

	if config.BeString {
		return func(data []byte) []byte {
			fmt.Println(string(data))

			return data
		}
	}

	return func(data []byte) []byte {
		fmt.Println(data)

		return data
	}
}
