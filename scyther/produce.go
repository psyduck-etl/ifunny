package scyther

import (
	"time"

	"github.com/gastrodon/psyduck/sdk"
)

func produceQueue(parse func(interface{}) error) (sdk.Producer, error) {
	config := mustScytherConfig(parse)
	if err := ensureQueue(config); err != nil {
		return nil, err
	}

	return func(signal chan string) (chan []byte, error) {
		data := make(chan []byte, 32)

		next := func() ([]byte, bool, error) {
			message, any, err := getQueueHead(config)
			if err != nil || !any {
				return nil, !config.StopIfExhausted, err
			}

			time.Sleep(time.Duration(config.DelayIfExhausted) * time.Millisecond)
			return message, true, nil
		}

		go func() {
			sdk.ProduceChunk(next, parse, data, signal)
		}()

		return data, nil
	}, nil
}
