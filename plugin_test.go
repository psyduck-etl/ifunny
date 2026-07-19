package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/psyduck-etl/sdk"
)

// TestMain registers tiny codec factories so ifunny tests run without a
// host binary. In production the psyduck host registers the real stdlib
// codec chain; here we need:
//
//   - "json": end-to-end for the codec-dependent transformers/producers.
//   - "string": the terminal-reference codec — its Decode returns a Go
//     string of the input bytes, its Encode expects a string (anything
//     else errors). bindEnrich dispatches on this shape.
//
// Anything else returns an error.
func TestMain(m *testing.M) {
	sdk.RegisterCodecs(func(spec string) (sdk.Codec, error) {
		switch spec {
		case "json":
			return jsonCodec{}, nil
		case "string":
			return stringCodec{}, nil
		}
		return nil, fmt.Errorf("test codec factory: unknown spec %q", spec)
	})
	os.Exit(m.Run())
}

type jsonCodec struct{}

func (jsonCodec) Decode(b []byte) (any, error) {
	var v any
	err := json.Unmarshal(b, &v)
	return v, err
}
func (jsonCodec) Encode(v any) ([]byte, error) { return json.Marshal(v) }

type stringCodec struct{}

func (stringCodec) Decode(b []byte) (any, error) { return string(b), nil }
func (stringCodec) Encode(v any) ([]byte, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("string codec: cannot encode %T", v)
	}
	return []byte(s), nil
}

// testAccept / testEmit build bound codec-config halves for driving
// bindEnrich directly. They panic on an unknown spec — a test-authoring
// bug, not a runtime condition.
func testAccept(spec string) *acceptConfig {
	c := &acceptConfig{Accept: spec}
	if err := c.bind(); err != nil {
		panic(err)
	}
	return c
}

func testEmit(spec string) *emitConfig {
	c := &emitConfig{Emit: spec}
	if err := c.bind(); err != nil {
		panic(err)
	}
	return c
}

// expectedResource describes what the assembly must advertise for a
// resource: its kind and the specs it must expose.
type expectedResource struct {
	kind  sdk.Kind
	specs []string
}

var expectedResources = map[string]expectedResource{
	"ifunny-feed":             {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "feed", "emit"}},
	"ifunny-timeline":         {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "by-id", "by-nick", "emit"}},
	"ifunny-explore":          {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "compilation", "kind", "emit"}},
	"ifunny-comments":         {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "emit"}},
	"ifunny-smiles":           {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "emit"}},
	"ifunny-republishers":     {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "emit"}},
	"ifunny-subscribers":      {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "user", "emit"}},
	"ifunny-subscriptions":    {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "user", "emit"}},
	"ifunny-channels":         {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "query", "emit"}},
	"ifunny-chat-history":     {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "channel", "emit"}},
	"ifunny-chat-listen":      {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "channel", "emit"}},
	"ifunny-chat-invites":     {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "emit"}},
	"ifunny-author":           {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "source", "emit-by", "accept", "emit"}},
	"ifunny-tags":             {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "accept"}},
	"ifunny-content":          {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "accept", "emit"}},
	"ifunny-user":             {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "by", "accept", "emit"}},
	"ifunny-channel":          {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "accept", "emit"}},
	"ifunny-timeline-explode": {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "by", "limit", "accept", "emit"}},
	"ifunny-comments-explode": {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "max-depth", "accept", "emit"}},
	"ifunny-interactions":     {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "interactions", "accept", "emit"}},
}

