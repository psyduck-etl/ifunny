package transform

import (
	"fmt"

	"github.com/gastrodon/psyduck/model"
)

func Inspect(parse func(interface{}) error) model.Transformer {
	return func(data interface{}) interface{} {
		fmt.Println(data)
		return data
	}
}
