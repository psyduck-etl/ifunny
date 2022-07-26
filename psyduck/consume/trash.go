package consume

import (
	"github.com/gastrodon/psyduck/sdk"
)

func Trash(parse func(interface{}) error) (sdk.Consumer, error) {
	return func(signal chan string) (chan []byte, chan error) {
		data := make(chan []byte, 32)

		go func() {
			for {
				select {
				case received := <-signal:
					panic(received)
				case <-data:
					continue
				}
			}
		}()

		return data, nil
	}, nil
}
