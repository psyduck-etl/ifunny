package transform

import (
	"encoding/json"
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
)

type ZoomConfig struct {
	Field string `yaml:"field"`
}

type ZoomTarget []byte

func (me *ZoomTarget) UnmarshalJSON(data []byte) error {
	*me = ZoomTarget(data)

	return nil
}

func mustZoomConfig(parse func(interface{}) error) *ZoomConfig {
	config := &ZoomConfig{}
	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}

func Zoom(parse func(interface{}) error) sdk.Transformer {
	config := mustZoomConfig(parse)

	return func(data []byte) []byte {
		if data == nil {
			panic(fmt.Errorf("data is nil"))
		}

		source := make(map[string]ZoomTarget)
		if err := json.Unmarshal(data, &source); err != nil {
			panic(err)
		}

		return source[config.Field]
	}
}
