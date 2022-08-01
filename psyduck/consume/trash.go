package consume

import (
	"github.com/gastrodon/psyduck/sdk"
)

func Trash(_ sdk.Parser, _ sdk.SpecParser) (sdk.Consumer, error) {
	return func(signal chan string, done func()) (chan []byte, chan error) {
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

			done()
		}()

		return data, nil
	}, nil
}
