package produce

import (
	"github.com/gastrodon/psyduck/model"
)

type constant struct {
	Value string `psy:"value"`
}

func constantDefault() *constant {
	return &constant{
		Value: "0",
	}
}

func Constant(parse func(interface{}) error) model.Producer {
	config := constantDefault()
	parse(config)

	return func(signal chan string) chan interface{} {
		data := make(chan interface{}, 32)
		alive := make(chan bool, 1)
		alive <- true

		go func() {
			for {
				select {
				case received := <-signal:
					panic(received)
				case <-alive:
					data <- config.Value
					alive <- true
				}
			}
		}()

		return data
	}
}
