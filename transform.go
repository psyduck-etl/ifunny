package main

import (
	"context"
	"fmt"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

// authorRef is the minimal user reference emitted by ifunny-author when
// its emit is a rich JSON object. Sparse emission uses the bare user id
// string instead.
type authorRef struct {
	ID   string `json:"id"`
	Nick string `json:"nick"`
}

// tagsEnvelope is the shape ifunny-tags emits: a Content's tag list
// wrapped in {"tags": [...]}. Sparse emission isn't defined for tags —
// a tag list has no terminal reference — and is rejected at bind time.
type tagsEnvelope struct {
	Tags []string `json:"tags"`
}

// extractAuthor pulls the author reference out of any entity's decoded
// map. Content nests it under "creator" (id keyed "id"); comments nest
// it under "user" (id keyed "id"); chat events nest it under "user" but
// key the id "user". Reading every shape lets a single transformer sit
// downstream of posts, comments, and chat messages alike.
//
// It reads the already-decoded map rather than raw bytes so runEnrich
// (which decodes once via the accept codec) doesn't re-parse. A missing
// author returns (authorRef{}, false, nil).
func extractAuthor(m map[string]any) (authorRef, bool) {
	for _, key := range []string{"creator", "user"} {
		nested, ok := m[key].(map[string]any)
		if !ok {
			continue
		}
		id, _ := nested["id"].(string)
		if id == "" {
			// Chat events key the author's id under "user" inside
			// the nested "user" object.
			id, _ = nested["user"].(string)
		}
		if id != "" {
			nick, _ := nested["nick"].(string)
			return authorRef{ID: id, Nick: nick}, true
		}
	}
	return authorRef{}, false
}

// enrichSpec is the per-transformer data table runEnrich walks. Each
// resource (author / tags / content / user / channel) supplies its own,
// and runEnrich holds the shared channel-loop + matrix dispatch.
//
// The matrix (per record): input encoding {sparse, rich} × output
// encoding {sparse, rich} = 4 combinations. Front half = obtain the
// target's terminal ref (needed for sparse-out) or the fetched target
// itself (needed for rich-out); back half = encode via the emit codec.
//
// name       — resource label used in per-record error text.
// targetRef  — extract T's terminal ref from a rich source map.
//              Nil-ok = second return false. A miss triggers the
//              fallback resolve path (fetch the source, extract from
//              its authoritative shape) when possible.
// resolveRef — fetch the source S by its ref, return T's ref.
//              Used on the sparse-in / rich-in-with-miss paths.
//              For same-entity resources (content/user/channel)
//              S=T and this is a no-op that echoes ref back.
// fetchTarget — hydrate T by ref. (nil, nil) = not-found, drops the
//              record. Only called when emit is rich.
type enrichSpec struct {
	name        string
	targetRef   func(m map[string]any) (string, bool)
	resolveRef  func(sourceRef string) (string, error)
	fetchTarget func(targetRef string) (any, error)
}

// runEnrich drains in, dispatching each record through spec + the two
// codecs per the accept/emit matrix. All error sends go through sendErr
// so a cancelled context can't block. Output channel is always closed
// on return.
func runEnrich(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error, accept, emit sdk.Codec, sparseOut bool, spec enrichSpec) {
	defer close(out)

	send := func(b []byte) bool {
		select {
		case out <- b:
			return true
		case <-ctx.Done():
			return false
		}
	}

	// emitRef encodes a target ref via the emit codec (sparse out).
	emitRef := func(ref string) bool {
		b, err := emit.Encode(ref)
		if err != nil {
			return sendErr(ctx, errs, err)
		}
		return send(b)
	}

	// emitRich fetches T by ref and encodes it via the emit codec
	// (rich out). A nil target drops the record.
	emitRich := func(ref string) bool {
		target, err := spec.fetchTarget(ref)
		if err != nil {
			return sendErr(ctx, errs, err)
		}
		if target == nil {
			return true // drop
		}
		b, err := emit.Encode(target)
		if err != nil {
			return sendErr(ctx, errs, err)
		}
		return send(b)
	}

	for {
		select {
		case data, ok := <-in:
			if !ok {
				return
			}
			decoded, err := accept.Decode(data)
			if err != nil {
				if !sendErr(ctx, errs, err) {
					return
				}
				continue
			}

			var ref string
			switch v := decoded.(type) {
			case string:
				// Sparse in: the input is S's terminal ref.
				// Resolve to T's ref (identity resources
				// return it unchanged; cross-entity resources
				// fetch S here).
				r, err := spec.resolveRef(v)
				if err != nil {
					if !sendErr(ctx, errs, err) {
						return
					}
					continue
				}
				ref = r
			case map[string]any:
				// Rich in: try to extract T's ref from the
				// map. On miss, fall back to resolving via
				// the source's own ref (the map should carry
				// S's id under the key resolveRef expects
				// when read via the sparse-in path — we
				// find that ref from the map's "id" field,
				// which the ifunny API always populates).
				if r, ok := spec.targetRef(v); ok {
					ref = r
					break
				}
				sourceRef, ok := v["id"].(string)
				if !ok || sourceRef == "" {
					if !sendErr(ctx, errs, fmt.Errorf("%s: rich input missing target and cannot fall back (no id field)", spec.name)) {
						return
					}
					continue
				}
				r, err := spec.resolveRef(sourceRef)
				if err != nil {
					if !sendErr(ctx, errs, err) {
						return
					}
					continue
				}
				ref = r
			default:
				if !sendErr(ctx, errs, fmt.Errorf("%s: cannot dispatch input of type %T", spec.name, decoded)) {
					return
				}
				continue
			}

			if ref == "" {
				// Not-found or degenerate: drop.
				continue
			}

			var okSend bool
			if sparseOut {
				okSend = emitRef(ref)
			} else {
				okSend = emitRich(ref)
			}
			if !okSend {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// authorTransformer builds the ifunny-author transformer. It maps a
// Content, Comment, or ChatEvent to its author's User.
//
// Matrix per record:
//
//   - accept=string, emit=string: fetch content by id → user id.        1 op
//   - accept=string, emit=json:   fetch content → fetch user.           2 ops
//   - accept=json,   emit=string: read creator.id from map (fallback
//     via content id if missing).                              0 or 1 op
//   - accept=json,   emit=json:   read creator.id from map → fetch user
//     (fallback via content id if missing).                    1 or 2 ops
//
// A record whose author cannot be resolved (e.g. system-authored)
// drops from the pipeline.
//
// Requires auth (shared client surface) — all four matrix cells can
// fetch. Accept / emit default to "json".
//
// Example (glue step between posts and their commenters' timelines):
//
//	transform "ifunny-author" "author" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  accept = "json"
//	  emit   = "string"
//	}
func authorTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := struct {
		authConfig
		Accept string `psy:"accept"`
		Emit   string `psy:"emit"`
	}{Accept: "json", Emit: "json"}
	if err := parse(&config); err != nil {
		return nil, err
	}

	accept, err := codecFor(config.Accept)
	if err != nil {
		return nil, err
	}
	emit, err := codecFor(config.Emit)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	sparseOut := stringy(config.Emit)

	spec := enrichSpec{
		name:      "ifunny-author",
		targetRef: func(m map[string]any) (string, bool) {
			author, ok := extractAuthor(m)
			return author.ID, ok
		},
		// sparse-in / rich-in-miss: fetch the source content and
		// pluck creator.id off it. The Content struct populates
		// Creator.ID on every hydrated response.
		resolveRef: func(contentID string) (string, error) {
			content, err := client.GetContent(contentID)
			if err != nil {
				return "", err
			}
			if content == nil {
				return "", nil
			}
			return content.Creator.ID, nil
		},
		fetchTarget: func(userID string) (any, error) {
			user, err := client.GetUser(compose.UserByID(userID))
			if err != nil {
				if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
					return nil, nil
				}
				return nil, err
			}
			return user, nil
		},
	}

	// Sparse out for author emits the bare user id — but per the
	// design (id-only, drops nick) the emit path in runEnrich passes
	// the ref through emit.Encode(ref). For discrete emit we could
	// still surface {id, nick} by materialising an authorRef; we don't,
	// to keep the matrix strictly two-dimensional (sparse=terminal ref,
	// rich=fetched target). Callers wanting {id, nick} chain
	// ifunny-user after this.
	_ = authorRef{}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		runEnrich(ctx, in, out, errs, accept, emit, sparseOut, spec)
	}, nil
}

// tagsTransformer builds the ifunny-tags transformer. It lifts a
// Content's tag list, emitting {"tags": [...]}.
//
// Matrix per record:
//
//   - accept=string, emit=json: fetch content by id → read .tags.    1 op
//   - accept=json,   emit=json: read map["tags"]; on miss fall back
//     to fetching by the map's "id" field.                    0 or 1 op
//   - emit=string: bind-time error (a tag list has no terminal ref).
//
// A post with no tags is dropped (an empty tag set contributes nothing
// to a census). Requires auth (fetches are possible on either accept).
//
// Note the shape: this emits the whole list as one record, because a
// psyduck transformer is strictly one-in-one-out. Per-tag consumers
// therefore need an explode step upstream of them — see the README.
//
// Example:
//
//	transform "ifunny-tags" "tags" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  accept = "json"
//	  emit   = "json"
//	}
func tagsTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := struct {
		authConfig
		Accept string `psy:"accept"`
		Emit   string `psy:"emit"`
	}{Accept: "json", Emit: "json"}
	if err := parse(&config); err != nil {
		return nil, err
	}

	if stringy(config.Emit) {
		return nil, fmt.Errorf("ifunny-tags: emit %q not supported — a tag list has no terminal reference", config.Emit)
	}

	accept, err := codecFor(config.Accept)
	if err != nil {
		return nil, err
	}
	emit, err := codecFor(config.Emit)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	// tags doesn't fit runEnrich (no terminal target ref; the output
	// is an aggregation rather than an entity). Inlining the loop
	// keeps the extract-or-fetch semantics visible in one place.
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)

		fetchTags := func(contentID string) ([]string, error) {
			content, err := client.GetContent(contentID)
			if err != nil {
				return nil, err
			}
			if content == nil {
				return nil, nil
			}
			return content.Tags, nil
		}

		emitTags := func(tags []string) bool {
			if len(tags) == 0 {
				return true // drop
			}
			b, err := emit.Encode(tagsEnvelope{Tags: tags})
			if err != nil {
				return sendErr(ctx, errs, err)
			}
			select {
			case out <- b:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for {
			select {
			case data, ok := <-in:
				if !ok {
					return
				}
				decoded, err := accept.Decode(data)
				if err != nil {
					if !sendErr(ctx, errs, err) {
						return
					}
					continue
				}

				switch v := decoded.(type) {
				case string:
					tags, err := fetchTags(v)
					if err != nil {
						if !sendErr(ctx, errs, err) {
							return
						}
						continue
					}
					if !emitTags(tags) {
						return
					}
				case map[string]any:
					if raw, ok := v["tags"]; ok {
						// Rich input carries a tag
						// list — trust it (even
						// empty, which drops).
						list, _ := raw.([]any)
						tags := make([]string, 0, len(list))
						for _, t := range list {
							if s, ok := t.(string); ok {
								tags = append(tags, s)
							}
						}
						if !emitTags(tags) {
							return
						}
						continue
					}
					// Rich but missing "tags": fall back
					// by content id.
					contentID, ok := v["id"].(string)
					if !ok || contentID == "" {
						if !sendErr(ctx, errs, fmt.Errorf("ifunny-tags: rich input missing tags and cannot fall back (no id field)")) {
							return
						}
						continue
					}
					tags, err := fetchTags(contentID)
					if err != nil {
						if !sendErr(ctx, errs, err) {
							return
						}
						continue
					}
					if !emitTags(tags) {
						return
					}
				default:
					if !sendErr(ctx, errs, fmt.Errorf("ifunny-tags: cannot dispatch input of type %T", decoded)) {
						return
					}
					continue
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}

// contentTransformer builds the ifunny-content transformer. It hydrates
// a Content reference into a Content entity.
//
// Matrix per record:
//
//   - accept=string, emit=string: identity no-op → bind error.
//   - accept=string, emit=json:   fetch content by id.              1 op
//   - accept=json,   emit=string: read map["id"], emit.             0 ops
//   - accept=json,   emit=json:   read map["id"] → fetch content.   1 op
//
// A not-found content drops. Requires auth.
//
// Example:
//
//	transform "ifunny-content" "hydrate" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  accept = "string"
//	  emit   = "json"
//	}
func contentTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := struct {
		authConfig
		Accept string `psy:"accept"`
		Emit   string `psy:"emit"`
	}{Accept: "json", Emit: "json"}
	if err := parse(&config); err != nil {
		return nil, err
	}

	if stringy(config.Accept) && stringy(config.Emit) {
		return nil, fmt.Errorf("ifunny-content: accept=string emit=string is a no-op (id → same id)")
	}

	accept, err := codecFor(config.Accept)
	if err != nil {
		return nil, err
	}
	emit, err := codecFor(config.Emit)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	sparseOut := stringy(config.Emit)

	spec := enrichSpec{
		name: "ifunny-content",
		targetRef: func(m map[string]any) (string, bool) {
			s, ok := m["id"].(string)
			return s, ok && s != ""
		},
		// Identity: source and target are the same entity, so
		// resolveRef echoes the ref back — no op.
		resolveRef: func(ref string) (string, error) { return ref, nil },
		fetchTarget: func(id string) (any, error) {
			content, err := client.GetContent(id)
			if err != nil {
				if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
					return nil, nil
				}
				return nil, err
			}
			return content, nil
		},
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		runEnrich(ctx, in, out, errs, accept, emit, sparseOut, spec)
	}, nil
}

// userConfigT configures ifunny-user. Exactly one of ByID / ByNick
// must be true — id lookups and nick lookups hit different endpoints
// and can behave differently on edge cases like renames. Named
// userConfigT to avoid colliding with the produce.go userConfig.
type userConfigT struct {
	authConfig
	ByID   bool   `psy:"by-id"`
	ByNick bool   `psy:"by-nick"`
	Accept string `psy:"accept"`
	Emit   string `psy:"emit"`
}

// userTransformer builds the ifunny-user transformer. It hydrates a
// User reference into a User entity, keyed by id (by-id) or nick
// (by-nick).
//
// Matrix per record (by-id mode; by-nick mode fetches to resolve the
// id even on the sparse→sparse cell — a nick and its id are not the
// same reference, so it's genuinely 1 op):
//
//   - by-id  accept=string, emit=string: identity no-op → bind error.
//   - by-id  accept=string, emit=json:   fetch user by id.               1 op
//   - by-id  accept=json,   emit=string: read map["id"], emit.           0 ops
//   - by-id  accept=json,   emit=json:   read map["id"] → fetch user.    1 op
//   - by-nick accept=string, emit=string: fetch to resolve nick → id.    1 op
//   - by-nick accept=string, emit=json:   fetch by nick.                 1 op
//   - by-nick accept=json,   emit=string: fetch by rich.nick → id.       1 op
//   - by-nick accept=json,   emit=json:   fetch by rich.nick → user.     1 op
//
// A not-found user drops. Requires auth.
//
// Example (chain after ifunny-author to hydrate commenter identities):
//
//	transform "ifunny-user" "hydrate" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  by-id  = true
//	  accept = "string"
//	  emit   = "json"
//	}
func userTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := &userConfigT{Accept: "json", Emit: "json"}
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.ByID == config.ByNick {
		return nil, fmt.Errorf("ifunny-user: exactly one of by-id or by-nick is required")
	}
	// by-id sparse→sparse is identity; by-nick sparse→sparse is
	// genuinely a fetch (nick → id) so we don't bind-error it.
	if config.ByID && stringy(config.Accept) && stringy(config.Emit) {
		return nil, fmt.Errorf("ifunny-user: by-id with accept=string emit=string is a no-op (id → same id)")
	}

	accept, err := codecFor(config.Accept)
	if err != nil {
		return nil, err
	}
	emit, err := codecFor(config.Emit)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	sparseOut := stringy(config.Emit)
	byNick := config.ByNick

	// getUserByRef hits the right endpoint per by-id/by-nick and
	// swallows not_found into (nil, nil) so runEnrich drops.
	getUser := func(ref string) (*ifunny.User, error) {
		req := compose.UserByID(ref)
		if byNick {
			req = compose.UserByNick(ref)
		}
		user, err := client.GetUser(req)
		if err != nil {
			if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
				return nil, nil
			}
			return nil, err
		}
		return user, nil
	}

	spec := enrichSpec{
		name: "ifunny-user",
		targetRef: func(m map[string]any) (string, bool) {
			key := "id"
			if byNick {
				key = "nick"
			}
			s, ok := m[key].(string)
			return s, ok && s != ""
		},
		resolveRef: func(ref string) (string, error) {
			if !byNick {
				// by-id: identity — the input ref is the
				// target ref.
				return ref, nil
			}
			// by-nick: nick → id genuinely takes a fetch.
			user, err := getUser(ref)
			if err != nil {
				return "", err
			}
			if user == nil {
				return "", nil
			}
			return user.ID, nil
		},
		fetchTarget: func(ref string) (any, error) {
			// After resolveRef the ref is a user id (by-id
			// mode) or a nick still (by-nick mode via the
			// rich-in path where targetRef returned nick).
			// In by-nick mode we want to fetch by nick so
			// callers get consistent semantics.
			if byNick {
				return getUser(ref)
			}
			// by-id: ref is an id.
			user, err := client.GetUser(compose.UserByID(ref))
			if err != nil {
				if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
					return nil, nil
				}
				return nil, err
			}
			return user, nil
		},
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		runEnrich(ctx, in, out, errs, accept, emit, sparseOut, spec)
	}, nil
}

