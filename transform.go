package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

// authoredContent, authoredComment, and authoredChatEvent are shallow
// shadows of the three source shapes ifunny-author accepts. Each keeps
// only the fields needed to satisfy the ifunny.Authored contract, so
// json.Unmarshal doesn't have to parse whole entity bodies just to
// pluck an author reference off the top.
//
// ifunny.Authored (AuthorID, AuthorNick) is the shared contract; the
// compile-time assertions below pin the shadows to it, so a library
// bump that widens the interface fails the build here instead of
// silently degrading extraction. Under emit-by = "nick" the extract
// closures still recover an authoritative nick via GetUser when a
// source omits it — see authorTransformer for the recovery path.

type authoredContent struct {
	Creator struct {
		ID   string `json:"id"`
		Nick string `json:"nick"`
	} `json:"creator"`
}

func (c *authoredContent) AuthorID() string   { return c.Creator.ID }
func (c *authoredContent) AuthorNick() string { return c.Creator.Nick }

type authoredComment struct {
	User struct {
		ID   string `json:"id"`
		Nick string `json:"nick"`
	} `json:"user"`
}

func (c *authoredComment) AuthorID() string   { return c.User.ID }
func (c *authoredComment) AuthorNick() string { return c.User.Nick }

type authoredChatEvent struct {
	User struct {
		ID   string `json:"user"` // ChatEvent nests the author id under json:"user"
		Nick string `json:"nick"`
	} `json:"user"`
}

func (c *authoredChatEvent) AuthorID() string   { return c.User.ID }
func (c *authoredChatEvent) AuthorNick() string { return c.User.Nick }

var (
	_ ifunny.Authored = (*authoredContent)(nil)
	_ ifunny.Authored = (*authoredComment)(nil)
	_ ifunny.Authored = (*authoredChatEvent)(nil)
)

// parseUserBy validates a "by" or "emit-by" string against the two
// user reference axes. Returns true iff the caller should key on nick.
// Callers wrap the returned error with their resource name so
// ifunny-user vs ifunny-author gets a self-locating message.
func parseUserBy(v string) (byNick bool, err error) {
	switch v {
	case "id":
		return false, nil
	case "nick":
		return true, nil
	default:
		return false, fmt.Errorf("unrecognized user reference axis %q; want \"id\" or \"nick\"", v)
	}
}

// enrichPlan is the per-transformer data table bindEnrich composes into
// a per-cell closure. Each primitive covers one leg of the accept×emit
// matrix; bindEnrich picks the composition that matches the cell at
// bind time so the per-record body carries no dispatch overhead.
//
// Every primitive takes the stage ctx as its first argument so the
// per-record API calls it makes are bound by the transformer's context:
// cancelling the stage aborts an in-flight fetch rather than only firing
// the emit guard after it returns.
//
// extract pulls T's terminal ref (and optionally a hydrated T from a
// recovery fetch — see the by-nick paths) out of rich input bytes.
// Called only on the !accept.sparse() cells. Direct json.Unmarshal
// against a shadow struct is deliberate; see bindEnrich for why the
// codec bypass is safe.
//
// resolve maps a bare source ref to T's terminal ref, again optionally
// short-circuiting with a hydrated T. Called only on the accept.sparse()
// cells. A nil resolve marks a cell whose source is unfetchable from
// the client surface (ifunny-author with source = comment | chat: there
// is no single-comment or single-message endpoint); bindEnrich returns
// a bind error in that case rather than silently degrading.
//
// fetch hydrates T by its terminal ref. Called only on the !emit.sparse()
// cells, and only when neither extract nor resolve short-circuited with
// a hydrated target. (nil, nil) means not-found → drop the record.
type enrichPlan struct {
	name    string
	extract func(ctx context.Context, data []byte) (targetRef string, target any, err error)
	resolve func(ctx context.Context, sourceRef string) (targetRef string, target any, err error)
	fetch   func(ctx context.Context, targetRef string) (any, error)
}

