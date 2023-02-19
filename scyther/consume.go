package main

import (
	"github.com/gastrodon/psyduck/sdk"
)

func consumeQueue(parse sdk.Parser, specParse sdk.SpecParser) (sdk.Consumer, error) {
	config := scytherConfigDefault()
	if err := parse(config); err != nil {
		return nil, err
	}

	if err := ensureQueue(config); err != nil {
		return nil, err
	}

	return func() (chan []byte, chan error, chan bool) {
		data := make(chan []byte)
		errors := make(chan error)
		done := make(chan bool)

		next := func(data []byte) (bool, error) {
			return true, putQueueHead(config, data)
		}

		go func() {
			sdk.ConsumeChunk(next, specParse, data, errors)
			done <- true
			close(data)
			close(errors)
			close(done)
		}()

		return data, errors, done
	}, nil
}
