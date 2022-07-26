package scyther

import (
	"github.com/gastrodon/psyduck/sdk"
)

func consumeQueue(parse func(interface{}) error) (sdk.Consumer, error) {
	config := scytherConfigDefault()
	if err := parse(config); err != nil {
		return nil, err
	}

	if err := ensureQueue(config); err != nil {
		return nil, err
	}

	return func(signal chan string) (chan []byte, chan error) {
		data := make(chan []byte, 32)
		errors := make(chan error)

		next := func(data []byte) (bool, error) {
			return true, putQueueHead(config, data)
		}

		go func() {
			sdk.ConsumeChunk(next, parse, data, errors, signal)
		}()

		return data, errors
	}, nil
}
