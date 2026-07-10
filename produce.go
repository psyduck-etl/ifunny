package main

import (
	"context"
	"fmt"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

// produceIter drains an ifunny result iterator onto send, encoding each
// value via codec. It stops when the iterator is exhausted (the channel
// closes and a nil value arrives) or the first error surfaces. Item cutoffs
// and rate limiting are applied by the host around this producer, so the
// iterator is always drained in full here. The producer respects context
// cancellation on send operations.
func produceIter[T any](ctx context.Context, iter <-chan ifunny.Result[*T], send chan<- []byte, errs chan<- error, codec sdk.Codec) {
	defer close(send)

	for r := range iter {
		if r.Err != nil {
			sendErr(ctx, errs, r.Err)
			return
		}

		if r.V == nil {
			return
		}

		b, err := codec.Encode(r.V)
		if err != nil {
			sendErr(ctx, errs, err)
			return
		}

		select {
		case send <- b:
		case <-ctx.Done():
			return
		}
	}
}

// feedConfig configures ifunny-feed. Feed names a global iFunny feed such
// as "featured" or "collective".
type feedConfig struct {
	authConfig
	Feed     string `psy:"feed"`
	Encoding string `psy:"encoding"`
}

// produceFeed builds the ifunny-feed producer. It walks a global iFunny
// feed (featured, collective, etc.) and emits each post as a Content entity
// encoded via codec (default "json"). iFunny serves the collective feed over
// POST where every other feed is a GET; the ifunny-go client handles that
// transparently, so feed = "collective" just works.
//
// Example:
//
//	produce "ifunny-feed" "featured" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  feed       = "featured"
//	  encoding   = "json"
//	  stop-after = 100
//	}
func produceFeed(parse sdk.Parser) (sdk.Producer, error) {
	config := &feedConfig{Encoding: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	codec, err := codecFor(config.Encoding)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterFeed(config.Feed), send, errs, codec)
	}, nil
}

// timelineConfig configures ifunny-timeline. Exactly one of ByID / ByNick
// must be set; setting both or neither errors at bind time. The two modes
// hit different endpoints (id lookup vs nick lookup) and behave differently
// on edge cases like renames.
type timelineConfig struct {
	authConfig
	ByID     string `psy:"by-id"`
	ByNick   string `psy:"by-nick"`
	Encoding string `psy:"encoding"`
}

// produceTimeline builds the ifunny-timeline producer. It walks the posts
// authored by a single user, emitting each as a Content entity encoded via
// codec (default "json"). Seed the user by id (via by-id) or by nick
// (via by-nick) — pick whichever the upstream stage carries.
//
// Example (by-id, chained from an ifunny-author transformer):
//
//	produce "ifunny-timeline" "hydrated" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  by-id = "1234567890"
//	  encoding = "json"
//	}
//
// Example (by-nick):
//
//	produce "ifunny-timeline" "some-user" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  by-nick = "some-user"
//	  encoding = "json"
//	}
func produceTimeline(parse sdk.Parser) (sdk.Producer, error) {
	config := &timelineConfig{Encoding: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	// Exactly one of by-id / by-nick must be set. Requiring both would
	// silently favour one; requiring neither has nothing to seed on.
	if (config.ByID == "") == (config.ByNick == "") {
		return nil, fmt.Errorf("ifunny-timeline: exactly one of by-id or by-nick is required")
	}

	codec, err := codecFor(config.Encoding)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		if config.ByNick != "" {
			produceIter(ctx, client.IterTimelineByNick(config.ByNick), send, errs, codec)
		} else {
			produceIter(ctx, client.IterTimeline(config.ByID), send, errs, codec)
		}
	}, nil
}

// exploreConfig configures ifunny-explore. Compilation names an explore
// compilation (e.g. "content_top_today"); Kind must match the compilation's
// entity type — content_* → "content", users_* → "user", chats_* → "chat".
type exploreConfig struct {
	authConfig
	Compilation string `psy:"compilation"`
	Kind        string `psy:"kind"`
	Encoding    string `psy:"encoding"`
}

// produceExplore builds the ifunny-explore producer. It walks one of
// iFunny's explore compilations and emits its entities via codec (default
// "json"). Explore is the closest thing iFunny has to a search seed: the
// compilation is a named pre-computed list on the server (top of the day,
// popular users, etc.). The producer dispatches to the right iterator
// based on Kind.
//
// Example (top content of the day, anonymous access):
//
//	produce "ifunny-explore" "top-content" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "ios"
//	    device-version = "17.5.1"
//	  }
//	  compilation = "content_top_today"
//	  kind        = "content"
//	  encoding    = "json"
//	  stop-after  = 25
//	}
//
// Example (popular chat channels, mint a fresh basic token at bind):
//
//	produce "ifunny-explore" "popular-chats" {
//	  auth-basic = "generate"
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  compilation = "chats_popular_last_week"
//	  kind        = "chat"
//	  encoding    = "json"
//	}
func produceExplore(parse sdk.Parser) (sdk.Producer, error) {
	config := &exploreConfig{Encoding: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	codec, err := codecFor(config.Encoding)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	switch config.Kind {
	case "content":
		return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
			produceIter(ctx, client.IterExploreContent(config.Compilation), send, errs, codec)
		}, nil
	case "user":
		return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
			produceIter(ctx, client.IterExploreUser(config.Compilation), send, errs, codec)
		}, nil
	case "chat":
		return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
			produceIter(ctx, client.IterExploreChatChannel(config.Compilation), send, errs, codec)
		}, nil
	default:
		return nil, fmt.Errorf("unknown explore kind %q, want one of: content, user, chat", config.Kind)
	}
}