// bindEnrich composes the correct closure for the accept×emit cell
// defined by (accept, emit). Rather than a per-record type switch, it
// picks a front half (extract vs resolve) at bind and a back half
// (emit ref vs hydrate + emit) at bind, then closes over the two.
//
// The direct json.Unmarshal inside extract closures bypasses the accept
// codec's Decode path. That is safe here because bindEnrich only routes
// records to extract when accept.sparse() is false, and acceptConfig.bind
// pinned the codec via exact-match sdk.GetCodec — a non-"string" spec
// today resolves to the literal "json" codec (host-registered or the
// test stub). The invariant "accept != string ⇒ record bytes are JSON"
// is therefore a bind-time property, not a per-record assumption; a
// future non-JSON rich codec would need per-transformer opt-in rather
// than silent participation. The read-only bypass is one-sided: the
// emit path always encodes through emit.Encode so downstream stages
// keep talking to the codec registry.
func bindEnrich(accept *acceptConfig, emit *emitConfig, plan enrichPlan) (sdk.Transformer, error) {
	if accept.sparse() && plan.resolve == nil {
		return nil, fmt.Errorf("%s: accept=string not satisfiable for this source (no fetch-by-ref endpoint)", plan.name)
	}

	// frontHalf yields T's terminal ref for one record, plus an
	// optional pre-hydrated T from a recovery fetch that the rich-out
	// closure reuses in place of a redundant plan.fetch call. It carries
	// the stage ctx so its fetches abort on cancellation.
	var frontHalf func(ctx context.Context, data []byte) (string, any, error)
	if accept.sparse() {
		frontHalf = func(ctx context.Context, data []byte) (string, any, error) {
			decoded, err := accept.Decode(data)
			if err != nil {
				return "", nil, err
			}
			ref, ok := decoded.(string)
			if !ok {
				return "", nil, fmt.Errorf("%s: expected string ref from sparse accept, got %T", plan.name, decoded)
			}
			if ref == "" {
				return "", nil, nil
			}
			return plan.resolve(ctx, ref)
		}
	} else {
		frontHalf = plan.extract
	}

	if emit.sparse() {
		return sdk.MapContext(func(ctx context.Context, data []byte) ([]byte, error) {
			ref, _, err := frontHalf(ctx, data)
			if err != nil || ref == "" {
				return nil, err
			}
			return emit.Encode(ref)
		}), nil
	}

	return sdk.MapContext(func(ctx context.Context, data []byte) ([]byte, error) {
		ref, target, err := frontHalf(ctx, data)
		if err != nil {
			return nil, err
		}
		if target == nil {
			if ref == "" {
				return nil, nil
			}
			target, err = plan.fetch(ctx, ref)
			if err != nil || target == nil {
				return nil, err
			}
		}
		return emit.Encode(target)
	}), nil
}

