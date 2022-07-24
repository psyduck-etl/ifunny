package scyther

import (
	"github.com/gastrodon/psyduck/model"
)

func consumeQueue(parse func(interface{}) error) model.Consumer {
	config := scytherConfigDefault()
	if err := parse(config); err != nil {
		panic(err)
	}

	ensureQueue(config)

	return func(signal chan string) chan interface{} {
		data := make(chan interface{}, 32)

		go func() {
			for each := range data {
				putQueueHead(config, []byte(each.(string)))
			}
		}()

		return data
	}
}