func TestPluginAssembly(t *testing.T) {
	plugin := Plugin()

	if plugin.Name() != "ifunny" {
		t.Fatalf("plugin name = %q, want ifunny", plugin.Name())
	}

	got := make(map[string]sdk.ResourceDescriptor)
	for _, r := range plugin.Resources() {
		if _, dup := got[r.Name]; dup {
			t.Errorf("resource %q declared more than once", r.Name)
		}
		got[r.Name] = r
	}

	if len(got) != len(expectedResources) {
		t.Errorf("resource count = %d, want %d", len(got), len(expectedResources))
	}

	for name, want := range expectedResources {
		r, ok := got[name]
		if !ok {
			t.Errorf("missing resource %q", name)
			continue
		}

		if r.Kinds != want.kind {
			t.Errorf("resource %q kinds = %d, want %d", name, r.Kinds, want.kind)
		}

		specNames := make(map[string]bool, len(r.Spec))
		for _, s := range r.Spec {
			specNames[s.Name] = true
		}
		for _, wantSpec := range want.specs {
			if !specNames[wantSpec] {
				t.Errorf("resource %q missing spec %q", name, wantSpec)
			}
		}
	}
}

func TestClientSpecsAuthModes(t *testing.T) {
	names := make(map[string]bool)
	for _, s := range clientSpecs() {
		names[s.Name] = true
	}
	for _, want := range []string{"auth-basic", "auth-bearer", "user-agent"} {
		if !names[want] {
			t.Errorf("clientSpecs missing auth spec %q", want)
		}
	}
}

// TestAuthoredShadows pins the three source shadows against the
// concrete json shapes ifunny-go produces. A library shape change (e.g.
// a Comment field renamed away from json:"user") would break these
// before any live pipeline hits a mis-extraction.
func TestAuthoredShadows(t *testing.T) {
	t.Run("content", func(t *testing.T) {
		var s authoredContent
		if err := json.Unmarshal([]byte(`{"id":"abc","creator":{"id":"u1","nick":"alice"}}`), &s); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if s.AuthorID() != "u1" || s.AuthorNick() != "alice" {
			t.Errorf("content shadow = (%q,%q), want (u1,alice)", s.AuthorID(), s.AuthorNick())
		}
	})
	t.Run("comment", func(t *testing.T) {
		var s authoredComment
		if err := json.Unmarshal([]byte(`{"id":"c1","cid":"abc","user":{"id":"u2","nick":"bob"}}`), &s); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if s.AuthorID() != "u2" || s.AuthorNick() != "bob" {
			t.Errorf("comment shadow = (%q,%q), want (u2,bob)", s.AuthorID(), s.AuthorNick())
		}
	})
	t.Run("chat event", func(t *testing.T) {
		// ChatEvent nests the author id under the inner json:"user"
		// tag inside the outer "user" object.
		var s authoredChatEvent
		if err := json.Unmarshal([]byte(`{"id":"m1","text":"hi","user":{"user":"u3","nick":"carol"}}`), &s); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if s.AuthorID() != "u3" || s.AuthorNick() != "carol" {
			t.Errorf("chat shadow = (%q,%q), want (u3,carol)", s.AuthorID(), s.AuthorNick())
		}
	})
}

// --- bindEnrich matrix coverage --------------------------------------
//
// A fake enrichPlan + counters lets these tests walk every cell of the
// accept/emit matrix without a live client. Each test is one cell.

// testEntity is the fake T that fakePlan's fetch returns.
type testEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// enrichCounters records how often each plan primitive fired, so tests
// can pin down "how many ops" per matrix cell.
type enrichCounters struct {
	extractCalls int
	resolveCalls int
	fetchCalls   int
}

// fakePlan builds an enrichPlan that reads its target ref off the
// "target-ref" json field of the input record for the rich-in path and
// echoes the sparse ref back for the sparse-in path (like a same-entity
// transformer's identity resolve). fetch returns a copy of resolved
// with ID = ref, or (nil, nil) when resolved is nil (not-found).
//
// If shortCircuit is true, extract returns a hydrated target alongside
// the ref, exercising the rich-out short-circuit that skips fetch.
func fakePlan(name string, c *enrichCounters, resolved *testEntity, shortCircuit bool) enrichPlan {
	return enrichPlan{
		name: name,
		extract: func(_ context.Context, data []byte) (string, any, error) {
			c.extractCalls++
			var s struct {
				TargetRef string `json:"target-ref"`
			}
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			if s.TargetRef == "" {
				return "", nil, nil
			}
			if shortCircuit && resolved != nil {
				out := *resolved
				out.ID = s.TargetRef
				return s.TargetRef, &out, nil
			}
			return s.TargetRef, nil, nil
		},
		resolve: func(_ context.Context, ref string) (string, any, error) {
			c.resolveCalls++
			return ref, nil, nil
		},
		fetch: func(_ context.Context, ref string) (any, error) {
			c.fetchCalls++
			if resolved == nil {
				return nil, nil
			}
			out := *resolved
			out.ID = ref
			return &out, nil
		},
	}
}