// channelTransformer builds the ifunny-channel transformer. It hydrates
// a ChatChannel reference into a ChatChannel entity, keyed by name.
//
// Matrix per record (identical shape to ifunny-content but keyed by
// "name" instead of "id"):
//
//   - accept=string, emit=string: identity no-op → bind error.
//   - accept=string, emit=json:   fetch channel by name.             1 op
//   - accept=json,   emit=string: read map["name"], emit.            0 ops
//   - accept=json,   emit=json:   read map["name"] → fetch channel.  1 op
//
// A not-found channel drops. Requires auth.
//
// Example:
//
//	transform "ifunny-channel" "hydrate" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  accept = "string"
//	  emit   = "json"
//	}
func channelTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := struct {
		authConfig
		Accept string `psy:"accept"`
		Emit   string `psy:"emit"`
	}{Accept: "json", Emit: "json"}
	if err := parse(&config); err != nil {
		return nil, err
	}

	if stringy(config.Accept) && stringy(config.Emit) {
		return nil, fmt.Errorf("ifunny-channel: accept=string emit=string is a no-op (name → same name)")
	}

	accept, err := codecFor(config.Accept)
	if err != nil {
		return nil, err
	}
	emit, err := codecFor(config.Emit)
	if err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	chat, err := client.Chat()
	if err != nil {
		return nil, err
	}

	sparseOut := stringy(config.Emit)

	spec := enrichSpec{
		name: "ifunny-channel",
		targetRef: func(m map[string]any) (string, bool) {
			s, ok := m["name"].(string)
			return s, ok && s != ""
		},
		// Identity — channels are self-referential like content.
		resolveRef: func(ref string) (string, error) { return ref, nil },
		fetchTarget: func(name string) (any, error) {
			channel, err := chat.GetChannel(compose.GetChannel(name))
			if err != nil {
				if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
					return nil, nil
				}
				return nil, err
			}
			return channel, nil
		},
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		runEnrich(ctx, in, out, errs, accept, emit, sparseOut, spec)
	}, nil
}
