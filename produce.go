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

// feedConfig configures ifunny-feed. Feed names a global iFunny feed such
// as "featured" or "collective".
type feedConfig struct {
	authConfig
	Feed string `psy:"feed"`
}

// produceFeed builds the ifunny-feed producer. It walks a global iFunny
// feed (featured, collective, etc.) and emits each post as a Content JSON
// entity. iFunny serves the collective feed over POST where every other
// feed is a GET; the ifunny-go client handles that transparently, so
// feed = "collective" just works.
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
//	  stop-after = 100
//	}
func produceFeed(parse sdk.Parser) (sdk.Producer, error) {
	config := new(feedConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		produceIter(client.IterFeed(config.Feed), send, errs)
	}, nil
}

// timelineConfig configures ifunny-timeline. Exactly one of ByID / ByNick
// must be set; setting both or neither errors at bind time. The two modes
// hit different endpoints (id lookup vs nick lookup) and behave differently
// on edge cases like renames.
type timelineConfig struct {
	authConfig
	ByID   string `psy:"by-id"`
	ByNick string `psy:"by-nick"`
}

// produceTimeline builds the ifunny-timeline producer. It walks the posts
// authored by a single user, emitting each as a Content JSON entity. Seed
// the user by id (via by-id) or by nick (via by-nick) — pick whichever the
// upstream stage carries.
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
//	}
func produceTimeline(parse sdk.Parser) (sdk.Producer, error) {
	config := new(timelineConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	// Exactly one of by-id / by-nick must be set. Requiring both would
	// silently favour one; requiring neither has nothing to seed on.
	if (config.ByID == "") == (config.ByNick == "") {
		return nil, fmt.Errorf("ifunny-timeline: exactly one of by-id or by-nick is required")
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		if config.ByNick != "" {
			produceIter(client.IterTimelineByNick(config.ByNick), send, errs)
		} else {
			produceIter(client.IterTimeline(config.ByID), send, errs)
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
}

// produceExplore builds the ifunny-explore producer. It walks one of
// iFunny's explore compilations and emits its entities. Explore is the
// closest thing iFunny has to a search seed: the compilation is a named
// pre-computed list on the server (top of the day, popular users, etc.).
// The producer dispatches to the right iterator based on Kind.
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
//	}
func produceExplore(parse sdk.Parser) (sdk.Producer, error) {
	config := new(exploreConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
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

// contentConfig configures the family of resources keyed by a single
// content id: ifunny-comments, ifunny-smiles, ifunny-republishers.
type contentConfig struct {
	authConfig
	Content string `psy:"content"`
}

// produceComments builds the ifunny-comments producer. It walks the
// comments on a single post and emits each as a Comment JSON entity. The
// producer eagerly fetches the content once at bind to fail fast on a bad
// content id rather than surfacing the error mid-stream.
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
//	}
func produceComments(parse sdk.Parser) (sdk.Producer, error) {
	config := new(contentConfig)
	if err := parse(config); err != nil {
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

	return func(send chan<- []byte, errs chan<- error) {
		produceIter(client.IterComments(config.Content), send, errs)
	}, nil
}

// repliesConfig configures ifunny-replies. Comment ids are only unique
// within their parent Content, so both fields are required.
type repliesConfig struct {
	authConfig
	Content string `psy:"content"`
	Comment string `psy:"comment"`
}

// produceReplies builds the ifunny-replies producer. It walks the replies
// to a single comment on a single post, emitting each as a Comment JSON
// entity.
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
//	}
func produceReplies(parse sdk.Parser) (sdk.Producer, error) {
	config := new(repliesConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		produceIter(client.IterReplies(config.Content, config.Comment), send, errs)
	}, nil
}

// produceSmiles builds the ifunny-smiles producer. It walks the users who
// smiled ("liked") a post, emitting each as a User JSON entity — a seed for
// the user-oriented producers (ifunny-timeline, ifunny-subscribers, ...).
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
//	}
func produceSmiles(parse sdk.Parser) (sdk.Producer, error) {
	config := new(contentConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		produceIter(client.IterSmiles(config.Content), send, errs)
	}, nil
}

// produceRepublishers builds the ifunny-republishers producer. It walks
// the users who republished (reposted) a post, emitting each as a User
// JSON entity.
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
//	}
func produceRepublishers(parse sdk.Parser) (sdk.Producer, error) {
	config := new(contentConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		produceIter(client.IterRepublishers(config.Content), send, errs)
	}, nil
}

// userConfig configures the pair of resources keyed by a single user id:
// ifunny-subscribers and ifunny-subscriptions.
type userConfig struct {
	authConfig
	User string `psy:"user"`
}

// produceSubscribers builds the ifunny-subscribers producer. It walks the
// users following a given user (their followers), emitting each as a User
// JSON entity.
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
//	}
func produceSubscribers(parse sdk.Parser) (sdk.Producer, error) {
	config := new(userConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		produceIter(client.IterSubscribers(config.User), send, errs)
	}, nil
}

// produceSubscriptions builds the ifunny-subscriptions producer. It walks
// the users a given user follows (their subscriptions), emitting each as a
// User JSON entity.
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
//	}
func produceSubscriptions(parse sdk.Parser) (sdk.Producer, error) {
	config := new(userConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		produceIter(client.IterSubscriptions(config.User), send, errs)
	}, nil
}

// channelsConfig configures ifunny-channels. An empty Query is the
// trending-channels feed (a single non-paged fetch); a non-empty Query
// hits the paginated open-channels search.
type channelsConfig struct {
	authConfig
	Query string `psy:"query"`
}

// produceChannels builds the ifunny-channels producer. It emits ChatChannel
// JSON entities — either the trending set (when Query is empty) or the
// search results for Query. Both modes hit REST endpoints, so anonymous
// (auth-basic) clients work; only downstream chat resources need
// auth-bearer.
//
// Example (trending channels):
//
//	produce "ifunny-channels" "trending" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
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
//	}
func produceChannels(parse sdk.Parser) (sdk.Producer, error) {
	config := new(channelsConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
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
