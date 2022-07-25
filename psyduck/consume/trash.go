package consume

import (
	"github.com/gastrodon/psyduck/sdk"
)

func Trash(parse func(interface{}) error) sdk.Consumer {
	return func(signal chan string) chan interface{} {
		data := make(chan interface{}, 32)

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

		return data
	}
}