func mustBind(t *testing.T, accept, emit string, plan enrichPlan) sdk.Transformer {
	t.Helper()
	tr, err := bindEnrich(testAccept(accept), testEmit(emit), plan)
	if err != nil {
		t.Fatalf("bindEnrich: %v", err)
	}
	return tr
}

// runTimeout bounds how long a single test may spend inside a transformer
// before we call it hung. Applied as a context deadline so the transformer
// gets a real cancellation, not just a t.Fatal race.
const runTimeout = 5 * time.Second

// runOne feeds a single record through tr and returns the single emitted
// record (nil = dropped) and the first reported error. Fatals if tr emits
// more than one record — use runMany for 1→N transformers.
func runOne(t *testing.T, tr sdk.Transformer, data []byte) ([]byte, error) {
	t.Helper()
	outs, err := runMany(t, tr, data)
	switch len(outs) {
	case 0:
		return nil, err
	case 1:
		return outs[0], err
	default:
		t.Fatalf("runOne: transformer emitted %d records, want ≤1", len(outs))
		return nil, err
	}
}

// runMany feeds a single input record through tr and collects every emitted
// output. Blocks on tr closing its out channel (via defer close(out) in
// the transformer contract), so any number of outputs is safe — no fixed
// buffer to overflow. A runTimeout-bounded context is passed to tr so a
// stuck transformer surfaces as a clear Fatal rather than blocking the
// suite.
func runMany(t *testing.T, tr sdk.Transformer, data []byte) ([][]byte, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), runTimeout)
	defer cancel()

	in := make(chan []byte, 1)
	out := make(chan []byte)
	// errs buffered generously so a transformer that reports a burst of
	// per-record errors doesn't block on a channel the test only drains at
	// the end. sendErr's own ctx-aware send prevents unbounded stalls.
	errs := make(chan error, 64)
	in <- data
	close(in)

	trDone := make(chan struct{})
	go func() {
		defer close(trDone)
		tr(ctx, in, out, errs)
	}()

	// Drain outs in the foreground; range unblocks when tr's deferred
	// close(out) runs.
	var got [][]byte
	for b := range out {
		got = append(got, b)
	}
	<-trDone

	if err := ctx.Err(); err != nil {
		t.Fatalf("transformer hung (%v after %s)", err, runTimeout)
	}

	var firstErr error
	select {
	case firstErr = <-errs:
	default:
	}
	return got, firstErr
}

