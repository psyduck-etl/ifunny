package ifunnymock

import (
	"context"
	"testing"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
)

// TestMockServerUserByNick verifies that users can be added and retrieved by nick.
func TestMockServerUserByNick(t *testing.T) {
	srv := New(t)
	alice := srv.AddUser("alice")

	client, err := ifunny.MakeClientBasic("test-token", ifunny.RawUserAgent("test-ua"), ifunny.WithAPIRoot(srv.URL()))
	if err != nil {
		t.Fatalf("MakeClientBasic: %v", err)
	}

	got, err := client.GetUser(context.Background(), compose.UserByNick("alice"))
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}

	if got.Nick != alice.Nick || got.ID != alice.ID {
		t.Errorf("got user (%q, %q), want (%q, %q)", got.Nick, got.ID, alice.Nick, alice.ID)
	}
}

// TestMockServerContent verifies content can be added and retrieved.
func TestMockServerContent(t *testing.T) {
	srv := New(t)
	alice := srv.AddUser("alice")
	content := srv.AddContent(alice)

	client, err := ifunny.MakeClientBasic("test-token", ifunny.RawUserAgent("test-ua"), ifunny.WithAPIRoot(srv.URL()))
	if err != nil {
		t.Fatalf("MakeClientBasic: %v", err)
	}

	got, err := client.GetContent(context.Background(), content.ID)
	if err != nil {
		t.Fatalf("GetContent: %v", err)
	}

	if got.ID != content.ID || got.Creator.ID != alice.ID {
		t.Errorf("got content (id=%q, creator=%q), want (id=%q, creator=%q)",
			got.ID, got.Creator.ID, content.ID, alice.ID)
	}
}

// TestMockServerTimeline verifies timeline pagination.
func TestMockServerTimeline(t *testing.T) {
	srv := New(t)
	alice := srv.AddUser("alice")

	// Add 5 content items
	for i := 1; i <= 5; i++ {
		srv.AddContent(alice)
	}

	client, err := ifunny.MakeClientBasic("test-token", ifunny.RawUserAgent("test-ua"), ifunny.WithAPIRoot(srv.URL()))
	if err != nil {
		t.Fatalf("MakeClientBasic: %v", err)
	}

	// Iterate and collect all
	var got []*ifunny.Content
	for r := range client.IterTimeline(context.Background(), alice.ID) {
		if r.Err != nil {
			t.Fatalf("IterTimeline error: %v", r.Err)
		}
		if r.V != nil {
			got = append(got, r.V)
		}
	}

	if len(got) != 5 {
		t.Errorf("got %d items, want 5", len(got))
	}
}

// TestMockServerComments verifies comments can be added and iterated.
func TestMockServerComments(t *testing.T) {
	srv := New(t)
	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	content := srv.AddContent(alice)

	// Add 3 comments
	c1 := srv.AddComment(content, bob, "comment 1")
	c2 := srv.AddComment(content, bob, "comment 2")
	c3 := srv.AddComment(content, bob, "comment 3")

	client, err := ifunny.MakeClientBasic("test-token", ifunny.RawUserAgent("test-ua"), ifunny.WithAPIRoot(srv.URL()))
	if err != nil {
		t.Fatalf("MakeClientBasic: %v", err)
	}

	// Iterate and collect
	var got []*ifunny.Comment
	for r := range client.IterComments(context.Background(), content.ID) {
		if r.Err != nil {
			t.Fatalf("IterComments error: %v", r.Err)
		}
		if r.V != nil {
			got = append(got, r.V)
		}
	}

	if len(got) != 3 {
		t.Errorf("got %d comments, want 3", len(got))
	}

	// Verify all comments are there
	ids := make(map[string]bool)
	for _, c := range got {
		ids[c.ID] = true
	}
	for _, expected := range []*ifunny.Comment{c1, c2, c3} {
		if !ids[expected.ID] {
			t.Errorf("missing comment %q", expected.ID)
		}
	}
}

// TestMockServerReplies verifies replies can be added and iterated.
func TestMockServerReplies(t *testing.T) {
	srv := New(t)
	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	carol := srv.AddUser("carol")
	content := srv.AddContent(alice)

	// Add a comment and replies
	comment := srv.AddComment(content, bob, "main comment")
	srv.AddReply(comment, carol, "reply 1")
	srv.AddReply(comment, carol, "reply 2")

	client, err := ifunny.MakeClientBasic("test-token", ifunny.RawUserAgent("test-ua"), ifunny.WithAPIRoot(srv.URL()))
	if err != nil {
		t.Fatalf("MakeClientBasic: %v", err)
	}

	// Iterate replies
	var got []*ifunny.Comment
	for r := range client.IterReplies(context.Background(), content.ID, comment.ID) {
		if r.Err != nil {
			t.Fatalf("IterReplies error: %v", r.Err)
		}
		if r.V != nil {
			got = append(got, r.V)
		}
	}

	if len(got) != 2 {
		t.Errorf("got %d replies, want 2", len(got))
	}
}

// TestMockServerSmiles verifies smiles iteration.
func TestMockServerSmiles(t *testing.T) {
	srv := New(t)
	alice := srv.AddUser("alice")
	content := srv.AddContent(alice)

	// Add smilers
	bob := srv.AddUser("bob")
	carol := srv.AddUser("carol")
	srv.AddSmiler(content, bob)
	srv.AddSmiler(content, carol)

	client, err := ifunny.MakeClientBasic("test-token", ifunny.RawUserAgent("test-ua"), ifunny.WithAPIRoot(srv.URL()))
	if err != nil {
		t.Fatalf("MakeClientBasic: %v", err)
	}

	// Iterate smiles
	var got []*ifunny.User
	for r := range client.IterSmiles(context.Background(), content.ID) {
		if r.Err != nil {
			t.Fatalf("IterSmiles error: %v", r.Err)
		}
		if r.V != nil {
			got = append(got, r.V)
		}
	}

	if len(got) != 2 {
		t.Errorf("got %d smilers, want 2", len(got))
	}
}
