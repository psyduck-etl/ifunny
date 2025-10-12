package main

import (
	"context"
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

type feedConfig struct {
	BearerToken string `psy:"bearer-token"`
	UserAgent   string `psy:"user-agent"`
	Feed        string `psy:"feed"`
	Timeline    string `psy:"timeline"`
	StopAfter   int    `psy:"stop-after"`
}

type exploreConfig struct {
	BearerToken string `psy:"bearer-token"`
	UserAgent   string `psy:"user-agent"`
	Kind        string `psy:"kind"`
	Compilation string `psy:"compilation"`
	StopAfter   int    `psy:"stop-after"`
}

func produceIter[T any](ctx context.Context, iter <-chan ifunny.Result[*T], stopAfter int, send func([]byte) error) error {
	count := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r := <-iter:
			if r.Err != nil {
				return r.Err
			}

			if r.V == nil {
				return nil // End of iterator
			}

			b, err := json.Marshal(r.V)
			if err != nil {
				return err
			}

			if err := send(b); err != nil {
				return err
			}

			count++
			if stopAfter > 0 && count >= stopAfter {
				return nil
			}
		}
	}
}

// Comments Producer
type commentsProducer struct {
	client  *ifunny.Client
	content string
}

func (p *commentsProducer) Start(ctx context.Context, send func([]byte) error) error {
	iter := p.client.IterComments(p.content)
	return produceIter(ctx, iter, 0, send)
}

func (p *commentsProducer) Stop() error {
	return nil
}

// Feed Producer
type feedProducer struct {
	client    *ifunny.Client
	feed      string
	timeline  string
	stopAfter int
}

func (p *feedProducer) Start(ctx context.Context, send func([]byte) error) error {
	if p.timeline != "" {
		iter := p.client.IterTimeline(p.timeline)
		return produceIter(ctx, iter, p.stopAfter, send)
	} else if p.feed != "" {
		iter := p.client.IterFeed(p.feed)
		return produceIter(ctx, iter, p.stopAfter, send)
	}
	return fmt.Errorf("exactly one of feed or timeline is required")
}

func (p *feedProducer) Stop() error {
	return nil
}

// Explore Producer
type exploreProducer struct {
	client      *ifunny.Client
	kind        string
	compilation string
	stopAfter   int
}

func (p *exploreProducer) Start(ctx context.Context, send func([]byte) error) error {
	switch p.kind {
	case "content":
		iter := p.client.IterExploreContent(p.compilation)
		return produceIter(ctx, iter, p.stopAfter, send)
	case "user":
		iter := p.client.IterExploreUser(p.compilation)
		return produceIter(ctx, iter, p.stopAfter, send)
	case "chat":
		iter := p.client.IterExploreChatChannel(p.compilation)
		return produceIter(ctx, iter, p.stopAfter, send)
	default:
		return fmt.Errorf("unknown explore data kind: %s", p.kind)
	}
}

func (p *exploreProducer) Stop() error {
	return nil
}

// Provider types
type commentsProvider struct{}

func (commentsProvider) ProvideProducer(parse sdk.Parser) (sdk.Producer, error) {
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

	return &commentsProducer{
		client:  client,
		content: config.Content,
	}, nil
}

type feedProvider struct{}

func (feedProvider) ProvideProducer(parse sdk.Parser) (sdk.Producer, error) {
	config := new(feedConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return &feedProducer{
		client:    client,
		feed:      config.Feed,
		timeline:  config.Timeline,
		stopAfter: config.StopAfter,
	}, nil
}

type exploreProvider struct{}

func (exploreProvider) ProvideProducer(parse sdk.Parser) (sdk.Producer, error) {
	config := new(exploreConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := ifunny.MakeClient(config.BearerToken, config.UserAgent)
	if err != nil {
		return nil, err
	}

	return &exploreProducer{
		client:      client,
		kind:        config.Kind,
		compilation: config.Compilation,
		stopAfter:   config.StopAfter,
	}, nil
}

var CommentsProducer = commentsProvider{}
var FeedProducer = feedProvider{}
var ExploreProducer = exploreProvider{}
