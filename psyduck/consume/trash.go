package consume

import "github.com/gastrodon/psyduck/model"

func Trash(parse func(interface{}) error) model.Consumer {
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
