package main

import (
	"encoding/json"
	"fmt"

	"github.com/open-ifunny/ifunny-go"
	"github.com/psyduck-etl/sdk"
)

type commentConfig struct {
	BearerToken string `psy:"bearer-token"`
	UserAgent   string `psy:"user-agent"`
	Content     string `psy:"content"`
}

func produceIter[T any](iter <-chan ifunny.Result[*T], stopAfter int, send chan<- []byte, errs chan<- error) {
	defer close(send)
	defer close(errs)
	if stopAfter == 0 {
		for {
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
		}
	}

	for i := 0; i < stopAfter; i++ {
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
	}
}

func produceComments(parse sdk.Parser) (sdk.Producer, error) {
	config := new(commentConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	if _, err := client.GetContent(config.Content); err != nil {
		return nil, err
	}

	iter := client.IterComments(config.Content)
	return func(send chan<- []byte, errs chan<- error) {
		produceIter(iter, 0, send, errs)
	}, nil
}

type feedConfig struct {
	BearerToken string `psy:"bearer-token"`
	UserAgent   string `psy:"user-agent"`
	Feed        string `psy:"feed"`
	Timeline    string `psy:"timeline"`
	StopAfter   int    `psy:"stop-after"`
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

	switch {
	case config.Timeline != "":
		return func(send chan<- []byte, errs chan<- error) {
			produceIter(client.IterTimeline(config.Timeline), config.StopAfter, send, errs)
		}, nil
	case config.Feed != "":
		return func(send chan<- []byte, errs chan<- error) {
			produceIter(client.IterFeed(config.Feed), config.StopAfter, send, errs)
		}, nil
	default:
		return nil, fmt.Errorf("exactly one of feed or timeline is required")
	}
}

type exploreConfig struct {
	BearerToken string `psy:"bearer-token"`
	UserAgent   string `psy:"user-agent"`
	Kind        string `psy:"kind"`
	Compilation string `psy:"compilation"`
	StopAfter   int    `psy:"stop-after"`
}

func produceExplore(parse sdk.Parser) (sdk.Producer, error) {
	config := new(exploreConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	switch config.Kind {
	case "content":
		return func(send chan<- []byte, errs chan<- error) {
			produceIter(client.IterExploreContent(config.Compilation), config.StopAfter, send, errs)
		}, nil
	case "user":
		return func(send chan<- []byte, errs chan<- error) {
			produceIter(client.IterExploreUser(config.Compilation), config.StopAfter, send, errs)
		}, nil
	case "chat":
		return func(send chan<- []byte, errs chan<- error) {
			produceIter(client.IterExploreChatChannel(config.Compilation), config.StopAfter, send, errs)
		}, nil
	default:
		return nil, fmt.Errorf("unknown explore data kind: %s", config.Kind)
	}
}
