package main

import (
	"encoding/json"

	"github.com/open-ifunny/ifunny-go"
	"github.com/psyduck-etl/sdk"
)

func produceFeed(parse sdk.Parser, specParse sdk.SpecParser) (sdk.Producer, error) {
	config := new(IFunnyConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	iter := client.IterFeed(config.Feed)

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)
		iters := 0

		for {
			if config.StopAfter != 0 && iters >= config.StopAfter {
				return
			}

			r := <-iter
			if r.Err != nil {
				errs <- r.Err
				return
			}

			b, err := json.Marshal(r.V)
			if err != nil {
				errs <- err
				return
			}

			send <- b
			iters++
		}
	}, nil
}
