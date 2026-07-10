package main

import (
	"context"

	ifunny "github.com/open-ifunny/ifunny-go"
)

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

		b, err := enc.Encode(r.V)
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

// contentConfig configures the family of resources keyed by a single
// content id: ifunny-comments, ifunny-smiles, ifunny-republishers.
type contentConfig struct {
	authConfig
	Content string `psy:"content"`
	emitConfig
}
