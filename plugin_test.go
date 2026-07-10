package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

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

// expectedResource describes what the assembly must advertise for a
// resource: its kind and the specs it must expose.
type expectedResource struct {
	kind  sdk.Kind
	specs []string
}

var expectedResources = map[string]expectedResource{
	"ifunny-feed":          {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "feed", "encoding"}},
	"ifunny-timeline":      {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "by-id", "by-nick", "encoding"}},
	"ifunny-explore":       {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "compilation", "kind", "encoding"}},
	"ifunny-comments":      {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "encoding"}},
	"ifunny-replies":       {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "comment", "encoding"}},
	"ifunny-smiles":        {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "encoding"}},
	"ifunny-republishers":  {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "encoding"}},
	"ifunny-subscribers":   {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "user", "encoding"}},
	"ifunny-subscriptions": {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "user", "encoding"}},
	"ifunny-channels":      {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "query", "encoding"}},
	"ifunny-chat-history":  {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "channel", "encoding"}},
	"ifunny-chat-listen":   {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "channel", "stop-after", "encoding"}},
	"ifunny-chat-invites":  {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "stop-after", "encoding"}},
	"ifunny-author":        {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "emit-by", "accept", "emit", "buffer"}},
	"ifunny-tags":          {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "accept", "emit", "buffer"}},
	"ifunny-content":       {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "accept", "emit", "buffer"}},
	"ifunny-user":          {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "by", "accept", "emit", "buffer"}},
	"ifunny-channel":       {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "accept", "emit", "buffer"}},
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

	in := make(chan []byte, 1)
	out := make(chan []byte, 4)
	errs := make(chan error, 1)

	in <- []byte("xyz")
	close(in)

	runEnrich(context.Background(), in, out, errs, stringCodec{}, stringCodec{}, true, 0, spec)

	var got [][]byte
	for b := range out {
		got = append(got, b)
	}
	if len(got) != 1 || string(got[0]) != "xyz" {
		t.Errorf("out = %q, want [xyz]", got)
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

	in := make(chan []byte, 1)
	out := make(chan []byte, 4)
	errs := make(chan error, 1)

	in <- []byte("xyz")
	close(in)

	runEnrich(context.Background(), in, out, errs, stringCodec{}, jsonCodec{}, false, 0, spec)

	got := new(testEntity)
	b := <-out
	if err := json.Unmarshal(b, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "xyz" || got.Name != "resolved" {
		t.Errorf("out = %+v, want id=xyz name=resolved", got)
	}
	if _, ok := <-out; ok {
		t.Errorf("out not closed")
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

	in := make(chan []byte, 1)
	out := make(chan []byte, 4)
	errs := make(chan error, 1)

	in <- []byte(`{"id":"abc","name":"hydrated"}`)
	close(in)

	runEnrich(context.Background(), in, out, errs, jsonCodec{}, stringCodec{}, true, 0, spec)

	b := <-out
	if string(b) != "abc" {
		t.Errorf("out = %q, want abc", string(b))
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

	in := make(chan []byte, 1)
	out := make(chan []byte, 4)
	errs := make(chan error, 1)

	in <- []byte(`{"id":"abc","name":"stale-cache"}`)
	close(in)

	runEnrich(context.Background(), in, out, errs, jsonCodec{}, jsonCodec{}, false, 0, spec)

	got := new(testEntity)
	if err := json.Unmarshal(<-out, got); err != nil {
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

	in := make(chan []byte, 1)
	out := make(chan []byte, 4)
	errs := make(chan error, 1)

	in <- []byte(`{"id":"src-id","name":"no-target"}`)
	close(in)

	runEnrich(context.Background(), in, out, errs, jsonCodec{}, stringCodec{}, true, 0, spec)

	b := <-out
	if string(b) != "src-id" {
		t.Errorf("out = %q, want src-id (via fallback)", string(b))
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

	in := make(chan []byte, 1)
	out := make(chan []byte, 4)
	errs := make(chan error, 1)

	in <- []byte("missing")
	close(in)

	runEnrich(context.Background(), in, out, errs, stringCodec{}, jsonCodec{}, false, 0, spec)

	if _, ok := <-out; ok {
		t.Errorf("expected no output on not-found")
	}
}

// Rich input with neither the target ref nor an "id" fallback field
// → per-record error.
func TestRunEnrichUnusableRichErrors(t *testing.T) {
	c := &enrichCounters{}
	spec := fakeSpec("test", "target-key", c, nil)

	in := make(chan []byte, 1)
	out := make(chan []byte, 4)
	errs := make(chan error, 4)

	in <- []byte(`{"other":"foo"}`)
	close(in)

	runEnrich(context.Background(), in, out, errs, jsonCodec{}, jsonCodec{}, false, 0, spec)

	select {
	case err := <-errs:
		if err == nil {
			t.Errorf("nil error")
		}
	default:
		t.Errorf("expected an error on unusable rich input")
	}
}

// TestRunEnrichPipelinesConcurrently verifies the two-stage pipe
// property: while stage B is blocked fetching record 0, stage A should
// keep decoding records and pushing them into the inter-stage buffer.
// Without pipelining, stage A cannot advance until stage B accepts each
// ref, and targetRef would be called at most once before the gate opens.
//
// Concretely: count targetRef invocations (stage A's per-record work)
// while stage B is blocked. With buffer=N, stage A should be able to
// process 1 (in-flight in B) + N (waiting in refs) = N+1 records before
// blocking on a full buffer.
func TestRunEnrichPipelinesConcurrently(t *testing.T) {
	const records = 8
	const buffer = 4

	var mu sync.Mutex
	targetRefCalls := 0
	gate := make(chan struct{})

	spec := enrichSpec{
		name: "test",
		targetRef: func(m map[string]any) (string, bool) {
			mu.Lock()
			targetRefCalls++
			mu.Unlock()
			s, ok := m["id"].(string)
			return s, ok
		},
		resolveRef: func(ref string) (string, error) { return ref, nil },
		fetchTarget: func(ref string) (any, error) {
			<-gate // stage B blocked here for every record
			return &testEntity{ID: ref}, nil
		},
	}

	in := make(chan []byte, records)
	out := make(chan []byte, records)
	errs := make(chan error, records)

	for i := 0; i < records; i++ {
		in <- []byte(`{"id":"x"}`)
	}
	close(in)

	done := make(chan struct{})
	go func() {
		runEnrich(context.Background(), in, out, errs, jsonCodec{}, jsonCodec{}, false, buffer, spec)
		close(done)
	}()

	// Let stage A run to steady state — it can process at most
	// buffer+1 records before blocking (buffer refs queued + 1 held by
	// stage B, who's parked on gate).
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	stalled := targetRefCalls
	mu.Unlock()

	// Release stage B so runEnrich can finish.
	close(gate)
	<-done

	// Sequential model: targetRefCalls would be 1 here (stage A can't
	// advance while stage B holds it, both in one goroutine).
	// Piped model: stage A gets to buffer+1 records before blocking.
	if stalled < buffer+1 {
		t.Errorf("stage A only processed %d records while stage B blocked; want ≥ %d (buffer+1)", stalled, buffer+1)
	}
}
