package consume

import (
	"github.com/gastrodon/psyduck/sdk"
)

func Trash(_ sdk.Parser, _ sdk.SpecParser) (sdk.Consumer, error) {
	return func() (chan []byte, chan error) {
		data := make(chan []byte, 32)

		go func() {
			for range data {
			}
		}()

		return data, nil
	}, nil
}
