package main

import (
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
// author returns (authorRef{}, false).
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

// parseUserBy validates a "by" or "emit-by" string against the two
// user reference axes. Returns true iff the caller should key on nick.
// The resource name is included in the error so ifunny-user vs
// ifunny-author callers get a self-locating message.
func parseUserBy(v, resource string) (byNick bool, err error) {
	switch v {
	case "id":
		return false, nil
	case "nick":
		return true, nil
	default:
		return false, fmt.Errorf("%s: unrecognized user reference axis %q; want \"id\" or \"nick\"", resource, v)
	}
}

// enrichSpec is the per-transformer data table runEnrich walks. Each
// resource (author / tags / content / user / channel) supplies its own,
// and runEnrich holds the shared per-record matrix dispatch.
//
// The matrix (per record): input encoding {sparse, rich} × output
// encoding {sparse, rich} = 4 combinations. Front half = obtain the
// target's terminal ref (needed for sparse-out) or the fetched target
// itself (needed for rich-out); back half = encode via the emit codec.
//
// name       — resource label used in per-record error text.
// targetRef  — extract T's terminal ref from a rich source map.
//
//	Nil-ok = second return false. A miss triggers the
//	fallback resolve path (fetch the source, extract from
//	its authoritative shape) when possible.
//
// resolveRef — fetch the source S by its ref, return T's ref.
//
//	Used on the sparse-in / rich-in-with-miss paths.
//	For same-entity resources (content/user/channel)
//	S=T and this is a no-op that echoes ref back.
//
// fetchTarget — hydrate T by ref. (nil, nil) = not-found, drops the
//
//	record. Only called when emit is rich.
type enrichSpec struct {
	name        string
	targetRef   func(m map[string]any) (string, bool)
	resolveRef  func(sourceRef string) (string, error)
	fetchTarget func(targetRef string) (any, error)
}

// runEnrich is the shared per-record body walked by the five enrich
// transformers. Given a decoded input (accept side), it obtains the
// target's terminal ref, then either emits the ref (sparse out) or
// fetches T and emits that (rich out). Returns (nil, nil) to drop a
// record (not-found, empty rich author, per-record miss without
// fallback), and (data, nil) to emit.
//
// Errors are returned; the SDK per-record transformer contract feeds
// them upstream, so no errs channel is needed.
func runEnrich(data []byte, accept *acceptConfig, emit *emitConfig, spec enrichSpec) ([]byte, error) {
	decoded, err := accept.Decode(data)
	if err != nil {
		return nil, err
	}

	var ref string
	switch v := decoded.(type) {
	case string:
		// Sparse in: the input is S's terminal ref. resolveRef
		// echoes it back on identity resources and fetches S on
		// cross-entity ones (e.g. author reads content → creator.id).
		r, err := spec.resolveRef(v)
		if err != nil {
			return nil, err
		}
		if r == "" {
			return nil, nil // source not found → drop
		}
		ref = r
	case map[string]any:
		// Rich in fast path: try to extract T's ref from the map.
		if r, ok := spec.targetRef(v); ok {
			ref = r
			break
		}
		// Rich in fallback: use the source's own id to fetch S
		// and re-extract T's ref from the authoritative response.
		sourceRef, ok := v["id"].(string)
		if !ok || sourceRef == "" {
			return nil, fmt.Errorf("%s: rich input missing target and cannot fall back (no id field)", spec.name)
		}
		r, err := spec.resolveRef(sourceRef)
		if err != nil {
			return nil, err
		}
		if r == "" {
			return nil, nil // source not found → drop
		}
		ref = r
	default:
		return nil, fmt.Errorf("%s: cannot dispatch input of type %T", spec.name, decoded)
	}

	if emit.sparse() {
		return emit.Encode(ref)
	}
	target, err := spec.fetchTarget(ref)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, nil // not found → drop
	}
	return emit.Encode(target)
}

