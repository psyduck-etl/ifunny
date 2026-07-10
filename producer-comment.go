package main

import (
	"context"

	"github.com/psyduck-etl/sdk"
)

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
//	  emit     = "json"
//	}
func produceComments(parse sdk.Parser) (sdk.Producer, error) {
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

	// Fail fast on a bad content id rather than mid-stream.
	if _, err := client.GetContent(config.Content); err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		produceIter(ctx, client.IterComments(config.Content), send, errs, &config.emitConfig)
	}, nil
}

// repliesConfig configures ifunny-replies. Comment ids are only unique
// within their parent Content, so both fields are required.
type repliesConfig struct {
	authConfig
	Content string `psy:"content"`
	Comment string `psy:"comment"`
	emitConfig
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
//	  emit     = "json"
//	}
func produceReplies(parse sdk.Parser) (sdk.Producer, error) {
	config := &repliesConfig{emitConfig: emitConfig{Emit: "json"}}
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
		produceIter(ctx, client.IterReplies(config.Content, config.Comment), send, errs, &config.emitConfig)
	}, nil
}
