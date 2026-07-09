package main

import (
	"context"
	"encoding/json"
	"fmt"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

// authorRef is the minimal user reference emitted by ifunny-author. It is
// the join key a downstream ifunny-timeline / ifunny-subscribers producer
// consumes.
type authorRef struct {
	ID   string `json:"id"`
	Nick string `json:"nick"`
}

// extractAuthor pulls the author reference out of any entity that carries
// one. Content nests it under "creator" (id keyed "id"); comments nest it
// under "user" (id keyed "id"); chat events nest it under "user" but key
// the id "user". Reading every shape lets a single transformer sit
// downstream of posts, comments, and chat messages alike.
func extractAuthor(data []byte) (authorRef, bool, error) {
	// A nested author object: content/comments carry the id under "id",
	// chat events carry it under "user".
	type nested struct {
		ID     string `json:"id"`
		UserID string `json:"user"`
		Nick   string `json:"nick"`
	}
	envelope := new(struct {
		Creator *nested `json:"creator"`
		User    *nested `json:"user"`
	})
	if err := json.Unmarshal(data, envelope); err != nil {
		return authorRef{}, false, err
	}

	for _, n := range []*nested{envelope.Creator, envelope.User} {
		if n == nil {
			continue
		}
		id := n.ID
		if id == "" {
			id = n.UserID
		}
		if id != "" {
			return authorRef{ID: id, Nick: n.Nick}, true, nil
		}
	}

	return authorRef{}, false, nil
}

// authorTransformer builds the ifunny-author transformer. It maps a
// Content, Comment, or ChatEvent to its {id, nick} author reference —
// the seed shape the user-oriented producers (ifunny-timeline,
// ifunny-subscribers, ...) accept. An entity with no resolvable author is
// dropped from the pipeline (simply not written to out), which the host
// treats as "skip this datum".
//
// Takes no config — the transformer is a pure JSON reshape.
//
// Example (glue step between posts and their commenters' timelines):
//
//	transform "ifunny-author" "author" {}
func authorTransformer(sdk.Parser) (sdk.Transformer, error) {
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case data, ok := <-in:
				if !ok {
					return
				}
				author, found, err := extractAuthor(data)
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				if !found {
					// Drop entities with no resolvable author.
					continue
				}
				marshalled, err := json.Marshal(author)
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				select {
				case out <- marshalled:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}

// tagsTransformer builds the ifunny-tags transformer. It lifts a post's
// tag list out of a Content record, emitting {"tags": [...]}. Content
// carries its tags as a plain []string under "tags"; this pulls just that
// field so downstream stages can aggregate it. A post with no tags is
// dropped (simply not written to out) — an empty tag set contributes nothing
// to a census.
//
// Note the shape: this emits the whole list as one record, because a
// psyduck transformer is strictly one-in-one-out and cannot fan a post's
// N tags into N records. Per-tag consumers (e.g. counting distinct tags
// via the mysql plugin, whose mysql-table/mysql-filter operate on one
// scalar field per record) therefore need a one-record-per-tag stream,
// which an explode step upstream of them must provide — see the README.
//
// Takes no config.
//
// Example:
//
//	transform "ifunny-tags" "tags" {}
func tagsTransformer(sdk.Parser) (sdk.Transformer, error) {
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case data, ok := <-in:
				if !ok {
					return
				}
				envelope := new(struct {
					Tags []string `json:"tags"`
				})
				if err := json.Unmarshal(data, envelope); err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				if len(envelope.Tags) == 0 {
					// Drop posts with no tags.
					continue
				}
				marshalled, err := json.Marshal(envelope)
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				select {
				case out <- marshalled:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}

// lookup builds a hydration transformer: it reads an {"id": ...} envelope
// from the input, resolves the full entity via looker, and re-emits it as
// JSON. A nil result from looker (e.g. a not-found user) drops the datum.
func lookup(looker func(id string) (any, error)) sdk.Transformer {
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case data, ok := <-in:
				if !ok {
					return
				}
				identity := new(struct {
					ID string `json:"id"`
				})
				if err := json.Unmarshal(data, identity); err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}

				found, err := looker(identity.ID)
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				if found == nil {
					// Drop entities where the looker returns nil.
					continue
				}

				marshalled, err := json.Marshal(found)
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				select {
				case out <- marshalled:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}

// lookupContent builds the ifunny-lookup-content transformer. It hydrates
// a light Content reference — a record whose "id" field carries the
// content id — into the full Content object.
//
// Takes only the shared auth surface; the input's "id" keys the lookup.
//
// Example:
//
//	transform "ifunny-lookup-content" "hydrate" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	}
func lookupContent(parse sdk.Parser) (sdk.Transformer, error) {
	client, _, err := newClient(parse)
	if err != nil {
		return nil, err
	}

	return lookup(func(id string) (any, error) {
		return client.GetContent(id)
	}), nil
}

// lookupUserConfig configures ifunny-lookup-user. Exactly one of ByID /
// ByNick must be true — id lookups and nick lookups hit different
// endpoints and can behave differently on edge cases like renames, so the
// caller picks explicitly rather than the transformer defaulting.
type lookupUserConfig struct {
	authConfig
	ByID   bool `psy:"by-id"`
	ByNick bool `psy:"by-nick"`
}

// lookupUser builds the ifunny-lookup-user transformer. It hydrates a
// light User reference — a record with "id" and/or "nick" fields, as
// emitted by ifunny-author — into the full User object. A not-found user
// drops the datum rather than failing the pipeline.
//
// Set exactly one of by-id / by-nick to pick which field of the input
// records the lookup keys on.
//
// Example (chain after ifunny-author to hydrate commenter identities):
//
//	transform "ifunny-lookup-user" "hydrate" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  by-id = true
//	}
//
// Example (lookup by nick when the upstream carries handles, not ids):
//
//	transform "ifunny-lookup-user" "by-handle" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  by-nick = true
//	}
func lookupUser(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(lookupUserConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	// Exactly one of by-id / by-nick must be set. Requiring an explicit
	// choice avoids surprising defaults when the upstream datum has both
	// fields (author refs do) — id lookups and nick lookups hit different
	// endpoints and can behave differently on edge cases (renames, etc.).
	if config.ByID == config.ByNick {
		return nil, fmt.Errorf("ifunny-lookup-user: exactly one of by-id or by-nick is required")
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	// The input's "id" or "nick" field keys the lookup depending on which
	// mode is set. Author refs carry both, so either mode chains off the
	// same upstream datum.
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case data, ok := <-in:
				if !ok {
					return
				}
				identity := new(struct {
					ID   string `json:"id"`
					Nick string `json:"nick"`
				})
				if err := json.Unmarshal(data, identity); err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}

				req := compose.UserByID(identity.ID)
				if config.ByNick {
					req = compose.UserByNick(identity.Nick)
				}

				user, err := client.GetUser(req)
				if err != nil {
					// A missing user is not a pipeline error — drop the datum.
					if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
						continue
					}
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}

				marshalled, err := json.Marshal(user)
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				select {
				case out <- marshalled:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}

// lookupChannel builds the ifunny-lookup-channel transformer. It hydrates
// a light ChatChannel reference — a record whose "name" field carries the
// channel name — into the full ChatChannel object.
//
// Channels are keyed by name (not a numeric id), so this transformer
// bypasses the shared lookup helper and reads the "name" field directly.
//
// Example:
//
//	transform "ifunny-lookup-channel" "hydrate" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	}
func lookupChannel(parse sdk.Parser) (sdk.Transformer, error) {
	client, _, err := newClient(parse)
	if err != nil {
		return nil, err
	}

	chat, err := client.Chat()
	if err != nil {
		return nil, err
	}

	// Channels are keyed by name, not a numeric id, so this reads the
	// "name" field rather than going through the shared lookup helper.
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case data, ok := <-in:
				if !ok {
					return
				}
				identity := new(struct {
					Name string `json:"name"`
				})
				if err := json.Unmarshal(data, identity); err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}

				channel, err := chat.GetChannel(compose.GetChannel(identity.Name))
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				if channel == nil {
					// Drop channels not found.
					continue
				}

				marshalled, err := json.Marshal(channel)
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				select {
				case out <- marshalled:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}