// authorTransformer builds the ifunny-author transformer. It maps a
// Content, Comment, or ChatEvent (chosen at bind via the `source`
// field) to its author's User. The emit-by field ("id" default, or
// "nick") names the user reference axis throughout — it decides which
// field on the source is the target ref, which endpoint hydrates the
// user, and what sparse-out emits.
//
// Sources: source = "content" (default), "comment", or "chat". Sources
// other than "content" have no single-item fetch endpoint on the client
// surface, so accept = "string" is rejected at bind time for them.
//
// Matrix per record (S = source, T = User):
//
//   - accept=string source=content:  fetch S (Content), read creator.<axis>.
//     emit=string   → 1 op (2 ops on by=nick recovery when creator.nick missing).
//     emit=json     → 2 ops (fetch S, fetch T; recovery may short-circuit).
//   - accept=string source=comment|chat: bind error.
//   - accept=json:  shadow-unmarshal, read <axis> off the shadow.
//     emit=string   → 0 ops (1 op on by=nick recovery when nick missing).
//     emit=json     → 1 op (fetch T fresh; by=nick recovery short-circuits).
//
// A record whose author cannot be resolved (system-authored, or the
// axis field ends up empty) drops from the pipeline.
//
// Requires auth (shared client surface). source is required; accept
// and emit default to "json".
//
// Example (glue step between posts and their commenters' timelines):
//
//	transform "ifunny-author" "author" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  source  = "content"   # or "comment", "chat"
//	  emit-by = "id"        # or "nick"
//	  accept  = "json"
//	  emit    = "string"
//	}
func authorTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := struct {
		authConfig
		acceptConfig
		emitConfig
		Source string `psy:"source"`
		EmitBy string `psy:"emit-by"`
	}{
		acceptConfig: acceptConfig{Accept: "json"},
		emitConfig:   emitConfig{Emit: "json"},
		EmitBy:       "id",
	}
	if err := parse(&config); err != nil {
		return nil, err
	}

	byNick, err := parseUserBy(config.EmitBy)
	if err != nil {
		return nil, fmt.Errorf("ifunny-author: %w", err)
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

	// getUser hydrates the target User by id or nick, applying the
	// not-found-drops convention. ctx is the stage context so a cancelled
	// stage aborts the in-flight lookup.
	getUser := func(ctx context.Context, req compose.Request) (*ifunny.User, error) {
		u, err := client.GetUser(ctx, req)
		if err != nil {
			if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
				return nil, nil
			}
			return nil, err
		}
		return u, nil
	}

	// recoverUser hydrates a User by id when the source had the id but
	// no nick. It returns the hydrated User so the rich-out path can
	// short-circuit fetch; callers read u.Nick themselves.
	recoverUser := func(ctx context.Context, id string) (*ifunny.User, error) {
		return getUser(ctx, compose.UserByID(id))
	}

	// pickRef reads the emit-by axis field off any Authored source.
	// Under by=nick with an empty nick, it recovers via GetUser and
	// returns the hydrated user for short-circuit; if the id is also
	// empty, the record drops.
	pickRef := func(ctx context.Context, a ifunny.Authored) (string, any, error) {
		if !byNick {
			id := a.AuthorID()
			return id, nil, nil
		}
		if nick := a.AuthorNick(); nick != "" {
			return nick, nil, nil
		}
		id := a.AuthorID()
		if id == "" {
			return "", nil, nil
		}
		u, err := recoverUser(ctx, id)
		if err != nil || u == nil {
			return "", nil, err
		}
		return u.Nick, u, nil
	}

	plan := enrichPlan{
		name: "ifunny-author",
		fetch: func(ctx context.Context, ref string) (any, error) {
			if byNick {
				return getUser(ctx, compose.UserByNick(ref))
			}
			return getUser(ctx, compose.UserByID(ref))
		},
	}

	switch config.Source {
	case "content":
		plan.extract = func(ctx context.Context, data []byte) (string, any, error) {
			var s authoredContent
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return pickRef(ctx, &s)
		}
		plan.resolve = func(ctx context.Context, contentID string) (string, any, error) {
			content, err := client.GetContent(ctx, contentID)
			if err != nil {
				return "", nil, err
			}
			if content == nil || content.Creator.ID == "" {
				return "", nil, nil
			}
			if !byNick {
				return content.Creator.ID, nil, nil
			}
			if content.Creator.Nick != "" {
				return content.Creator.Nick, nil, nil
			}
			u, err := recoverUser(ctx, content.Creator.ID)
			if err != nil || u == nil {
				return "", nil, err
			}
			return u.Nick, u, nil
		}
	case "comment":
		plan.extract = func(ctx context.Context, data []byte) (string, any, error) {
			var s authoredComment
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return pickRef(ctx, &s)
		}
	case "chat":
		plan.extract = func(ctx context.Context, data []byte) (string, any, error) {
			var s authoredChatEvent
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return pickRef(ctx, &s)
		}
	default:
		return nil, fmt.Errorf("ifunny-author: source is required (one of \"content\", \"comment\", \"chat\"); got %q", config.Source)
	}

	return bindEnrich(&config.acceptConfig, &config.emitConfig, plan)
}