// sparse in + sparse out: resolve echoes the ref through; no extract,
// no fetch.
func TestBindEnrichSparseIn_SparseOut(t *testing.T) {
	c := &enrichCounters{}
	tr := mustBind(t, "string", "string", fakePlan("test", c, nil, false))

	b, err := runOne(t, tr, []byte("xyz"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if string(b) != "xyz" {
		t.Errorf("out = %q, want xyz", b)
	}
	if c.resolveCalls != 1 || c.fetchCalls != 0 || c.extractCalls != 0 {
		t.Errorf("calls = %+v, want resolve=1 fetch=0 extract=0", *c)
	}
}

// sparse in + rich out: resolve then fetch.
func TestBindEnrichSparseIn_RichOut(t *testing.T) {
	c := &enrichCounters{}
	tr := mustBind(t, "string", "json", fakePlan("test", c, &testEntity{Name: "resolved"}, false))

	b, err := runOne(t, tr, []byte("xyz"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := new(testEntity)
	if err := json.Unmarshal(b, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "xyz" || got.Name != "resolved" {
		t.Errorf("out = %+v, want id=xyz name=resolved", got)
	}
	if c.resolveCalls != 1 || c.fetchCalls != 1 {
		t.Errorf("calls = %+v, want resolve=1 fetch=1", *c)
	}
}

// rich in + sparse out: 0 API ops. Target ref is read from the shadow
// and emitted directly.
func TestBindEnrichRichIn_SparseOut(t *testing.T) {
	c := &enrichCounters{}
	tr := mustBind(t, "json", "string", fakePlan("test", c, nil, false))

	b, err := runOne(t, tr, []byte(`{"target-ref":"abc","other":"ignored"}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if string(b) != "abc" {
		t.Errorf("out = %q, want abc", b)
	}
	if c.extractCalls != 1 || c.resolveCalls != 0 || c.fetchCalls != 0 {
		t.Errorf("calls = %+v, want extract=1 resolve=0 fetch=0", *c)
	}
}

// rich in + rich out: extract T's ref, then fetch fresh. The emit
// doctrine forbids returning stale rich objects verbatim.
func TestBindEnrichRichIn_RichOut(t *testing.T) {
	c := &enrichCounters{}
	tr := mustBind(t, "json", "json", fakePlan("test", c, &testEntity{Name: "fresh"}, false))

	b, err := runOne(t, tr, []byte(`{"target-ref":"abc","name":"stale-cache"}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := new(testEntity)
	if err := json.Unmarshal(b, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "fresh" {
		t.Errorf("out.name = %q, want fresh (rich→rich must refetch)", got.Name)
	}
	if c.fetchCalls != 1 {
		t.Errorf("fetch = %d, want 1", c.fetchCalls)
	}
}

// rich in + rich out with short-circuit: extract already hydrated the
// target (by-nick recovery flow); fetch must not fire.
func TestBindEnrichRichIn_RichOut_ShortCircuit(t *testing.T) {
	c := &enrichCounters{}
	tr := mustBind(t, "json", "json", fakePlan("test", c, &testEntity{Name: "recovered"}, true))

	b, err := runOne(t, tr, []byte(`{"target-ref":"abc"}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := new(testEntity)
	if err := json.Unmarshal(b, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "recovered" {
		t.Errorf("out.name = %q, want recovered (short-circuit)", got.Name)
	}
	if c.fetchCalls != 0 {
		t.Errorf("fetch = %d, want 0 (short-circuit skips fetch)", c.fetchCalls)
	}
}

// Rich input whose extract returns an empty ref drops (no author on the
// record; nothing to emit).
func TestBindEnrichRichIn_EmptyRefDrops(t *testing.T) {
	c := &enrichCounters{}
	tr := mustBind(t, "json", "json", fakePlan("test", c, &testEntity{Name: "fresh"}, false))

	b, err := runOne(t, tr, []byte(`{"other":"foo"}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if b != nil {
		t.Errorf("out = %q, want nil (empty ref drops)", b)
	}
	if c.fetchCalls != 0 {
		t.Errorf("fetch = %d, want 0", c.fetchCalls)
	}
}

// A fetch that returns (nil, nil) — the not-found convention — drops
// the record.
func TestBindEnrichNotFoundDrops(t *testing.T) {
	c := &enrichCounters{}
	// resolved == nil → fetch returns (nil, nil).
	tr := mustBind(t, "string", "json", fakePlan("test", c, nil, false))

	b, err := runOne(t, tr, []byte("missing"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if b != nil {
		t.Errorf("out = %q, want nil (not-found drops)", b)
	}
}

// A plan with resolve = nil and accept = "string" errors at bind: the
// unfetchable-source cell (source = comment | chat on ifunny-author).
func TestBindEnrichUnfetchableSparseErrors(t *testing.T) {
	plan := enrichPlan{
		name:    "test",
		extract: func(context.Context, []byte) (string, any, error) { return "", nil, nil },
		fetch:   func(context.Context, string) (any, error) { return nil, nil },
	}
	_, err := bindEnrich(testAccept("string"), testEmit("json"), plan)
	if err == nil {
		t.Fatal("expected bind error for accept=string with nil resolve")
	}
}

// A plan with resolve = nil is fine on rich-in cells.
func TestBindEnrichUnfetchableRichOK(t *testing.T) {
	plan := enrichPlan{
		name: "test",
		extract: func(_ context.Context, data []byte) (string, any, error) {
			var s struct {
				TargetRef string `json:"target-ref"`
			}
			if err := json.Unmarshal(data, &s); err != nil {
				return "", nil, err
			}
			return s.TargetRef, nil, nil
		},
		fetch: func(context.Context, string) (any, error) { return nil, nil },
	}
	tr, err := bindEnrich(testAccept("json"), testEmit("string"), plan)
	if err != nil {
		t.Fatalf("bindEnrich: %v", err)
	}
	b, err := runOne(t, tr, []byte(`{"target-ref":"abc"}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if string(b) != "abc" {
		t.Errorf("out = %q, want abc", b)
	}
}

// An extract that returns an error surfaces per-record on errs, and the
// record drops without halting the stage.
func TestBindEnrichExtractErrorPropagates(t *testing.T) {
	sentinel := errors.New("shadow decode failed")
	plan := enrichPlan{
		name:    "test",
		extract: func(context.Context, []byte) (string, any, error) { return "", nil, sentinel },
		fetch:   func(context.Context, string) (any, error) { return nil, nil },
	}
	tr, err := bindEnrich(testAccept("json"), testEmit("json"), plan)
	if err != nil {
		t.Fatalf("bindEnrich: %v", err)
	}
	if _, err := runOne(t, tr, []byte(`{}`)); !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want sentinel", err)
	}
}

// The stage ctx handed to a bindEnrich transformer must reach the plan's
// per-record fetch, so cancelling the stage aborts an in-flight API call
// rather than only firing the emit guard afterward. This pins that
// down-propagation: fetch sees the exact ctx the transformer was run with.
func TestBindEnrichThreadsStageCtxToFetch(t *testing.T) {
	type ctxKey struct{}
	stageCtx := context.WithValue(t.Context(), ctxKey{}, "stage")

	var gotFetch, gotExtract context.Context
	plan := enrichPlan{
		name: "test",
		extract: func(ctx context.Context, _ []byte) (string, any, error) {
			gotExtract = ctx
			return "abc", nil, nil
		},
		fetch: func(ctx context.Context, ref string) (any, error) {
			gotFetch = ctx
			return &testEntity{ID: ref}, nil
		},
	}
	tr, err := bindEnrich(testAccept("json"), testEmit("json"), plan)
	if err != nil {
		t.Fatalf("bindEnrich: %v", err)
	}

	in := make(chan []byte, 1)
	out := make(chan []byte)
	errs := make(chan error, 1)
	in <- []byte(`{"id":"abc"}`)
	close(in)

	go tr(stageCtx, in, out, errs)
	for range out { //nolint:revive // drain to completion
	}

	if gotExtract == nil || gotExtract.Value(ctxKey{}) != "stage" {
		t.Errorf("extract ctx = %v, want the stage ctx", gotExtract)
	}
	if gotFetch == nil || gotFetch.Value(ctxKey{}) != "stage" {
		t.Errorf("fetch ctx = %v, want the stage ctx", gotFetch)
	}
}

// testParser wraps a map into an sdk.Parser via mapstructure, so tests
// can drive transformer factories without an HCL host. TagName "psy"
// mirrors production; Squash flattens embedded config halves
// (authConfig / acceptConfig / emitConfig) so their fields sit at the
// top level of the values map, matching how the host parses HCL blocks.
func testParser(values map[string]any) sdk.Parser {
	return func(dst any) error {
		dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
			TagName: "psy",
			Squash:  true,
			Result:  dst,
		})
		if err != nil {
			return err
		}
		return dec.Decode(values)
	}
}

// withAuth returns a values map preloaded with a literal auth-basic token
// and android user-agent block — clientFor accepts these without any
// network call (see client.go: literal auth-basic hits MakeClientBasic
// directly), which is what makes bind-level tests here safe. extra fields
// overlay onto the base.
func withAuth(extra map[string]any) map[string]any {
	m := map[string]any{
		"auth-basic": "test-token",
		"user-agent": map[string]any{
			"device":         "android",
			"device-version": "14",
		},
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestParseUserBy covers the new one-arg signature: caller wraps its own
// resource-name prefix around the returned error.
func TestParseUserBy(t *testing.T) {
	cases := []struct {
		in      string
		byNick  bool
		wantErr bool
	}{
		{"id", false, false},
		{"nick", true, false},
		{"bogus", false, true},
		{"", false, true},
	}
	for _, tc := range cases {
		name := tc.in
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			got, err := parseUserBy(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr=%v", err, tc.wantErr)
			}
			if got != tc.byNick {
				t.Errorf("byNick = %v, want %v", got, tc.byNick)
			}
		})
	}
}

// TestTagsTransformer exercises the bind-level refusals and the per-record
// paths that don't reach the iFunny API (rich input carrying tags or an
// unfetchable no-id map). The id-fallback path is intentionally not tested
// here — it calls GetContent, which is a network operation.
func TestTagsTransformer(t *testing.T) {
	t.Run("RichInputTagsPresent", func(t *testing.T) {
		tr, err := tagsTransformer(testParser(withAuth(nil)))
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		outs, err := runMany(t, tr, []byte(`{"tags":["cats","memes"]}`))
		if err != nil {
			t.Fatalf("tr: %v", err)
		}
		got := make([]string, len(outs))
		for i, b := range outs {
			got[i] = string(b)
		}
		if want := []string{"cats", "memes"}; !equalStrings(got, want) {
			t.Errorf("tags = %v, want %v", got, want)
		}
	})

	t.Run("RichInputMixedTypesSkipsNonStrings", func(t *testing.T) {
		tr, err := tagsTransformer(testParser(withAuth(nil)))
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		outs, err := runMany(t, tr, []byte(`{"tags":["a", 1, "b", null, "c"]}`))
		if err != nil {
			t.Fatalf("tr: %v", err)
		}
		got := make([]string, len(outs))
		for i, b := range outs {
			got[i] = string(b)
		}
		if want := []string{"a", "b", "c"}; !equalStrings(got, want) {
			t.Errorf("tags = %v, want %v", got, want)
		}
	})

	t.Run("RichInputEmptyTagsDrops", func(t *testing.T) {
		tr, err := tagsTransformer(testParser(withAuth(nil)))
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		out, err := runOne(t, tr, []byte(`{"tags":[]}`))
		if err != nil {
			t.Fatalf("tr: %v", err)
		}
		if out != nil {
			t.Errorf("out = %s, want nil (drop)", out)
		}
	})

	t.Run("RichInputNoTagsNoIDErrors", func(t *testing.T) {
		tr, err := tagsTransformer(testParser(withAuth(nil)))
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		_, err = runOne(t, tr, []byte(`{"other":"value"}`))
		if err == nil || !strings.Contains(err.Error(), "no id field") {
			t.Errorf("err = %v, want no-id fallback error", err)
		}
	})
}

// TestAuthorTransformerBindErrors covers bind-time refusals only — every
// case here fails before authorTransformer's returned closure runs, so no
// API call is possible.
func TestAuthorTransformerBindErrors(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]any
	}{
		{"SourceMissing", nil},
		{"SourceUnknown", map[string]any{"source": "post"}},
		{"SourceCommentAcceptStringRejected", map[string]any{"source": "comment", "accept": "string"}},
		{"SourceChatAcceptStringRejected", map[string]any{"source": "chat", "accept": "string"}},
		{"EmitByBogus", map[string]any{"source": "content", "emit-by": "email"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := authorTransformer(testParser(withAuth(tc.values)))
			if err == nil {
				t.Fatalf("expected bind error, got nil")
			}
			if !strings.Contains(err.Error(), "ifunny-author") {
				t.Errorf("error message missing ifunny-author prefix: %v", err)
			}
		})
	}
}
