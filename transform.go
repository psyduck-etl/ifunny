package main

import (
	"encoding/json"
	"fmt"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
	"github.com/psyduck-etl/sdk"
)

// tagsEnvelope is the shape ifunny-tags emits: a Content's tag list
// wrapped in {"tags": [...]}. Sparse emission isn't defined for tags —
// a tag list has no terminal reference — and is rejected at bind time.
type tagsEnvelope struct {
	Tags []string `json:"tags"`
}

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

// enrichPlan is the per-transformer data table bindEnrich composes into
// a per-cell closure. Each primitive covers one leg of the accept×emit
// matrix; bindEnrich picks the composition that matches the cell at
// bind time so the per-record body carries no dispatch overhead.
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
	extract func(data []byte) (targetRef string, target any, err error)
	resolve func(sourceRef string) (targetRef string, target any, err error)
	fetch   func(targetRef string) (any, error)
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
	// closure reuses in place of a redundant plan.fetch call.
	var frontHalf func(data []byte) (string, any, error)
	if accept.sparse() {
		frontHalf = func(data []byte) (string, any, error) {
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
			return plan.resolve(ref)
		}
	} else {
		frontHalf = plan.extract
	}

	if emit.sparse() {
		return func(data []byte) ([]byte, error) {
			ref, _, err := frontHalf(data)
			if err != nil || ref == "" {
				return nil, err
			}
			return emit.Encode(ref)
		}, nil
	}

	return func(data []byte) ([]byte, error) {
		ref, target, err := frontHalf(data)
		if err != nil {
			return nil, err
		}
		if target == nil {
			if ref == "" {
				return nil, nil
			}
			target, err = plan.fetch(ref)
			if err != nil || target == nil {
				return nil, err
			}
		}
		return emit.Encode(target)
	}, nil
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
// Requires auth (shared client surface). Accept / emit default to
// "json"; source defaults to "content" for back-compat.
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
		Source:       "content",
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

	// getUser hydrates the target User by id or nick, applying the
	// not-found-drops convention.
	getUser := func(req compose.Request) (*ifunny.User, error) {
		u, err := client.GetUser(req)
		if err != nil {
			if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
				return nil, nil
			}
			return nil, err
		}
		return u, nil
	}

	// recoverNick recovers an authoritative nick from an author id
	// when the source shape had the id but no nick. It returns the
	// hydrated User so the rich-out path can short-circuit fetch.
	recoverNick := func(id string) (string, *ifunny.User, error) {
		u, err := getUser(compose.UserByID(id))
		if err != nil || u == nil {
			return "", nil, err
		}
		return u.Nick, u, nil
	}

	// pickRef reads the emit-by axis field off any Authored source.
	// Under by=nick with an empty nick, it recoveries via GetUser and
	// returns the hydrated user for short-circuit; if the id is also
	// empty, the record drops.
	pickRef := func(a ifunny.Authored) (string, any, error) {
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
		return recoverNick(id)
	}

	plan := enrichPlan{
		name: "ifunny-author",
		fetch: func(ref string) (any, error) {
			if byNick {
				return getUser(compose.UserByNick(ref))
			}
			return getUser(compose.UserByID(ref))
		},
	}

	switch config.Source {
	case "content":
		plan.extract = func(data []byte) (string, any, error) {
			var s authoredContent
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return pickRef(&s)
		}
		plan.resolve = func(contentID string) (string, any, error) {
			content, err := client.GetContent(contentID)
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
			nick, u, err := recoverNick(content.Creator.ID)
			return nick, u, err
		}
	case "comment":
		plan.extract = func(data []byte) (string, any, error) {
			var s authoredComment
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return pickRef(&s)
		}
	case "chat":
		plan.extract = func(data []byte) (string, any, error) {
			var s authoredChatEvent
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return pickRef(&s)
		}
	default:
		return nil, fmt.Errorf("ifunny-author: unrecognized source %q; want \"content\", \"comment\", or \"chat\"", config.Source)
	}

	return bindEnrich(&config.acceptConfig, &config.emitConfig, plan)
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

	fetchContent := func(id string) (any, error) {
		content, err := client.GetContent(id)
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
		extract: func(data []byte) (string, any, error) {
			var s struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return s.ID, nil, nil
		},
		resolve: func(ref string) (string, any, error) { return ref, nil, nil },
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

	getUser := func(req compose.Request) (*ifunny.User, error) {
		u, err := client.GetUser(req)
		if err != nil {
			if apiErr, ok := ifunny.AsAPIError(err); ok && apiErr.Kind == "not_found" {
				return nil, nil
			}
			return nil, err
		}
		return u, nil
	}

	var extract func(data []byte) (string, any, error)
	if byNick {
		extract = func(data []byte) (string, any, error) {
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
			u, err := getUser(compose.UserByID(s.ID))
			if err != nil || u == nil {
				return "", nil, err
			}
			return u.Nick, u, nil
		}
	} else {
		extract = func(data []byte) (string, any, error) {
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
		resolve: func(ref string) (string, any, error) { return ref, nil, nil },
		fetch: func(ref string) (any, error) {
			if byNick {
				return getUser(compose.UserByNick(ref))
			}
			return getUser(compose.UserByID(ref))
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

	chat, err := client.Chat()
	if err != nil {
		return nil, err
	}

	fetchChannel := func(name string) (any, error) {
		channel, err := chat.GetChannel(compose.GetChannel(name))
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
		extract: func(data []byte) (string, any, error) {
			var s struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return s.Name, nil, nil
		},
		resolve: func(ref string) (string, any, error) { return ref, nil, nil },
		fetch:   fetchChannel,
	}

	return bindEnrich(&config.acceptConfig, &config.emitConfig, plan)
}
