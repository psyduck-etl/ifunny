package main

import (
	"encoding/json"
	"sync"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

type invitesConfig struct {
	authConfig
	StopAfter int `psy:"stop-after"`
}

// produceChatInvites streams live channel invites received by the logged-in
// user. Like ifunny-chat-listen, the subscription has no natural end so we
// declare stop-after locally; a stop-after of 0 listens until the process
// exits.
//
// Unlike ifunny-chat-listen this is not a per-channel subscription — the
// underlying WAMP topic delivers every invite the current user gets. Each
// invite arrives as a *ChatChannel, which is exactly what a downstream
// chat producer (ifunny-chat-history, ifunny-chat-listen) chains from.
//
// Requires a bearer-token: there is no such thing as an "invited anonymous
// user", so basic-token / generate-basic modes have nothing to receive.
func produceChatInvites(parse sdk.Parser) (sdk.Producer, error) {
	config := new(invitesConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	chat, err := client.Chat()
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		invites := make(chan *ifunny.ChatChannel)
		done := make(chan struct{})

		unsubscribe, err := chat.OnChannelInvite(func(_ int, channel *ifunny.ChatChannel) error {
			select {
			case invites <- channel:
			case <-done:
			}
			return nil
		})
		if err != nil {
			errs <- err
			return
		}

		var once sync.Once
		stop := func() {
			once.Do(func() {
				close(done)
				unsubscribe()
			})
		}
		defer stop()

		count := 0
		for {
			select {
			case channel := <-invites:
				b, err := json.Marshal(channel)
				if err != nil {
					errs <- err
					return
				}
				send <- b

				count++
				if config.StopAfter > 0 && count >= config.StopAfter {
					return
				}
			case <-done:
				return
			}
		}
	}, nil
}

type chatConfig struct {
	authConfig
	Channel string `psy:"channel"`
}

// produceChatHistory backfills a public channel's message history over the
// WAMP connection. IterMessages walks the channel from newest to oldest and
// terminates on its own, so this drains it like any other iterator.
func produceChatHistory(parse sdk.Parser) (sdk.Producer, error) {
	config := new(chatConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	chat, err := client.Chat()
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		iter := chat.IterMessages(compose.ListMessages(config.Channel, 30, compose.NoPage[int]()))
		produceIter(iter, send, errs)
	}, nil
}

type chatListenConfig struct {
	authConfig
	Channel   string `psy:"channel"`
	StopAfter int    `psy:"stop-after"`
}

// produceChatListen streams live events from a public channel. Unlike the
// REST producers, a live subscription never ends on its own — the SDK
// Producer signature carries no cancellation channel — so this resource
// declares its own stop-after to bound the listen and unsubscribe cleanly.
// A stop-after of 0 listens until the process exits.
//
// OnChannelEvent delivers events on the websocket's goroutine via a
// callback. We bridge those onto an internal channel and marshal them on
// the producer goroutine. A done channel lets the callback abandon a send
// the moment we stop, so a late event can never block on a listener that
// has already gone away, and sync.Once makes teardown safe to call twice.
func produceChatListen(parse sdk.Parser) (sdk.Producer, error) {
	config := new(chatListenConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	chat, err := client.Chat()
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		events := make(chan *ifunny.ChatEvent)
		done := make(chan struct{})

		unsubscribe, err := chat.OnChannelEvent(config.Channel, func(event *ifunny.ChatEvent) error {
			select {
			case events <- event:
			case <-done:
			}
			return nil
		})
		if err != nil {
			errs <- err
			return
		}

		var once sync.Once
		stop := func() {
			once.Do(func() {
				close(done)
				unsubscribe()
			})
		}
		defer stop()

		count := 0
		for {
			select {
			case event := <-events:
				b, err := json.Marshal(event)
				if err != nil {
					errs <- err
					return
				}
				send <- b

				count++
				if config.StopAfter > 0 && count >= config.StopAfter {
					return
				}
			case <-done:
				return
			}
		}
	}, nil
}
