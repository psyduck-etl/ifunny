package scyther

import (
	"time"

	"github.com/gastrodon/psyduck/sdk"
)

func produceQueue(parse func(interface{}) error) sdk.Producer {
	config := scytherConfigDefault()
	if err := parse(config); err != nil {
		panic(err)
	}

	ensureQueue(config)

	return func(signal chan string) chan interface{} {
		data := make(chan interface{}, 32)

		go func() {
			for {
				message, any := getQueueHead(config)
				if !any {
					time.Sleep(1 * time.Second)
					continue
				}

				data <- message
			}
		}()

		return data
	}
}
