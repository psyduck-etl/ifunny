package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/psyduck-etl/sdk"
)

// TestMain registers tiny codec factories so ifunny tests run without a
// host binary. In production the psyduck host registers the real stdlib
// codec chain; here we need:
//
//   - "json": end-to-end for the codec-dependent transformers/producers.
//   - "string": the terminal-reference codec — its Decode returns a Go
//     string of the input bytes, its Encode expects a string (anything
//     else errors). The enrich transformers dispatch on this shape.
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
// runEnrich directly. They panic on an unknown spec — a test-authoring
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
	"ifunny-feed":          {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "feed", "emit"}},
	"ifunny-timeline":      {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "by-id", "by-nick", "emit"}},
	"ifunny-explore":       {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "compilation", "kind", "emit"}},
	"ifunny-comments":      {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "emit"}},
	"ifunny-replies":       {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "comment", "emit"}},
	"ifunny-smiles":        {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "emit"}},
	"ifunny-republishers":  {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "emit"}},
	"ifunny-subscribers":   {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "user", "emit"}},
	"ifunny-subscriptions": {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "user", "emit"}},
	"ifunny-channels":      {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "query", "emit"}},
	"ifunny-chat-history":  {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "channel", "emit"}},
	"ifunny-chat-listen":   {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "channel", "emit"}},
	"ifunny-chat-invites":  {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "emit"}},
	"ifunny-author":        {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "emit-by", "accept", "emit"}},
	"ifunny-tags":          {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "accept", "emit"}},
	"ifunny-content":       {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "accept", "emit"}},
	"ifunny-user":          {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "by", "accept", "emit"}},
	"ifunny-channel":       {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "accept", "emit"}},
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

func TestExtractAuthor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantID   string
		wantNick string
		wantOK   bool
	}{
		{
			name:     "content creator",
			body:     `{"id":"abc","creator":{"id":"u1","nick":"alice"}}`,
			wantID:   "u1",
			wantNick: "alice",
			wantOK:   true,
		},
		{
			name:     "comment user",
			body:     `{"id":"c1","cid":"abc","user":{"id":"u2","nick":"bob"}}`,
			wantID:   "u2",
			wantNick: "bob",
			wantOK:   true,
		},
		{
			name:     "chat event user",
			body:     `{"id":"m1","text":"hi","user":{"user":"u3","nick":"carol"}}`,
			wantID:   "u3",
			wantNick: "carol",
			wantOK:   true,
		},
		{
			name:   "no author",
			body:   `{"id":"abc"}`,
			wantOK: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal([]byte(tc.body), &m); err != nil {
				t.Fatalf("prep unmarshal: %v", err)
			}
			author, ok := extractAuthor(m)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if author.ID != tc.wantID || author.Nick != tc.wantNick {
				t.Errorf("author = %+v, want id=%q nick=%q", author, tc.wantID, tc.wantNick)
			}
		})
	}
}

// The chat event nests its author id under a "user" json key inside a
// "user" object; extractAuthor must read that shape.
func TestExtractAuthorChatEventShape(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(`{"user":{"user":"u9","nick":"dan"}}`), &m); err != nil {
		t.Fatalf("prep: %v", err)
	}
	author, ok := extractAuthor(m)
	if !ok {
		t.Fatalf("extractAuthor ok=false")
	}
	if author.ID != "u9" {
		t.Errorf("id = %q, want u9", author.ID)
	}
}

// --- runEnrich matrix coverage ------------------------------------------
//
// A fake enrichSpec + counters lets these tests walk every cell of the
// accept/emit matrix without a live client. Each test is one cell.

// testEntity is the fake T that fakeSpec's fetchTarget returns.
type testEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// enrichCounters records how often resolveRef and fetchTarget fired,
// so tests can pin down "how many ops" per matrix cell.
type enrichCounters struct {
	resolveCalls int
	fetchCalls   int
}

