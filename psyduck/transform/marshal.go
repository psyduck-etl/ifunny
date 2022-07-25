package transform

import (
	"encoding/json"

	"github.com/gastrodon/psyduck/sdk"
)

func MarshalJSON(parse func(interface{}) error) sdk.Transformer {
	return func(data interface{}) interface{} {
		dataBytes, err := json.Marshal(data)
		if err != nil {
			panic(err)
		}

		return string(dataBytes)
	}
}
