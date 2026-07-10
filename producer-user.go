package main

import (
	"context"

	"github.com/psyduck-etl/sdk"
)

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
//	  emit     = "json"
//	}
func produceSmiles(parse sdk.Parser) (sdk.Producer, error) {
	config := &contentConfig{emitConfig: emitConfig{Emit: "json"}}
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
		produceIter(ctx, client.IterSmiles(config.Content), send, errs, &config.emitConfig)
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
//	  emit     = "json"
//	}
func produceRepublishers(parse sdk.Parser) (sdk.Producer, error) {
	config := &contentConfig{emitConfig: emitConfig{Emit: "json"}}
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
		produceIter(ctx, client.IterRepublishers(config.Content), send, errs, &config.emitConfig)
	}, nil
}

// userConfig configures the pair of resources keyed by a single user id:
// ifunny-subscribers and ifunny-subscriptions.
type userConfig struct {
	authConfig
	User string `psy:"user"`
	emitConfig
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
//	  emit     = "json"
//	}
func produceSubscribers(parse sdk.Parser) (sdk.Producer, error) {
	config := &userConfig{emitConfig: emitConfig{Emit: "json"}}
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
		produceIter(ctx, client.IterSubscribers(config.User), send, errs, &config.emitConfig)
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
//	  emit     = "json"
//	}
func produceSubscriptions(parse sdk.Parser) (sdk.Producer, error) {
	config := &userConfig{emitConfig: emitConfig{Emit: "json"}}
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
		produceIter(ctx, client.IterSubscriptions(config.User), send, errs, &config.emitConfig)
	}, nil
}