// contentConfig configures the family of resources keyed by a single
// content id: ifunny-comments, ifunny-smiles, ifunny-republishers.
type contentConfig struct {
	authConfig
	Content  string `psy:"content"`
	Encoding string `psy:"encoding"`
}

// produceComments builds the ifunny-comments producer. It walks the
// comments on a single post and emits each as a Comment entity encoded
// via codec (default "json"). The producer eagerly fetches the content
// once at bind to fail fast on a bad content id rather than surfacing
// the error mid-stream.
//
// Example (mint + cache a basic token so restarts skip the ~15s handshake):
//
//	produce "ifunny-comments" "on-post" {
//	  auth-basic = "generate-cache"
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  content = "abc123"
//	  encoding = "json"
//	}
func produceComments(parse sdk.Parser) (sdk.Producer, error) {
	config := &contentConfig{Encoding: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	codec, err := codecFor(config.Encoding)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	// Fail fast on a bad content id rather than mid-stream.
	if _, err := client.GetContent(config.Content); err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterComments(config.Content), send, errs, codec)
	}, nil
}

// repliesConfig configures ifunny-replies. Comment ids are only unique
// within their parent Content, so both fields are required.
type repliesConfig struct {
	authConfig
	Content  string `psy:"content"`
	Comment  string `psy:"comment"`
	Encoding string `psy:"encoding"`
}

// produceReplies builds the ifunny-replies producer. It walks the replies
// to a single comment on a single post, emitting each as a Comment entity
// encoded via codec (default "json").
//
// Example:
//
//	produce "ifunny-replies" "on-comment" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  content = "abc123"
//	  comment = "def456"
//	  encoding = "json"
//	}
func produceReplies(parse sdk.Parser) (sdk.Producer, error) {
	config := &repliesConfig{Encoding: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	codec, err := codecFor(config.Encoding)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterReplies(config.Content, config.Comment), send, errs, codec)
	}, nil
}

// produceSmiles builds the ifunny-smiles producer. It walks the users who
// smiled ("liked") a post, emitting each as a User entity encoded via codec
// (default "json") — a seed for the user-oriented producers (ifunny-timeline,
// ifunny-subscribers, ...).
//
// Example:
//
//	produce "ifunny-smiles" "on-post" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  content = "abc123"
//	  encoding = "json"
//	}
func produceSmiles(parse sdk.Parser) (sdk.Producer, error) {
	config := &contentConfig{Encoding: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	codec, err := codecFor(config.Encoding)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterSmiles(config.Content), send, errs, codec)
	}, nil
}

// produceRepublishers builds the ifunny-republishers producer. It walks
// the users who republished (reposted) a post, emitting each as a User
// entity encoded via codec (default "json").
//
// Example:
//
//	produce "ifunny-republishers" "on-post" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  content = "abc123"
//	  encoding = "json"
//	}
func produceRepublishers(parse sdk.Parser) (sdk.Producer, error) {
	config := &contentConfig{Encoding: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	codec, err := codecFor(config.Encoding)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterRepublishers(config.Content), send, errs, codec)
	}, nil
}

// userConfig configures the pair of resources keyed by a single user id:
// ifunny-subscribers and ifunny-subscriptions.
type userConfig struct {
	authConfig
	User     string `psy:"user"`
	Encoding string `psy:"encoding"`
}

// produceSubscribers builds the ifunny-subscribers producer. It walks the
// users following a given user (their followers), emitting each as a User
// entity encoded via codec (default "json").
//
// Example:
//
//	produce "ifunny-subscribers" "of-user" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  user = "u9876543210"
//	  encoding = "json"
//	}
func produceSubscribers(parse sdk.Parser) (sdk.Producer, error) {
	config := &userConfig{Encoding: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	codec, err := codecFor(config.Encoding)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterSubscribers(config.User), send, errs, codec)
	}, nil
}

// produceSubscriptions builds the ifunny-subscriptions producer. It walks
// the users a given user follows (their subscriptions), emitting each as a
// User entity encoded via codec (default "json").
//
// Example:
//
//	produce "ifunny-subscriptions" "of-user" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  user = "u9876543210"
//	  encoding = "json"
//	}
func produceSubscriptions(parse sdk.Parser) (sdk.Producer, error) {
	config := &userConfig{Encoding: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	codec, err := codecFor(config.Encoding)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterSubscriptions(config.User), send, errs, codec)
	}, nil
}

// channelsConfig configures ifunny-channels. An empty Query is the
// trending-channels feed (a single non-paged fetch); a non-empty Query
// hits the paginated open-channels search.
type channelsConfig struct {
	authConfig
	Query    string `psy:"query"`
	Encoding string `psy:"encoding"`
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
//	  encoding = "json"
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
//	  encoding = "json"
//	}
func produceChannels(parse sdk.Parser) (sdk.Producer, error) {
	config := &channelsConfig{Encoding: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	codec, err := codecFor(config.Encoding)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	// An empty query means "trending", which is a single non-paged fetch;
	// a query hits the paginated open-channels search.
	if config.Query == "" {
		return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
			defer close(send)

			channels, err := client.GetChannels(compose.ChatsTrending)
			if err != nil {
				sendErr(ctx, errs, err)
				return
			}

			for _, channel := range channels {
				b, err := codec.Encode(channel)
				if err != nil {
					sendErr(ctx, errs, err)
					return
				}
				select {
				case send <- b:
				case <-ctx.Done():
					return
				}
			}
		}, nil
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterChannelsQuery(config.Query), send, errs, codec)
	}, nil
}
