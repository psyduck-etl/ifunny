package main

import (
	"context"
	"fmt"

	"github.com/psyduck-etl/sdk"
)

// feedConfig configures ifunny-feed. Feed names a global iFunny feed such
// as "featured" or "collective".
type feedConfig struct {
	authConfig
	emitConfig
	Feed string `psy:"feed"`
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
//	  emit       = "json"
//	  stop-after = 100
//	}
func produceFeed(parse sdk.Parser) (sdk.Producer, error) {
	config := &feedConfig{emitConfig: emitConfig{Emit: "json"}}
	if err := parse(config); err != nil {
		return nil, err
	}

	if err := config.emitConfig.bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterFeed(ctx, config.Feed), send, errs, &config.emitConfig)
	}, nil
}

// timelineConfig configures ifunny-timeline. Exactly one of ByID / ByNick
// must be set; setting both or neither errors at bind time. The two modes
// hit different endpoints (id lookup vs nick lookup) and behave differently
// on edge cases like renames.
type timelineConfig struct {
	authConfig
	emitConfig
	ByID   string `psy:"by-id"`
	ByNick string `psy:"by-nick"`
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
//	  emit     = "json"
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
//	  emit     = "json"
//	}
func produceTimeline(parse sdk.Parser) (sdk.Producer, error) {
	config := &timelineConfig{emitConfig: emitConfig{Emit: "json"}}
	if err := parse(config); err != nil {
		return nil, err
	}

	// Exactly one of by-id / by-nick must be set. Requiring both would
	// silently favour one; requiring neither has nothing to seed on.
	if (config.ByID == "") == (config.ByNick == "") {
		return nil, fmt.Errorf("ifunny-timeline: exactly one of by-id or by-nick is required")
	}

	if err := config.emitConfig.bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	if config.ByNick != "" {
		return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
			produceIter(ctx, client.IterTimelineByNick(ctx, config.ByNick), send, errs, &config.emitConfig)
		}, nil
	}
	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterTimeline(ctx, config.ByID), send, errs, &config.emitConfig)
	}, nil
}

// exploreConfig configures ifunny-explore. Compilation names an explore
// compilation (e.g. "content_top_today"); Kind must match the compilation's
// entity type — content_* → "content", users_* → "user", chats_* → "chat".
type exploreConfig struct {
	authConfig
	emitConfig
	Compilation string `psy:"compilation"`
	Kind        string `psy:"kind"`
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
//	  emit        = "json"
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
//	  emit        = "json"
//	}
func produceExplore(parse sdk.Parser) (sdk.Producer, error) {
	config := &exploreConfig{emitConfig: emitConfig{Emit: "json"}}
	if err := parse(config); err != nil {
		return nil, err
	}

	if err := config.emitConfig.bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	switch config.Kind {
	case "content":
		return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
			produceIter(ctx, client.IterExploreContent(ctx, config.Compilation), send, errs, &config.emitConfig)
		}, nil
	case "user":
		return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
			produceIter(ctx, client.IterExploreUser(ctx, config.Compilation), send, errs, &config.emitConfig)
		}, nil
	case "chat":
		return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
			produceIter(ctx, client.IterExploreChatChannel(ctx, config.Compilation), send, errs, &config.emitConfig)
		}, nil
	default:
		return nil, fmt.Errorf("unknown explore kind %q, want one of: content, user, chat", config.Kind)
	}
}
