package main

import (
	"encoding/json"
	"fmt"

	"github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

type feedConfig struct {
	BearerToken string `psy:"bearer-token"`
	UserAgent   string `psy:"user-agent"`

	Feed      string `psy:"feed"`
	Timeline  string `psy:"timeline"`
	StopAfter int    `psy:"stop-after"`
}

func produceFeed(parse sdk.Parser) (sdk.Producer, error) {
	config := new(feedConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	var iter <-chan ifunny.Result[*ifunny.Content]
	switch {
	case config.Timeline != "":
		iter = client.IterFeed(config.Timeline, compose.Timeline)
	case config.Feed != "":
		iter = client.IterFeed(config.Feed, compose.Feed)
	default:
		return nil, fmt.Errorf("exactly one of feed or timeline is required")
	}

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

			if r.V == nil {
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
