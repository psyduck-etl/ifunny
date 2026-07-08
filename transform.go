package main

import (
	"encoding/json"

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

// authorTransformer maps a Content, Comment, or ChatEvent to its author
// reference. An entity with no resolvable author is dropped from the
// pipeline (nil output), which the host treats as "skip this datum".
func authorTransformer(sdk.Parser) (sdk.Transformer, error) {
	return func(data []byte) ([]byte, error) {
		author, ok, err := extractAuthor(data)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return json.Marshal(author)
	}, nil
}

// tagsTransformer lifts a post's tag list out of a Content record, emitting
// {"tags": [...]}. Content carries its tags as a plain []string under "tags";
// this pulls just that field so downstream stages can aggregate it. A post
// with no tags is dropped (nil output) — an empty tag set contributes nothing
// to a tag census.
//
// Note the shape: this emits the whole list as one record, because a psyduck
// transformer is strictly one-in-one-out and cannot fan a post's N tags into
// N records. Per-tag consumers (e.g. counting distinct tags via the mysql
// plugin, whose mysql-table/mysql-filter operate on one scalar field per
// record) therefore need a one-record-per-tag stream, which an explode step
// upstream of them must provide — see the README.
func tagsTransformer(sdk.Parser) (sdk.Transformer, error) {
	return func(data []byte) ([]byte, error) {
		envelope := new(struct {
			Tags []string `json:"tags"`
		})
		if err := json.Unmarshal(data, envelope); err != nil {
			return nil, err
		}
		if len(envelope.Tags) == 0 {
			return nil, nil
		}
		return json.Marshal(envelope)
	}, nil
}

// lookup builds a hydration transformer: it reads an {"id": ...} envelope
// from the input, resolves the full entity via looker, and re-emits it as
// JSON. A nil result from looker (e.g. a not-found user) drops the datum.
func lookup(looker func(id string) (any, error)) sdk.Transformer {
	return func(data []byte) ([]byte, error) {
		identity := new(struct {
			ID string `json:"id"`
		})
		if err := json.Unmarshal(data, identity); err != nil {
			return nil, err
		}

		found, err := looker(identity.ID)
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, nil
		}

		return json.Marshal(found)
	}
}

func lookupContent(parse sdk.Parser) (sdk.Transformer, error) {
	client, _, err := newClient(parse)
	if err != nil {
		return nil, err
	}

	return lookup(func(id string) (any, error) {
		return client.GetContent(id)
	}), nil
}

type lookupUserConfig struct {
	authConfig
	ByNick bool `psy:"by-nick"`
}

func lookupUser(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(lookupUserConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	// By id (default) the input's "id" field keys the lookup; by nick the
	// "nick" field does. Author refs carry both, so either mode chains off
	// the same upstream datum.
	return func(data []byte) ([]byte, error) {
		identity := new(struct {
			ID   string `json:"id"`
			Nick string `json:"nick"`
		})
		if err := json.Unmarshal(data, identity); err != nil {
			return nil, err
		}

		req := compose.UserByID(identity.ID)
		if config.ByNick {
			req = compose.UserByNick(identity.Nick)
		}

		user, err := client.GetUser(req)
		if err != nil {
			// A missing user is not a pipeline error — drop the datum.
			if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
				return nil, nil
			}
			return nil, err
		}

		return json.Marshal(user)
	}, nil
}

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
	return func(data []byte) ([]byte, error) {
		identity := new(struct {
			Name string `json:"name"`
		})
		if err := json.Unmarshal(data, identity); err != nil {
			return nil, err
		}

		channel, err := chat.GetChannel(compose.GetChannel(identity.Name))
		if err != nil {
			return nil, err
		}
		if channel == nil {
			return nil, nil
		}

		return json.Marshal(channel)
	}, nil
}