// fakeSpec builds an enrichSpec whose targetRef reads targetField off
// the input map, whose resolveRef echoes the source ref through (like
// a same-entity transformer's identity resolve), and whose fetchTarget
// returns a copy of resolved with ID replaced by ref (or nil,nil when
// resolved is nil, simulating not-found).
func fakeSpec(name, targetField string, c *enrichCounters, resolved *testEntity) enrichSpec {
	return enrichSpec{
		name: name,
		targetRef: func(m map[string]any) (string, bool) {
			s, ok := m[targetField].(string)
			return s, ok && s != ""
		},
		resolveRef: func(ref string) (string, error) {
			c.resolveCalls++
			return ref, nil
		},
		fetchTarget: func(ref string) (any, error) {
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

// sparse in + sparse out: resolveRef echoes the ref through; no fetch.
// For a same-entity spec this is 0 API ops (resolveRef is trivial).
func TestRunEnrichSparseIn_SparseOut(t *testing.T) {
	c := &enrichCounters{}
	spec := fakeSpec("test", "id", c, nil)

	b, err := runEnrich([]byte("xyz"), testAccept("string"), testEmit("string"), spec)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if string(b) != "xyz" {
		t.Errorf("out = %q, want xyz", b)
	}
	if c.resolveCalls != 1 {
		t.Errorf("resolve = %d, want 1", c.resolveCalls)
	}
	if c.fetchCalls != 0 {
		t.Errorf("fetch = %d, want 0 (sparse out never hydrates)", c.fetchCalls)
	}
}

// sparse in + rich out: one fetch to hydrate the target.
func TestRunEnrichSparseIn_RichOut(t *testing.T) {
	c := &enrichCounters{}
	spec := fakeSpec("test", "id", c, &testEntity{Name: "resolved"})

	b, err := runEnrich([]byte("xyz"), testAccept("string"), testEmit("json"), spec)
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
	if c.fetchCalls != 1 {
		t.Errorf("fetch = %d, want 1", c.fetchCalls)
	}
}

// rich in + sparse out: 0 ops. Target ref is read from the map and
// emitted directly; no resolve, no fetch.
func TestRunEnrichRichIn_SparseOut(t *testing.T) {
	c := &enrichCounters{}
	spec := fakeSpec("test", "id", c, nil)

	b, err := runEnrich([]byte(`{"id":"abc","name":"hydrated"}`), testAccept("json"), testEmit("string"), spec)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if string(b) != "abc" {
		t.Errorf("out = %q, want abc", b)
	}
	if c.resolveCalls != 0 || c.fetchCalls != 0 {
		t.Errorf("no calls expected; got resolve=%d fetch=%d", c.resolveCalls, c.fetchCalls)
	}
}

// rich in + rich out: fetches T fresh. Rich emission is always a
// hydrator — the input map is never assumed *fully* rich, so we
// re-fetch even when the input already carries a stale name.
func TestRunEnrichRichIn_RichOut(t *testing.T) {
	c := &enrichCounters{}
	spec := fakeSpec("test", "id", c, &testEntity{Name: "fresh"})

	b, err := runEnrich([]byte(`{"id":"abc","name":"stale-cache"}`), testAccept("json"), testEmit("json"), spec)
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

// Partial rich (map missing the transformer's target field): fall back
// to resolveRef via the map's "id" (source-ref) field. This is the
// "trusted only insofar as we find it useful" case.
func TestRunEnrichRichMissingTargetFallsBack(t *testing.T) {
	c := &enrichCounters{}
	// Target field is "author-id" but the map has only "id" — so
	// targetRef misses and we fall through to resolveRef("src-id").
	spec := fakeSpec("test", "author-id", c, nil)

	b, err := runEnrich([]byte(`{"id":"src-id","name":"no-target"}`), testAccept("json"), testEmit("string"), spec)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if string(b) != "src-id" {
		t.Errorf("out = %q, want src-id (via fallback)", b)
	}
	if c.resolveCalls != 1 {
		t.Errorf("resolve = %d, want 1 (fallback)", c.resolveCalls)
	}
}

// A fetchTarget that returns (nil, nil) — the not-found convention —
// drops the record.
func TestRunEnrichNotFoundDrops(t *testing.T) {
	c := &enrichCounters{}
	// resolved == nil → fetchTarget returns (nil, nil).
	spec := fakeSpec("test", "id", c, nil)

	b, err := runEnrich([]byte("missing"), testAccept("string"), testEmit("json"), spec)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if b != nil {
		t.Errorf("out = %q, want nil (not-found drops)", b)
	}
}

// Rich input with neither the target ref nor an "id" fallback field
// → per-record error.
func TestRunEnrichUnusableRichErrors(t *testing.T) {
	c := &enrichCounters{}
	spec := fakeSpec("test", "target-key", c, nil)

	_, err := runEnrich([]byte(`{"other":"foo"}`), testAccept("json"), testEmit("json"), spec)
	if err == nil {
		t.Errorf("expected an error on unusable rich input")
	}
}
