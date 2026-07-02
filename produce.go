package main

import (
	"encoding/json"
	"fmt"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

// produceIter drains an ifunny result iterator onto send, marshalling each
// value to JSON. It stops when the iterator is exhausted (the channel
// closes and a nil value arrives) or the first error surfaces. Item cutoffs
// and rate limiting are applied by the host around this producer, so the
// iterator is always drained in full here.
func produceIter[T any](iter <-chan ifunny.Result[*T], send chan<- []byte, errs chan<- error) {
	defer close(send)
	defer close(errs)

	for r := range iter {
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

type feedConfig struct {
	clientConfig
	Feed string `psy:"feed"`
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

	return func(send chan<- []byte, errs chan<- error) {
		produceIter(client.IterFeed(config.Feed), send, errs)
	}, nil
}

type timelineConfig struct {
	clientConfig
	User   string `psy:"user"`
	ByNick bool   `psy:"by-nick"`
}

func produceTimeline(parse sdk.Parser) (sdk.Producer, error) {
	config := new(timelineConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		if config.ByNick {
			pageInto(client, "content", "/timelines/users/by_nick/"+config.User, send, errs)
		} else {
			produceIter(client.IterTimeline(config.User), send, errs)
		}
	}, nil
}

type exploreConfig struct {
	clientConfig
	Compilation string `psy:"compilation"`
	Kind        string `psy:"kind"`
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
			produceIter(client.IterExploreContent(config.Compilation), send, errs)
		}, nil
	case "user":
		return func(send chan<- []byte, errs chan<- error) {
			produceIter(client.IterExploreUser(config.Compilation), send, errs)
		}, nil
	case "chat":
		return func(send chan<- []byte, errs chan<- error) {
			produceIter(client.IterExploreChatChannel(config.Compilation), send, errs)
		}, nil
	default:
		return nil, fmt.Errorf("unknown explore kind %q, want one of: content, user, chat", config.Kind)
	}
}

type contentConfig struct {
	clientConfig
	Content string `psy:"content"`
}

func produceComments(parse sdk.Parser) (sdk.Producer, error) {
	config := new(contentConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	// Fail fast on a bad content id rather than mid-stream.
	if _, err := client.GetContent(config.Content); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		produceIter(client.IterComments(config.Content), send, errs)
	}, nil
}

type repliesConfig struct {
	clientConfig
	Content string `psy:"content"`
	Comment string `psy:"comment"`
}

func produceReplies(parse sdk.Parser) (sdk.Producer, error) {
	config := new(repliesConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		pageInto(client, "replies", "/content/"+config.Content+"/comments/"+config.Comment+"/replies", send, errs)
	}, nil
}

func produceSmiles(parse sdk.Parser) (sdk.Producer, error) {
	config := new(contentConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		pageInto(client, "users", "/content/"+config.Content+"/smiles", send, errs)
	}, nil
}

func produceRepublishers(parse sdk.Parser) (sdk.Producer, error) {
	config := new(contentConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		pageInto(client, "users", "/content/"+config.Content+"/republished", send, errs)
	}, nil
}

type userConfig struct {
	clientConfig
	User string `psy:"user"`
}

func produceSubscribers(parse sdk.Parser) (sdk.Producer, error) {
	config := new(userConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		pageInto(client, "users", "/users/"+config.User+"/subscribers", send, errs)
	}, nil
}

func produceSubscriptions(parse sdk.Parser) (sdk.Producer, error) {
	config := new(userConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		pageInto(client, "users", "/users/"+config.User+"/subscriptions", send, errs)
	}, nil
}

type channelsConfig struct {
	clientConfig
	Query string `psy:"query"`
}

func produceChannels(parse sdk.Parser) (sdk.Producer, error) {
	config := new(channelsConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	// An empty query means "trending", which is a single non-paged fetch;
	// a query hits the paginated open-channels search.
	if config.Query == "" {
		return func(send chan<- []byte, errs chan<- error) {
			defer close(send)
			defer close(errs)

			channels, err := client.GetChannels(compose.ChatsTrending)
			if err != nil {
				errs <- err
				return
			}

			for _, channel := range channels {
				b, err := json.Marshal(channel)
				if err != nil {
					errs <- err
					return
				}
				send <- b
			}
		}, nil
	}

	return func(send chan<- []byte, errs chan<- error) {
		produceIter(client.IterChannelsQuery(config.Query), send, errs)
	}, nil
}