// tagsTransformer builds the ifunny-tags transformer. It lifts a
// Content's tag list, emitting each tag as its own message (1→N shape).
// Each emitted record is the raw UTF-8 bytes of the tag string — e.g.
// []byte("cats") — with no codec wrapping. A tag has no structured
// shape variation, so there is no emit config.
//
// Matrix per record:
//
//   - accept=string: fetch content by id → read .tags.               1 op
//   - accept=json:   read map["tags"]; on miss fall back to fetching
//     by the map's "id" field.                              0 or 1 op
//
// A post with no tags is dropped (an empty tag set contributes nothing
// to a census). Requires auth (fetches are possible on either accept).
//
// Prior implementation emitted {"tags": [...]} as a single record, requiring
// downstream explode. Current implementation explodes internally, emitting
// each tag string as its own record for direct composability.
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
//	}
func tagsTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := struct {
		authConfig
		acceptConfig
	}{
		acceptConfig: acceptConfig{Accept: "json"},
	}
	if err := parse(&config); err != nil {
		return nil, err
	}

	if err := config.acceptConfig.bind(); err != nil {
		return nil, err
	}

	client, err := clientFor(&config.authConfig)
	if err != nil {
		return nil, err
	}

	fetchTags := func(ctx context.Context, contentID string) ([]string, error) {
		content, err := client.GetContent(ctx, contentID)
		if err != nil {
			return nil, err
		}
		if content == nil {
			return nil, nil
		}
		return content.Tags, nil
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)

		for msg := range in {
			decoded, err := config.acceptConfig.Decode(msg)
			if err != nil {
				if !sendErr(ctx, errs, err) {
					return
				}
				continue
			}

			var tags []string
			switch v := decoded.(type) {
			case string:
				t, err := fetchTags(ctx, v)
				if err != nil {
					if !sendErr(ctx, errs, err) {
						return
					}
					continue
				}
				tags = t
			case map[string]any:
				if raw, ok := v["tags"]; ok {
					list, ok := raw.([]any)
					if !ok {
						if !sendErr(ctx, errs, fmt.Errorf("ifunny-tags: content %v has non-array tags field of type %T", v["id"], raw)) {
							return
						}
						continue
					}
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
					if !sendErr(ctx, errs, fmt.Errorf("ifunny-tags: rich input missing tags and cannot fall back (no id field)")) {
						return
					}
					continue
				}
				t, err := fetchTags(ctx, contentID)
				if err != nil {
					if !sendErr(ctx, errs, err) {
						return
					}
					continue
				}
				tags = t
			default:
				if !sendErr(ctx, errs, fmt.Errorf("ifunny-tags: cannot dispatch input of type %T", decoded)) {
					return
				}
				continue
			}

			// Emit each tag as its own record: raw UTF-8 bytes of the tag
			// string, no codec wrapping.
			for _, tag := range tags {
				select {
				case out <- []byte(tag):
				case <-ctx.Done():
					return
				}
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
//   - accept=json,   emit=string: read shadow.id, emit.             0 ops
//   - accept=json,   emit=json:   read shadow.id → fetch content.   1 op
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

	fetchContent := func(ctx context.Context, id string) (any, error) {
		content, err := client.GetContent(ctx, id)
		if err != nil {
			if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
				return nil, nil
			}
			return nil, err
		}
		return content, nil
	}

	plan := enrichPlan{
		name: "ifunny-content",
		extract: func(_ context.Context, data []byte) (string, any, error) {
			var s struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return s.ID, nil, nil
		},
		resolve: func(_ context.Context, ref string) (string, any, error) { return ref, nil, nil },
		fetch:   fetchContent,
	}

	return bindEnrich(&config.acceptConfig, &config.emitConfig, plan)
}

