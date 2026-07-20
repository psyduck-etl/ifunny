package main

import (
	"context"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

// chatConfig configures ifunny-chat-history, keyed by a single public
// channel name.
type chatConfig struct {
	authConfig
	emitConfig
	Channel string `psy:"channel"`
}

// produceChatHistory builds the ifunny-chat-history producer. It backfills
// a public channel's message history over the WAMP connection, emitting
// each ChatEvent via codec (default "json"). IterMessages walks the channel
// newest-to-oldest and terminates on its own, so this drains it like any
// REST iterator.
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
//	  emit     = "json"
//	}
func produceChatHistory(ctx context.Context, parse sdk.Parser) (sdk.Producer, error) {
	config := &chatConfig{emitConfig: emitConfig{Emit: "json"}}
	if err := parse(config); err != nil {
		return nil, err
	}

	if err := config.emitConfig.Bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	chat, err := client.Chat(context.Background())
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		iter := chat.IterMessages(ctx, compose.ListMessages(config.Channel, 30, compose.NONE, 0))
		produceIter(ctx, iter, send, errs, &config.emitConfig)
	}, nil
}

// chatListenConfig configures ifunny-chat-listen.
type chatListenConfig struct {
	authConfig
	emitConfig
	Channel string `psy:"channel"`
}

// produceChatListen builds the ifunny-chat-listen producer. It streams
// live events from a public channel, emitting each ChatEvent via codec
// (default "json"). A live subscription has no natural end, so bound the
// run with the host-owned stop-after BlockMeta — the host's flow.Producer
// wrapper cancels ctx at the cutoff and the producer unsubscribes cleanly
// via ctx.Done.
//
// OnChannelEvent delivers events on the websocket's goroutine via a
// callback. We bridge those onto an internal channel and encode them on
// the producer goroutine. A done channel lets the callback abandon a send
// the moment we stop, so a late event can never block on a listener that
// has already gone away. Teardown runs once, in the producer's defer.
//
// Requires auth-bearer. Takes an emit spec (default "json").
//
// Example:
//
//	produce "ifunny-chat-listen" "live" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  channel = "chat.some-channel-name"
//	  emit    = "json"
//	}
func produceChatListen(ctx context.Context, parse sdk.Parser) (sdk.Producer, error) {
	config := &chatListenConfig{emitConfig: emitConfig{Emit: "json"}}
	if err := parse(config); err != nil {
		return nil, err
	}

	if err := config.emitConfig.Bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	chat, err := client.Chat(context.Background())
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)

		events := make(chan *ifunny.ChatEvent)
		done := make(chan struct{})

		unsubscribe, err := chat.OnChannelEvent(ctx, config.Channel, func(event *ifunny.ChatEvent) error {
			select {
			case events <- event:
			case <-done:
			}
			return nil
		})
		if err != nil {
			sendErr(ctx, errs, err)
			return
		}

		defer unsubscribe()
		defer close(done)

		for {
			select {
			case event := <-events:
				b, err := config.Encode(event)
				if err != nil {
					sendErr(ctx, errs, err)
					return
				}
				select {
				case send <- b:
				case <-ctx.Done():
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
	emitConfig
}

// produceChatInvites builds the ifunny-chat-invites producer. It streams
// live channel invites received by the logged-in user, emitting each as a
// ChatChannel entity encoded via codec (default "json") — the same shape
// ifunny-channels emits, so it chains straight into ifunny-chat-history or
// ifunny-chat-listen. Like ifunny-chat-listen, the subscription has no
// natural end; bound the run with the host-owned stop-after BlockMeta.
//
// Unlike ifunny-chat-listen this is not a per-channel subscription — the
// underlying WAMP topic delivers every invite the current user gets.
//
// Requires auth-bearer: anonymous (auth-basic) clients have nothing to
// receive — an "invited anonymous user" doesn't exist. Takes an encoding
// spec (default "json").
//
// Example:
//
//	produce "ifunny-chat-invites" "incoming" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  emit = "json"
//	}
func produceChatInvites(ctx context.Context, parse sdk.Parser) (sdk.Producer, error) {
	config := &invitesConfig{emitConfig: emitConfig{Emit: "json"}}
	if err := parse(config); err != nil {
		return nil, err
	}

	if err := config.emitConfig.Bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	chat, err := client.Chat(context.Background())
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)

		invites := make(chan *ifunny.ChatChannel)
		done := make(chan struct{})

		unsubscribe, err := chat.OnChannelInvite(ctx, func(_ int, channel *ifunny.ChatChannel) error {
			select {
			case invites <- channel:
			case <-done:
			}
			return nil
		})
		if err != nil {
			sendErr(ctx, errs, err)
			return
		}

		defer unsubscribe()
		defer close(done)

		for {
			select {
			case channel := <-invites:
				b, err := config.Encode(channel)
				if err != nil {
					sendErr(ctx, errs, err)
					return
				}
				select {
				case send <- b:
				case <-ctx.Done():
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

// channelsConfig configures ifunny-channels. An empty Query is the
// trending-channels feed (a single non-paged fetch); a non-empty Query
// hits the paginated open-channels search.
type channelsConfig struct {
	authConfig
	emitConfig
	Query string `psy:"query"`
}

// produceChannels builds the ifunny-channels producer. It emits ChatChannel
// entities encoded via codec (default "json") — either the trending set
// (when Query is empty) or the search results for Query. Both modes hit
// REST endpoints, so anonymous (auth-basic) clients work; only downstream
// chat resources need auth-bearer.
//
// Example (trending channels):
//
//	produce "ifunny-channels" "trending" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  emit     = "json"
//	  stop-after = 10
//	}
//
// Example (search open channels):
//
//	produce "ifunny-channels" "gaming" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  query = "gaming"
//	  emit     = "json"
//	}
func produceChannels(ctx context.Context, parse sdk.Parser) (sdk.Producer, error) {
	config := &channelsConfig{emitConfig: emitConfig{Emit: "json"}}
	if err := parse(config); err != nil {
		return nil, err
	}

	if err := config.emitConfig.Bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	// An empty query means "trending" — a one-shot iterator upstream
	// (no pagination); a query hits the paginated open-channels search.
	if config.Query == "" {
		return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
			produceIter(ctx, client.IterChannelsTrending(ctx), send, errs, &config.emitConfig)
		}, nil
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterChannelsQuery(ctx, config.Query), send, errs, &config.emitConfig)
	}, nil
}
