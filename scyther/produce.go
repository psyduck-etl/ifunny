package scyther

import (
	"time"

	"github.com/gastrodon/psyduck/sdk"
)

func produceQueue(parse func(interface{}) error) sdk.Producer {
	config := mustScytherConfig(parse)
	ensureQueue(config)

	return func(signal chan string) chan []byte {
		data := make(chan []byte, 32)

		next := func() ([]byte, bool) {
			message, any := getQueueHead(config)

			if any {
				return message, true
			}

			if config.StopIfExhausted {
				return nil, false
			}

			time.Sleep(time.Duration(config.DelayIfExhausted) * time.Millisecond)
			return nil, true
		}

		go func() {
			sdk.ProduceChunk(next, parse, data, signal)
		}()

		return data
	}
}