// userConfigT configures ifunny-user. By names which user reference the
// transformer keys on — "id" (default, numeric id lookup) or "nick"
// (nickname lookup). The two hit different endpoints and can behave
// differently on edge cases like renames. Named userConfigT to avoid
// colliding with the producer-user.go userConfig.
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
// Matrix per record:
//
//   - by=id   accept=string, emit=string: identity no-op → bind error.
//   - by=id   accept=string, emit=json:   fetch user by id.               1 op
//   - by=id   accept=json,   emit=string: read shadow.id.                 0 ops
//   - by=id   accept=json,   emit=json:   read shadow.id → fetch user.    1 op
//   - by=nick accept=string, emit=string: identity no-op → bind error.
//   - by=nick accept=string, emit=json:   fetch user by nick.             1 op
//   - by=nick accept=json,   emit=string: read shadow.nick.        0 ops
//     (recovery: nick empty but id present → fetch user by id.)          1 op
//   - by=nick accept=json,   emit=json:   read shadow.nick, fetch user.   1 op
//     (recovery short-circuits: the by-id fetch is the emit target.)     1 op
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

	byNick, err := parseUserBy(config.By)
	if err != nil {
		return nil, fmt.Errorf("ifunny-user: %w", err)
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

	getUser := func(ctx context.Context, req compose.Request) (*ifunny.User, error) {
		u, err := client.GetUser(ctx, req)
		if err != nil {
			if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
				return nil, nil
			}
			return nil, err
		}
		return u, nil
	}

	var extract func(ctx context.Context, data []byte) (string, any, error)
	if byNick {
		extract = func(ctx context.Context, data []byte) (string, any, error) {
			var s struct {
				ID   string `json:"id"`
				Nick string `json:"nick"`
			}
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			if s.Nick != "" {
				return s.Nick, nil, nil
			}
			if s.ID == "" {
				return "", nil, nil
			}
			u, err := getUser(ctx, compose.UserByID(s.ID))
			if err != nil || u == nil {
				return "", nil, err
			}
			return u.Nick, u, nil
		}
	} else {
		extract = func(_ context.Context, data []byte) (string, any, error) {
			var s struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return s.ID, nil, nil
		}
	}

	plan := enrichPlan{
		name:    "ifunny-user",
		extract: extract,
		resolve: func(_ context.Context, ref string) (string, any, error) { return ref, nil, nil },
		fetch: func(ctx context.Context, ref string) (any, error) {
			if byNick {
				return getUser(ctx, compose.UserByNick(ref))
			}
			return getUser(ctx, compose.UserByID(ref))
		},
	}

	return bindEnrich(&config.acceptConfig, &config.emitConfig, plan)
}

// channelTransformer builds the ifunny-channel transformer. It hydrates
// a ChatChannel reference into a ChatChannel entity, keyed by name.
//
// Matrix per record (identical shape to ifunny-content but keyed by
// "name" instead of "id"):
//
//   - accept=string, emit=string: identity no-op → bind error.
//   - accept=string, emit=json:   fetch channel by name.             1 op
//   - accept=json,   emit=string: read shadow.name.                  0 ops
//   - accept=json,   emit=json:   read shadow.name → fetch channel.  1 op
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

	chat, err := client.Chat(context.Background())
	if err != nil {
		return nil, err
	}

	fetchChannel := func(ctx context.Context, name string) (any, error) {
		channel, err := chat.GetChannel(ctx, compose.GetChannel(name))
		if err != nil {
			if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
				return nil, nil
			}
			return nil, err
		}
		return channel, nil
	}

	plan := enrichPlan{
		name: "ifunny-channel",
		extract: func(_ context.Context, data []byte) (string, any, error) {
			var s struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return s.Name, nil, nil
		},
		resolve: func(_ context.Context, ref string) (string, any, error) { return ref, nil, nil },
		fetch:   fetchChannel,
	}

	return bindEnrich(&config.acceptConfig, &config.emitConfig, plan)
}

// ============================================================================
// EXPLODE TRANSFORMERS (1→N)
// ============================================================================

