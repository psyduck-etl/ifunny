package main

import (
	"context"
	"encoding/json"
	"sync"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

// chatConfig configures ifunny-chat-history, keyed by a single public
// channel name.
type chatConfig struct {
	authConfig
	Channel string `psy:"channel"`
}

// produceChatHistory builds the ifunny-chat-history producer. It backfills
// a public channel's message history over the WAMP connection, emitting
// each ChatEvent as JSON. IterMessages walks the channel newest-to-oldest
// and terminates on its own, so this drains it like any REST iterator.
//
// Requires auth-bearer: iFunny's chat WAMP handshake authenticates with a
// bearer ticket and rejects anonymous (basic) clients.
//
// Example:
//
//	produce "ifunny-chat-history" "backfill" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  channel = "chat.some-channel-name"
//	}
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

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		iter := chat.IterMessages(compose.ListMessages(config.Channel, 30, compose.NoPage[int]()))
		produceIter(ctx, iter, send, errs)
	}, nil
}

// chatListenConfig configures ifunny-chat-listen. StopAfter bounds the
// live subscription (0 = listen until the process exits); the field lives
// on the resource rather than as a host-owned attribute because the WAMP
// callback pattern needs to know the bound to tear its bridge down.
type chatListenConfig struct {
	authConfig
	Channel   string `psy:"channel"`
	StopAfter int    `psy:"stop-after"`
}

// produceChatListen builds the ifunny-chat-listen producer. It streams
// live events from a public channel, emitting each ChatEvent as JSON. A
// live subscription has no natural end — the SDK Producer signature carries
// no cancellation channel — so the resource declares its own stop-after to
// bound the listen and unsubscribe cleanly. StopAfter of 0 listens until
// the process exits.
//
// OnChannelEvent delivers events on the websocket's goroutine via a
// callback. We bridge those onto an internal channel and marshal them on
// the producer goroutine. A done channel lets the callback abandon a send
// the moment we stop, so a late event can never block on a listener that
// has already gone away, and sync.Once makes teardown safe to call twice.
//
// Requires auth-bearer.
//
// Example:
//
//	produce "ifunny-chat-listen" "live" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  channel    = "chat.some-channel-name"
//	  stop-after = 100
//	}
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

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)

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
			select {
			case errs <- err:
			case <-ctx.Done():
			}
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
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					return
				}
				select {
				case send <- b:
				case <-ctx.Done():
					return
				}

				count++
				if config.StopAfter > 0 && count >= config.StopAfter {
					return
				}
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}, nil
}

// invitesConfig configures ifunny-chat-invites. Unlike the other chat
// resources it takes no channel field — the underlying WAMP topic is
// scoped to the authenticated user, not to a specific channel.
type invitesConfig struct {
	authConfig
	StopAfter int `psy:"stop-after"`
}

// produceChatInvites builds the ifunny-chat-invites producer. It streams
// live channel invites received by the logged-in user, emitting each as a
// ChatChannel JSON entity — the same shape ifunny-channels emits, so it
// chains straight into ifunny-chat-history or ifunny-chat-listen. Like
// ifunny-chat-listen, the subscription has no natural end, so this
// resource declares its own stop-after (0 = listen until process exits).
//
// Unlike ifunny-chat-listen this is not a per-channel subscription — the
// underlying WAMP topic delivers every invite the current user gets.
//
// Requires auth-bearer: anonymous (auth-basic) clients have nothing to
// receive — an "invited anonymous user" doesn't exist.
//
// Example:
//
//	produce "ifunny-chat-invites" "incoming" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  stop-after = 10
//	}
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

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)

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
			select {
			case errs <- err:
			case <-ctx.Done():
			}
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
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					return
				}
				select {
				case send <- b:
				case <-ctx.Done():
					return
				}

				count++
				if config.StopAfter > 0 && count >= config.StopAfter {
					return
				}
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}, nil
}
