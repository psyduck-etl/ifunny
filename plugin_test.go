package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/psyduck-etl/sdk"
)

// expectedResource describes what the assembly must advertise for a
// resource: its kind and the specs it must expose.
type expectedResource struct {
	kind  sdk.Kind
	specs []string
}

var expectedResources = map[string]expectedResource{
	"ifunny-feed":           {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "feed"}},
	"ifunny-timeline":       {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "by-id", "by-nick"}},
	"ifunny-explore":        {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "compilation", "kind"}},
	"ifunny-comments":       {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content"}},
	"ifunny-replies":        {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content", "comment"}},
	"ifunny-smiles":         {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content"}},
	"ifunny-republishers":   {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "content"}},
	"ifunny-subscribers":    {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "user"}},
	"ifunny-subscriptions":  {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "user"}},
	"ifunny-channels":       {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "query"}},
	"ifunny-chat-history":   {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "channel"}},
	"ifunny-chat-listen":    {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "channel", "stop-after"}},
	"ifunny-chat-invites":   {sdk.PRODUCER, []string{"auth-basic", "auth-bearer", "user-agent", "stop-after"}},
	"ifunny-author":         {sdk.TRANSFORMER, nil},
	"ifunny-tags":           {sdk.TRANSFORMER, nil},
	"ifunny-lookup-content": {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent"}},
	"ifunny-lookup-user":    {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent", "by-id", "by-nick"}},
	"ifunny-lookup-channel": {sdk.TRANSFORMER, []string{"auth-basic", "auth-bearer", "user-agent"}},
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
			author, ok, err := extractAuthor([]byte(tc.body))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
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
// "user" object; extractAuthor must read that shape. This guards the
// specific mapping ChatEvent uses.
func TestExtractAuthorChatEventShape(t *testing.T) {
	author, ok, err := extractAuthor([]byte(`{"user":{"user":"u9","nick":"dan"}}`))
	if err != nil || !ok {
		t.Fatalf("extractAuthor ok=%v err=%v", ok, err)
	}
	if author.ID != "u9" {
		t.Errorf("id = %q, want u9", author.ID)
	}
}

func TestAuthorTransformer(t *testing.T) {
	transform, err := authorTransformer(nil)
	if err != nil {
		t.Fatalf("build transformer: %v", err)
	}

	// Test with an entity that has an author.
	in := make(chan []byte, 1)
	out := make(chan []byte, 1)
	errs := make(chan error, 1)

	in <- []byte(`{"id":"abc","creator":{"id":"u1","nick":"alice"}}`)
	close(in)

	transform(context.Background(), in, out, errs)

	result, ok := <-out
	if !ok {
		t.Fatal("expected out to be readable")
	}
	if _, ok := <-out; ok {
		t.Fatal("expected out to be closed")
	}

	got := new(authorRef)
	if err := json.Unmarshal(result, got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got.ID != "u1" || got.Nick != "alice" {
		t.Errorf("output = %+v, want id=u1 nick=alice", got)
	}

	// Test with an entity that has no author (should be dropped).
	in2 := make(chan []byte, 1)
	out2 := make(chan []byte, 1)
	errs2 := make(chan error, 1)

	in2 <- []byte(`{"id":"abc"}`)
	close(in2)

	transform(context.Background(), in2, out2, errs2)

	if _, ok := <-out2; ok {
		t.Errorf("expected no output for author-less entity, but got data")
	}
}

func TestTagsTransformer(t *testing.T) {
	transform, err := tagsTransformer(nil)
	if err != nil {
		t.Fatalf("build transformer: %v", err)
	}

	// Test with a post that has tags.
	in := make(chan []byte, 1)
	out := make(chan []byte, 1)
	errs := make(chan error, 1)

	in <- []byte(`{"id":"abc","tags":["funny","cats","meme"],"type":"pic"}`)
	close(in)

	transform(context.Background(), in, out, errs)

	result, ok := <-out
	if !ok {
		t.Fatal("expected out to be readable")
	}
	if _, ok := <-out; ok {
		t.Fatal("expected out to be closed")
	}

	got := new(struct {
		Tags []string `json:"tags"`
	})
	if err := json.Unmarshal(result, got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(got.Tags) != 3 || got.Tags[0] != "funny" || got.Tags[2] != "meme" {
		t.Errorf("tags = %v, want [funny cats meme]", got.Tags)
	}

	// A post with no tags is dropped.
	for _, body := range []string{`{"id":"abc"}`, `{"id":"abc","tags":[]}`} {
		in2 := make(chan []byte, 1)
		out2 := make(chan []byte, 1)
		errs2 := make(chan error, 1)

		in2 <- []byte(body)
		close(in2)

		transform(context.Background(), in2, out2, errs2)

		if _, ok := <-out2; ok {
			t.Errorf("expected no output for %s, but got data", body)
		}
	}
}

func TestLookup(t *testing.T) {
	type entity struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	var gotID string
	transform := lookup(func(id string) (any, error) {
		gotID = id
		return entity{ID: id, Name: "resolved"}, nil
	})

	in := make(chan []byte, 1)
	out := make(chan []byte, 1)
	errs := make(chan error, 1)

	in <- []byte(`{"id":"xyz","nick":"ignored"}`)
	close(in)

	transform(context.Background(), in, out, errs)

	result, ok := <-out
	if !ok {
		t.Fatal("expected out to be readable")
	}
	if _, ok := <-out; ok {
		t.Fatal("expected out to be closed")
	}

	if gotID != "xyz" {
		t.Errorf("looker got id %q, want xyz", gotID)
	}

	got := new(entity)
	if err := json.Unmarshal(result, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "resolved" {
		t.Errorf("resolved name = %q, want resolved", got.Name)
	}
}

// A looker returning nil (e.g. a not-found lookup) drops the datum.
func TestLookupNilDropsDatum(t *testing.T) {
	transform := lookup(func(string) (any, error) {
		return nil, nil
	})

	in := make(chan []byte, 1)
	out := make(chan []byte, 1)
	errs := make(chan error, 1)

	in <- []byte(`{"id":"missing"}`)
	close(in)

	transform(context.Background(), in, out, errs)

	if _, ok := <-out; ok {
		t.Errorf("expected no output for nil lookup, but got data")
	}
}