// timelineTransformer builds the ifunny-timeline transformer. It explodes
// a user identifier into their timeline / posts feed, emitting each content
// item as its own message. The shape is user-reference → []content.
//
// Keying: Exactly one of accept=string (bare user id/nick) or accept=json
// (rich user object with an id or nick field) is supported. The emit axis
// ("id" default, or "nick") selects the reference axis for both the by-id
// and by-nick lookup paths, but for content emission the transformer always
// fetches full Content entities (emit=string emits IDs, emit=json emits rich
// objects per the output codec).
//
// The optional limit field (0 = no limit) stops emission after the Nth item.
// Requires auth (shared client surface).
//
// Example:
//
//	transform "ifunny-timeline" "expand" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  accept = "string"
//	  emit   = "string"
//	  limit  = 100
//	}
func timelineTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := struct {
		authConfig
		acceptConfig
		emitConfig
		By    string `psy:"by"`
		Limit int    `psy:"limit"`
	}{
		acceptConfig: acceptConfig{Accept: "json"},
		emitConfig:   emitConfig{Emit: "json"},
		By:           "id",
	}
	if err := parse(&config); err != nil {
		return nil, err
	}

	byNick, err := parseUserBy(config.By)
	if err != nil {
		return nil, fmt.Errorf("ifunny-timeline: %w", err)
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

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)

		for msg := range in {
			decoded, err := config.acceptConfig.Decode(msg)
			if err != nil {
				if !sendErr(ctx, errs, err) {
					return
				}
				continue
			}

			var userRef string
			if config.acceptConfig.sparse() {
				ref, ok := decoded.(string)
				if !ok {
					if !sendErr(ctx, errs, fmt.Errorf("ifunny-timeline: expected string ref, got %T", decoded)) {
						return
					}
					continue
				}
				userRef = ref
			} else {
				// Rich input: extract user reference from map
				richIn, ok := decoded.(map[string]any)
				if !ok {
					if !sendErr(ctx, errs, fmt.Errorf("ifunny-timeline: expected map, got %T", decoded)) {
						return
					}
					continue
				}
				if byNick {
					if nick, ok := richIn["nick"].(string); ok && nick != "" {
						userRef = nick
					} else if id, ok := richIn["id"].(string); ok && id != "" {
						userRef = id
					}
				} else {
					if id, ok := richIn["id"].(string); ok && id != "" {
						userRef = id
					}
				}
			}

			if userRef == "" {
				continue
			}

			// Iterate the timeline
			var iter <-chan ifunny.Result[*ifunny.Content]
			if byNick {
				iter = client.IterTimelineByNick(ctx, userRef)
			} else {
				iter = client.IterTimeline(ctx, userRef)
			}

			count := 0
		emit:
			for r := range iter {
				if r.Err != nil {
					if !sendErr(ctx, errs, r.Err) {
						return
					}
					break
				}
				if r.V == nil {
					break
				}

				// Encode and emit
				var toEmit any
				if config.emitConfig.sparse() {
					toEmit = r.V.ID
				} else {
					toEmit = r.V
				}

				b, err := config.emitConfig.Encode(toEmit)
				if err != nil {
					if !sendErr(ctx, errs, err) {
						return
					}
					break
				}

				select {
				case out <- b:
					count++
					if config.Limit > 0 && count >= config.Limit {
						break emit
					}
				case <-ctx.Done():
					return
				}
			}
		}
	}, nil
}

