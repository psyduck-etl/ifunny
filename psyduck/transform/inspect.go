package transform

import (
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
)

func Inspect(parse func(interface{}) error) sdk.Transformer {
	return func(data []byte) []byte {
		fmt.Println(data)
		return data
	}
}