// authorTransformer builds the ifunny-author transformer. It maps a
// Content, Comment, or ChatEvent to its author's User. The emit-by
// field ("id" default, or "nick") names the user reference axis
// throughout — it decides which creator/user field on the source is
// the target ref, which endpoint fetchTarget calls, and what
// sparse-out emits.
//
// Matrix per record:
//
//   - accept=string, emit=string: fetch content by id → creator ref.    1 op
//   - accept=string, emit=json:   fetch content → fetch user.           2 ops
//   - accept=json,   emit=string: read creator.<emit-by> from map
//     (fallback via content id if missing).                    0 or 1 op
//   - accept=json,   emit=json:   read creator.<emit-by> from map → fetch
//     user (fallback via content id if missing).               1 or 2 ops
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
//	  emit-by = "id"    # or "nick"
//	  accept  = "json"
//	  emit    = "string"
//	}
func authorTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := struct {
		authConfig
		acceptConfig
		emitConfig
		EmitBy string `psy:"emit-by"`
	}{
		acceptConfig: acceptConfig{Accept: "json"},
		emitConfig:   emitConfig{Emit: "json"},
		EmitBy:       "id",
	}
	if err := parse(&config); err != nil {
		return nil, err
	}

	byNick, err := parseUserBy(config.EmitBy, "ifunny-author")
	if err != nil {
		return nil, err
	}

	if err := config.acceptConfig.bind(); err != nil {
		return nil, err
	}
	if err := config.emitConfig.bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	pickCreatorField := func(c *ifunny.Content) string {
		if byNick {
			return c.Creator.Nick
		}
		return c.Creator.ID
	}
	pickAuthorRef := func(a authorRef) string {
		if byNick {
			return a.Nick
		}
		return a.ID
	}

	spec := enrichSpec{
		name: "ifunny-author",
		targetRef: func(m map[string]any) (string, bool) {
			author, ok := extractAuthor(m)
			if !ok {
				return "", false
			}
			ref := pickAuthorRef(author)
			return ref, ref != ""
		},
		resolveRef: func(contentID string) (string, error) {
			content, err := client.GetContent(contentID)
			if err != nil {
				return "", err
			}
			if content == nil {
				return "", nil
			}
			return pickCreatorField(content), nil
		},
		fetchTarget: func(ref string) (any, error) {
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
		},
	}

	return func(data []byte) ([]byte, error) {
		return runEnrich(data, &config.acceptConfig, &config.emitConfig, spec)
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
		acceptConfig
		emitConfig
	}{
		acceptConfig: acceptConfig{Accept: "json"},
		emitConfig:   emitConfig{Emit: "json"},
	}
	if err := parse(&config); err != nil {
		return nil, err
	}

	if config.emitConfig.sparse() {
		return nil, fmt.Errorf("ifunny-tags: emit %q not supported — a tag list has no terminal reference", config.Emit)
	}

	if err := config.acceptConfig.bind(); err != nil {
		return nil, err
	}
	if err := config.emitConfig.bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

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

	return func(data []byte) ([]byte, error) {
		decoded, err := config.acceptConfig.Decode(data)
		if err != nil {
			return nil, err
		}
		var tags []string
		switch v := decoded.(type) {
		case string:
			t, err := fetchTags(v)
			if err != nil {
				return nil, err
			}
			tags = t
		case map[string]any:
			if raw, ok := v["tags"]; ok {
				list, _ := raw.([]any)
				out := make([]string, 0, len(list))
				for _, t := range list {
					if s, ok := t.(string); ok {
						out = append(out, s)
					}
				}
				tags = out
				break
			}
			contentID, ok := v["id"].(string)
			if !ok || contentID == "" {
				return nil, fmt.Errorf("ifunny-tags: rich input missing tags and cannot fall back (no id field)")
			}
			t, err := fetchTags(contentID)
			if err != nil {
				return nil, err
			}
			tags = t
		default:
			return nil, fmt.Errorf("ifunny-tags: cannot dispatch input of type %T", decoded)
		}

		if len(tags) == 0 {
			return nil, nil // drop
		}
		return config.emitConfig.Encode(tagsEnvelope{Tags: tags})
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
		acceptConfig
		emitConfig
	}{
		acceptConfig: acceptConfig{Accept: "json"},
		emitConfig:   emitConfig{Emit: "json"},
	}
	if err := parse(&config); err != nil {
		return nil, err
	}

	if config.acceptConfig.sparse() && config.emitConfig.sparse() {
		return nil, fmt.Errorf("ifunny-content: accept=string emit=string is a no-op (id → same id)")
	}

	if err := config.acceptConfig.bind(); err != nil {
		return nil, err
	}
	if err := config.emitConfig.bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	spec := enrichSpec{
		name: "ifunny-content",
		targetRef: func(m map[string]any) (string, bool) {
			s, ok := m["id"].(string)
			return s, ok && s != ""
		},
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

	return func(data []byte) ([]byte, error) {
		return runEnrich(data, &config.acceptConfig, &config.emitConfig, spec)
	}, nil
}