// commentsTransformer builds the ifunny-comments transformer. It explodes
// a content identifier into the comment forest on that content, emitting
// each comment (both top-level and replies) as its own message. The shape
// is content-reference → []comment. Comment forest is walked depth-first:
// for each top-level comment, emit it and then drain its replies before
// advancing to the next top-level.
//
// The optional max-depth field caps reply depth: 0 = top-level only,
// ≥1 = include replies, -1 = unlimited (default). iFunny replies don't
// nest beyond one level, so ≥1 and -1 emit the same set. This transformer
// supports accept=string (bare content id) or accept=json (rich object
// with id field). Always emits Comment entities encoded via emit codec.
// Requires auth.
//
// Example:
//
//	transform "ifunny-comments" "explode" {
//	  auth-basic = env.IFUNNY_BASIC
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  accept = "string"
//	  emit   = "json"
//	}
func commentsTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := struct {
		authConfig
		acceptConfig
		emitConfig
		MaxDepth int `psy:"max-depth"`
	}{
		acceptConfig: acceptConfig{Accept: "json"},
		emitConfig:   emitConfig{Emit: "json"},
	}
	if err := parse(&config); err != nil {
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

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)

		emitComment := func(c *ifunny.Comment) bool {
			b, err := config.emitConfig.Encode(c)
			if err != nil {
				sendErr(ctx, errs, err)
				return false
			}
			select {
			case out <- b:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for msg := range in {
			decoded, err := config.acceptConfig.Decode(msg)
			if err != nil {
				if !sendErr(ctx, errs, err) {
					return
				}
				continue
			}

			var contentID string
			if config.acceptConfig.sparse() {
				ref, ok := decoded.(string)
				if !ok {
					if !sendErr(ctx, errs, fmt.Errorf("ifunny-comments: expected string ref, got %T", decoded)) {
						return
					}
					continue
				}
				contentID = ref
			} else {
				richIn, ok := decoded.(map[string]any)
				if !ok {
					if !sendErr(ctx, errs, fmt.Errorf("ifunny-comments: expected map, got %T", decoded)) {
						return
					}
					continue
				}
				if id, ok := richIn["id"].(string); ok {
					contentID = id
				}
			}

			if contentID == "" {
				continue
			}

			// Walk the comment forest depth-first
			for r := range client.IterComments(ctx, contentID) {
				if r.Err != nil {
					if !sendErr(ctx, errs, r.Err) {
						return
					}
					break
				}
				if r.V == nil {
					break
				}

				comment := r.V
				if !emitComment(comment) {
					return
				}

				// max-depth: 0 = top-level only; -1 or ≥1 = include replies.
				if config.MaxDepth != 0 {
					if comment.Num.Replies > 0 {
						for rr := range client.IterReplies(ctx, contentID, comment.ID) {
							if rr.Err != nil {
								if !sendErr(ctx, errs, rr.Err) {
									return
								}
								break
							}
							if rr.V == nil {
								break
							}
							if !emitComment(rr.V) {
								return
							}
						}
					}
				}
			}
		}
	}, nil
}

// interactionPlan describes the resolved set of enabled interactions for
// the ifunny-interactions transformer, including which user iterators to fan out.
type interactionPlan struct {
	author      bool
	smiles      bool
	republishes bool
	comments    bool
}

// parseInteractions parses and validates the interactions list, returning
// an interactionPlan. Empty list errors at parse time.
func parseInteractions(list []string) (*interactionPlan, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("ifunny-interactions: interactions list must not be empty")
	}
	valid := map[string]bool{"author": true, "smiles": true, "republishes": true, "comments": true}
	plan := &interactionPlan{}
	for _, v := range list {
		if !valid[v] {
			return nil, fmt.Errorf(`ifunny-interactions: unknown interaction %q; want one of: "author", "smiles", "republishes", "comments"`, v)
		}
		switch v {
		case "author":
			plan.author = true
		case "smiles":
			plan.smiles = true
		case "republishes":
			plan.republishes = true
		case "comments":
			plan.comments = true
		}
	}
	return plan, nil
}

