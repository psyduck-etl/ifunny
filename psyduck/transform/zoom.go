package transform

import (
	"encoding/json"
	"errors"

	"github.com/gastrodon/psyduck/sdk"
)

type ZoomConfig struct {
	Field string `psy:"field"`
}

type ZoomTarget []byte

func (me *ZoomTarget) UnmarshalJSON(data []byte) error {
	*me = ZoomTarget(data)

	return nil
}

func mustZoomConfig(parse sdk.Parser) *ZoomConfig {
	config := new(ZoomConfig)
	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}

func Zoom(parse sdk.Parser, _ sdk.SpecParser) (sdk.Transformer, error) {
	config := mustZoomConfig(parse)

	return func(data []byte) ([]byte, error) {
		if data == nil {
			return nil, errors.New("data is nil")
		}

		source := make(map[string]ZoomTarget)
		if err := json.Unmarshal(data, &source); err != nil {
			return nil, err
		}

		return source[config.Field], nil
	}, nil
}
