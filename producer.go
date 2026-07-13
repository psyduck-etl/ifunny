package main

import (
	"context"

	ifunny "github.com/open-ifunny/ifunny-go"
)

// emitOne encodes v via enc and forwards the bytes onto send. It reports
// encode failures via errs and returns false on encode failure or on ctx
// cancellation, matching the "return on first failure" convention shared
// by every producer in this plugin. Callers stop draining their iterator
// as soon as it returns false.
func emitOne(ctx context.Context, v any, send chan<- []byte, errs chan<- error, enc *emitConfig) bool {
	b, err := enc.Encode(v)
	if err != nil {
		sendErr(ctx, errs, err)
		return false
	}
	select {
	case send <- b:
		return true
	case <-ctx.Done():
		return false
	}
}

// produceIter drains an ifunny result iterator onto send, encoding each
// value via the bound emit codec. It stops when the iterator is exhausted
// (the channel closes and a nil value arrives) or the first error surfaces.
// Item cutoffs and rate limiting are applied by the host around this
// producer, so the iterator is always drained in full here. The producer
// respects context cancellation on send operations.
func produceIter[T any](ctx context.Context, iter <-chan ifunny.Result[*T], send chan<- []byte, errs chan<- error, enc *emitConfig) {
	defer close(send)

	for r := range iter {
		if r.Err != nil {
			sendErr(ctx, errs, r.Err)
			return
		}

		if r.V == nil {
			return
		}

		if !emitOne(ctx, r.V, send, errs, enc) {
			return
		}
	}
}

// contentConfig configures the family of resources keyed by a single
// content id: ifunny-comments, ifunny-smiles, ifunny-republishers.
type contentConfig struct {
	authConfig
	emitConfig
	Content string `psy:"content"`
}
