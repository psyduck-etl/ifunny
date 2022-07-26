package scyther

import (
	"github.com/gastrodon/psyduck/sdk"
)

func consumeQueue(parse func(interface{}) error) sdk.Consumer {
	config := scytherConfigDefault()
	if err := parse(config); err != nil {
		panic(err)
	}

	ensureQueue(config)

	return func(signal chan string) chan []byte {
		data := make(chan []byte, 32)

		go func() {
			for each := range data {
				putQueueHead(config, each)
			}
		}()

		return data
	}
}