// interactionsTransformer builds the ifunny-interactions transformer. It
// explodes a content identifier into users from selected interactions on
// that content, emitting each user as its own message. The shape is
// content-reference → []user. The interactions list (author, smiles,
// republishes, comments) defines which user iterators to fan out in parallel.
//
// - author: the author of the content itself (single user).
// - smiles: iterate the users who smiled the content.
// - republishes: iterate the users who republished the content.
// - comments: iterate the authors of comments on the content.
//
// Goroutines fan out per-interaction, all writing to a shared out channel;
// ordering is unspecified. The transformer respects ctx cancellation on all
// sub-goroutines and ensures clean exit before returning.
//
// The author and comments sub-iterators emit a shallow user (ID + Nick
// only), not a hydrated profile — chain ifunny-user downstream if you
// need the full user object. smiles and republishes emit whatever the
// underlying iFunny iterator returns.
//
// Example:
//
//	transform "ifunny-interactions" "expand" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  accept = "json"
//	  emit   = "json"
//	  interactions = ["author", "smiles", "republishes"]
//	}
func interactionsTransformer(parse sdk.Parser) (sdk.Transformer, error) {
	config := struct {
		authConfig
		acceptConfig
		emitConfig
		Interactions []string `psy:"interactions"`
	}{
		acceptConfig: acceptConfig{Accept: "json"},
		emitConfig:   emitConfig{Emit: "json"},
	}
	if err := parse(&config); err != nil {
		return nil, err
	}

	plan, err := parseInteractions(config.Interactions)
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

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		var wg sync.WaitGroup

		// emitUser encodes and sends a single user to out. Returns false on
		// error or cancellation, matching the producer pattern.
		emitUser := func(u *ifunny.User) bool {
			var toEmit any
			if config.emitConfig.sparse() {
				toEmit = u.ID
			} else {
				toEmit = u
			}
			b, err := config.emitConfig.Encode(toEmit)
			if err != nil {
				sendErr(ctx, errs, err)
				return false
			}
			select {
			case out <- b:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for msg := range in {
			decoded, err := config.acceptConfig.Decode(msg)
			if err != nil {
				if !sendErr(ctx, errs, err) {
					return
				}
				continue
			}

			var contentID string
			if config.acceptConfig.sparse() {
				ref, ok := decoded.(string)
				if !ok {
					if !sendErr(ctx, errs, fmt.Errorf("ifunny-interactions: expected string ref, got %T", decoded)) {
						return
					}
					continue
				}
				contentID = ref
			} else {
				richIn, ok := decoded.(map[string]any)
				if !ok {
					if !sendErr(ctx, errs, fmt.Errorf("ifunny-interactions: expected map, got %T", decoded)) {
						return
					}
					continue
				}
				if id, ok := richIn["id"].(string); ok {
					contentID = id
				}
			}

			if contentID == "" {
				continue
			}

			// Serialize per-input: block until the previous record's fan-out
			// finishes before spawning goroutines for this one. Bounds the
			// live goroutine count at (number of enabled interactions) and
			// keeps output naturally grouped by input, at the cost of no
			// pipelining across inputs.
			wg.Wait()

			// Fan out one goroutine per enabled interaction
			if plan.author {
				wg.Add(1)
				go func() {
					defer wg.Done()
					content, err := client.GetContent(ctx, contentID)
					if err != nil {
						// Return-value discarded: this is the last statement
						// in the goroutine, so nothing to do after either way.
						_ = sendErr(ctx, errs, err)
						return
					}
					if content != nil && content.Creator.ID != "" {
						emitUser(&ifunny.User{ID: content.Creator.ID, Nick: content.Creator.Nick})
					}
				}()
			}

			if plan.smiles {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for r := range client.IterSmiles(ctx, contentID) {
						if r.Err != nil {
							if !sendErr(ctx, errs, r.Err) {
								return
							}
							break
						}
						if r.V == nil {
							break
						}
						if !emitUser(r.V) {
							return
						}
					}
				}()
			}

			if plan.republishes {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for r := range client.IterRepublishers(ctx, contentID) {
						if r.Err != nil {
							if !sendErr(ctx, errs, r.Err) {
								return
							}
							break
						}
						if r.V == nil {
							break
						}
						if !emitUser(r.V) {
							return
						}
					}
				}()
			}

			if plan.comments {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for r := range client.IterComments(ctx, contentID) {
						if r.Err != nil {
							if !sendErr(ctx, errs, r.Err) {
								return
							}
							break
						}
						if r.V == nil {
							break
						}
						// Emit the comment author
						if r.V.User.ID != "" {
							if !emitUser(&ifunny.User{ID: r.V.User.ID, Nick: r.V.User.Nick}) {
								return
							}
						}
						// Walk replies. A reply-iterator error is reported per-record
						// and breaks *this* replies loop only — the outer comments
						// iteration continues to the next top-level comment.
						if r.V.Num.Replies > 0 {
							for rr := range client.IterReplies(ctx, contentID, r.V.ID) {
								if rr.Err != nil {
									if !sendErr(ctx, errs, rr.Err) {
										return
									}
									break
								}
								if rr.V == nil {
									break
								}
								if rr.V.User.ID != "" {
									if !emitUser(&ifunny.User{ID: rr.V.User.ID, Nick: rr.V.User.Nick}) {
										return
									}
								}
							}
						}
					}
				}()
			}
		}

		wg.Wait()
	}, nil
}
