package main

import (
	"time"

	"github.com/gastrodon/psyduck/sdk"
)

func produceQueue(parse sdk.Parser, specParse sdk.SpecParser) (sdk.Producer, error) {
	config := mustScytherConfig(parse)
	if err := ensureQueue(config); err != nil {
		return nil, err
	}

	return func() (chan []byte, chan error) {
		data := make(chan []byte, 32)
		errors := make(chan error)

		next := func() ([]byte, bool, error) {
			message, any, err := getQueueHead(config)
			if err != nil || !any {
				return nil, !config.StopIfExhausted, err
			}

			time.Sleep(time.Duration(config.DelayIfExhausted) * time.Millisecond)
			return message, true, nil
		}

		go func() {
			sdk.ProduceChunk(next, specParse, data, errors)
			close(data)
			close(errors)
		}()

		return data, errors
	}, nil
}
