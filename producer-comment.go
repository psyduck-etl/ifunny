package main

import (
	"context"

	"github.com/psyduck-etl/sdk"
)

// produceComments builds the ifunny-comments producer. It walks the
// comment forest on a single post depth-first: for each top-level
// comment it emits the comment itself and then, if the comment has
// replies, drains that comment's reply iterator before advancing to
// the next top-level comment. Every emission is a Comment entity
// encoded via codec (default "json"). The producer eagerly fetches
// the content once at bind to fail fast on a bad content id rather
// than surfacing the error mid-stream.
//
// This replaces the older split between ifunny-comments and
// ifunny-replies — a downstream consumer walking a post's whole
// comment thread would have had to run two producers and stitch them
// on the client's comment id. The forest walk here does that
// stitching in one stream.
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
		defer close(send)

		for r := range client.IterComments(config.Content) {
			if r.Err != nil {
				sendErr(ctx, errs, r.Err)
				return
			}
			if r.V == nil {
				return
			}
			comment := r.V
			if !emitOne(ctx, comment, send, errs, &config.emitConfig) {
				return
			}
			if comment.Num.Replies == 0 {
				continue
			}
			for rr := range client.IterReplies(config.Content, comment.ID) {
				if rr.Err != nil {
					sendErr(ctx, errs, rr.Err)
					return
				}
				if rr.V == nil {
					break
				}
				if !emitOne(ctx, rr.V, send, errs, &config.emitConfig) {
					return
				}
			}
		}
	}, nil
}