// userConfigT configures ifunny-user. By names which user reference the
// transformer keys on — "id" (default, numeric id lookup) or "nick"
// (nickname lookup). The two hit different endpoints and can behave
// differently on edge cases like renames. Named userConfigT to avoid
// colliding with the produce.go userConfig.
type userConfigT struct {
	authConfig
	acceptConfig
	emitConfig
	By string `psy:"by"`
}

// userTransformer builds the ifunny-user transformer. It hydrates a
// User reference into a User entity, keyed by id (`by = "id"`) or nick
// (`by = "nick"`).
//
// Matrix per record (`by = "id"` mode; `by = "nick"` mode fetches to
// resolve the id even on the sparse→sparse cell — a nick and its id
// are not the same reference, so it's genuinely 1 op):
//
//   - by=id   accept=string, emit=string: identity no-op → bind error.
//   - by=id   accept=string, emit=json:   fetch user by id.               1 op
//   - by=id   accept=json,   emit=string: read map["id"], emit.           0 ops
//   - by=id   accept=json,   emit=json:   read map["id"] → fetch user.    1 op
//   - by=nick accept=string, emit=string: fetch to resolve nick → nick.   1 op
//   - by=nick accept=string, emit=json:   fetch by nick.                  1 op
//   - by=nick accept=json,   emit=string: fetch by rich.nick → nick.      1 op
//   - by=nick accept=json,   emit=json:   fetch by rich.nick → user.      1 op
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
//	  by     = "id"
//	  accept = "string"
//	  emit   = "json"
//	}
func userTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := &userConfigT{
		acceptConfig: acceptConfig{Accept: "json"},
		emitConfig:   emitConfig{Emit: "json"},
		By:           "id",
	}
	if err := parse(config); err != nil {
		return nil, err
	}

	byNick, err := parseUserBy(config.By, "ifunny-user")
	if err != nil {
		return nil, err
	}
	if config.acceptConfig.sparse() && config.emitConfig.sparse() {
		return nil, fmt.Errorf("ifunny-user: accept=string emit=string is a no-op (%s → same %s)", config.By, config.By)
	}

	if err := config.acceptConfig.bind(); err != nil {
		return nil, err
	}
	if err := config.emitConfig.bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	key := "id"
	if byNick {
		key = "nick"
	}

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
			s, ok := m[key].(string)
			return s, ok && s != ""
		},
		resolveRef:  func(ref string) (string, error) { return ref, nil },
		fetchTarget: func(ref string) (any, error) { return getUser(ref) },
	}

	return func(data []byte) ([]byte, error) {
		return runEnrich(data, &config.acceptConfig, &config.emitConfig, spec)
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
		acceptConfig
		emitConfig
	}{
		acceptConfig: acceptConfig{Accept: "json"},
		emitConfig:   emitConfig{Emit: "json"},
	}
	if err := parse(&config); err != nil {
		return nil, err
	}

	if config.acceptConfig.sparse() && config.emitConfig.sparse() {
		return nil, fmt.Errorf("ifunny-channel: accept=string emit=string is a no-op (name → same name)")
	}

	if err := config.acceptConfig.bind(); err != nil {
		return nil, err
	}
	if err := config.emitConfig.bind(); err != nil {
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

	spec := enrichSpec{
		name: "ifunny-channel",
		targetRef: func(m map[string]any) (string, bool) {
			s, ok := m["name"].(string)
			return s, ok && s != ""
		},
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

	return func(data []byte) ([]byte, error) {
		return runEnrich(data, &config.acceptConfig, &config.emitConfig, spec)
	}, nil
}
