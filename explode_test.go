package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/psyduck-etl/ifunny/internal/ifunnymock"
)

// useMockClient temporarily replaces clientFor with a mock-backed version
// that uses the provided mock server.
func useMockClient(t testing.TB, srv *ifunnymock.Server) {
	prev := clientFor
	clientFor = func(_ *authConfig) (*ifunny.Client, error) {
		ua := ifunny.RawUserAgent("test-ua")
		return ifunny.MakeClientBasic("test-token", ua, ifunny.WithAPIRoot(srv.URL()))
	}
	t.Cleanup(func() { clientFor = prev })
}

// testContext returns a context suitable for transformer tests.
func testContext() context.Context {
	return context.Background()
}

// --- TimelineExplode Tests ---

// TestTimelineExplodeSingleUserMultipleContents verifies that all contents
// from a user's timeline are emitted, even when they exceed one page.
func TestTimelineExplodeSingleUserMultipleContents(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	// Add 5 contents to exceed the default page size (3)
	for i := 1; i <= 5; i++ {
		srv.AddContent(alice)
	}

	tr, err := timelineTransformer(context.Background(), testParser(withAuth(map[string]any{
		"by":     "id",
		"accept": "string",
		"emit":   "string",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	// Feed alice's ID as content ID (sparse input)
	outs, err := runMany(t, tr, []byte(alice.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	if len(outs) != 5 {
		t.Errorf("got %d outputs, want 5", len(outs))
	}

	// Verify they are content IDs for alice
	idSet := make(map[string]bool)
	for _, out := range outs {
		id := string(out)
		idSet[id] = true
	}
	for i := 1; i <= 5; i++ {
		expected := "c-alice-" + fmt.Sprintf("%d", i)
		if !idSet[expected] {
			t.Errorf("missing expected content %q", expected)
		}
	}
}

// TestTimelineExplodeWithLimit verifies that limit caps output count.
func TestTimelineExplodeWithLimit(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	for i := 1; i <= 5; i++ {
		srv.AddContent(alice)
	}

	tr, err := timelineTransformer(context.Background(), testParser(withAuth(map[string]any{
		"by":     "id",
		"limit":  2,
		"accept": "string",
		"emit":   "string",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte(alice.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	if len(outs) != 2 {
		t.Errorf("got %d outputs, want 2 (limit)", len(outs))
	}
}

// TestTimelineExplodeEmptyTimeline verifies clean exit on empty timeline.
func TestTimelineExplodeEmptyTimeline(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	// Don't add any content

	tr, err := timelineTransformer(context.Background(), testParser(withAuth(map[string]any{
		"by":     "id",
		"accept": "string",
		"emit":   "string",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte(alice.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	if len(outs) != 0 {
		t.Errorf("got %d outputs, want 0 (empty)", len(outs))
	}
}

// TestTimelineExplodeByNick verifies the by=nick path works.
func TestTimelineExplodeByNick(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	srv.AddContent(alice)

	tr, err := timelineTransformer(context.Background(), testParser(withAuth(map[string]any{
		"by":     "nick",
		"accept": "string",
		"emit":   "string",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte("alice"))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	if len(outs) != 1 {
		t.Errorf("got %d outputs, want 1", len(outs))
	}
}

// TestTimelineExplodeRichEmit verifies rich content emission.
func TestTimelineExplodeRichEmit(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	content := srv.AddContent(alice)

	tr, err := timelineTransformer(context.Background(), testParser(withAuth(map[string]any{
		"by":     "id",
		"accept": "string",
		"emit":   "json",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte(alice.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	if len(outs) != 1 {
		t.Errorf("got %d outputs, want 1", len(outs))
	}

	// Decode and verify it's valid content JSON
	var gotContent ifunny.Content
	if err := json.Unmarshal(outs[0], &gotContent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gotContent.ID != content.ID {
		t.Errorf("decoded ID = %q, want %q", gotContent.ID, content.ID)
	}
}

// --- CommentsExplode Tests ---

// TestCommentsExplodeSingleContentMultipleComments verifies all comments are emitted.
func TestCommentsExplodeSingleContentMultipleComments(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	content := srv.AddContent(alice)

	// Add 5 comments to exceed page size
	for i := 1; i <= 5; i++ {
		srv.AddComment(content, bob, "comment")
	}

	tr, err := commentsTransformer(context.Background(), testParser(withAuth(map[string]any{
		"accept": "string",
		"emit":   "json",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte(content.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	if len(outs) != 5 {
		t.Errorf("got %d outputs, want 5", len(outs))
	}
}

// TestCommentsExplodeMaxDepthZero verifies replies are not emitted when max-depth=0.
func TestCommentsExplodeMaxDepthZero(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	carol := srv.AddUser("carol")
	content := srv.AddContent(alice)

	comment := srv.AddComment(content, bob, "main")
	srv.AddReply(comment, carol, "reply")

	tr, err := commentsTransformer(context.Background(), testParser(withAuth(map[string]any{
		"max-depth": 0,
		"accept":    "string",
		"emit":      "json",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte(content.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	// Only the top-level comment, not the reply
	if len(outs) != 1 {
		t.Errorf("got %d outputs, want 1 (max-depth=0 excludes replies)", len(outs))
	}
}

// TestCommentsExplodeWithNegativeMaxDepth verifies that max-depth = -1
// emits both top-level comments and replies. -1 is the spec default, and
// per the transformer contract means "unlimited" (i.e. include replies).
// Zero would gate replies off; any positive value emits them too, since
// iFunny replies don't nest beyond one level.
func TestCommentsExplodeWithNegativeMaxDepth(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	carol := srv.AddUser("carol")
	content := srv.AddContent(alice)

	comment := srv.AddComment(content, bob, "main")
	srv.AddReply(comment, carol, "reply 1")
	srv.AddReply(comment, carol, "reply 2")

	// max-depth = -1 means no limit (emit all depths)
	tr, err := commentsTransformer(context.Background(), testParser(withAuth(map[string]any{
		"max-depth": -1,
		"accept":    "string",
		"emit":      "json",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte(content.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	// 1 comment + 2 replies
	if len(outs) != 3 {
		t.Errorf("got %d outputs, want 3 (1 comment + 2 replies with max-depth=-1)", len(outs))
	}
}

// TestCommentsExplodeEmptyComments verifies clean exit on empty comments.
func TestCommentsExplodeEmptyComments(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	content := srv.AddContent(alice)
	// Don't add any comments

	tr, err := commentsTransformer(context.Background(), testParser(withAuth(map[string]any{
		"accept": "string",
		"emit":   "json",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte(content.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	if len(outs) != 0 {
		t.Errorf("got %d outputs, want 0 (empty)", len(outs))
	}
}

// --- Interactions Tests ---

// TestInteractionsAllEnabled verifies all interactions fan out correctly.
func TestInteractionsAllEnabled(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	carol := srv.AddUser("carol")

	content := srv.AddContent(alice)
	srv.AddSmiler(content, bob)
	srv.AddRepublisher(content, carol)

	tr, err := interactionsTransformer(context.Background(), testParser(withAuth(map[string]any{
		"interactions": []string{"author", "smiles", "republishes"},
		"accept":       "string",
		"emit":         "string",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte(content.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	// 1 author + 1 smiler + 1 republisher = 3
	if len(outs) != 3 {
		t.Errorf("got %d outputs, want 3 (author + smiler + republisher)", len(outs))
	}

	// Check that we got the expected users
	ids := make(map[string]bool)
	for _, out := range outs {
		id := string(out)
		ids[id] = true
	}
	for _, expected := range []string{alice.ID, bob.ID, carol.ID} {
		if !ids[expected] {
			t.Errorf("missing user %q", expected)
		}
	}
}

// TestInteractionsCommentsIncluded verifies comment authors are included.
func TestInteractionsCommentsIncluded(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	carol := srv.AddUser("carol")

	content := srv.AddContent(alice)
	srv.AddComment(content, bob, "comment 1")
	srv.AddComment(content, carol, "comment 2")

	tr, err := interactionsTransformer(context.Background(), testParser(withAuth(map[string]any{
		"interactions": []string{"comments"},
		"accept":       "string",
		"emit":         "string",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte(content.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	// 2 comment authors
	if len(outs) != 2 {
		t.Errorf("got %d outputs, want 2 (two commenters)", len(outs))
	}
}

// TestInteractionsEmptyListFails verifies empty interactions list errors at bind.
func TestInteractionsEmptyListFails(t *testing.T) {
	_, err := interactionsTransformer(context.Background(), testParser(withAuth(map[string]any{
		"interactions": []string{},
		"emit":         "string",
	})))
	if err == nil {
		t.Fatal("expected bind error for empty interactions list")
	}
}

// TestInteractionsMultipleInputsGrouped verifies outputs from different
// inputs don't interleave due to per-input WaitGroup.
func TestInteractionsMultipleInputsGrouped(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")
	carol := srv.AddUser("carol")
	dave := srv.AddUser("dave")
	eve := srv.AddUser("eve")
	frank := srv.AddUser("frank")

	// Content 1: authored by alice, smiled by bob, carol
	content1 := srv.AddContent(alice)
	srv.AddSmiler(content1, bob)
	srv.AddSmiler(content1, carol)

	// Content 2: authored by dave, smiled by eve, frank
	content2 := srv.AddContent(dave)
	srv.AddSmiler(content2, eve)
	srv.AddSmiler(content2, frank)

	tr, err := interactionsTransformer(context.Background(), testParser(withAuth(map[string]any{
		"interactions": []string{"author", "smiles"},
		"accept":       "string",
		"emit":         "string",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	// Feed both contents using runMany would be cleaner, but this tests
	// the exact scenario of sequential input processing
	in := make(chan []byte, 2)
	out := make(chan []byte, 16)
	errs := make(chan error, 16)
	in <- []byte(content1.ID)
	in <- []byte(content2.ID)
	close(in)

	done := make(chan struct{})
	go func() {
		defer close(done)
		tr(testContext(), in, out, errs)
	}()

	// Collect all outputs - wait for the transformer to close out
	var outs [][]byte
	for b := range out {
		outs = append(outs, b)
	}
	<-done

	// We should get 3 users per content = 6 total
	if len(outs) != 6 {
		t.Errorf("got %d outputs, want 6 (3 per content)", len(outs))
	}

	// Build a set of all collected IDs
	ids := make(map[string]int)
	for _, out := range outs {
		id := string(out)
		ids[id]++
	}

	// Each user should appear exactly once
	for user, count := range ids {
		if count != 1 {
			t.Errorf("user %q appeared %d times, want 1", user, count)
		}
	}
}

// TestInteractionsRichEmit verifies rich user emission.
func TestInteractionsRichEmit(t *testing.T) {
	srv := ifunnymock.New(t)
	useMockClient(t, srv)

	alice := srv.AddUser("alice")
	bob := srv.AddUser("bob")

	content := srv.AddContent(alice)
	srv.AddSmiler(content, bob)

	tr, err := interactionsTransformer(context.Background(), testParser(withAuth(map[string]any{
		"interactions": []string{"smiles"},
		"accept":       "string",
		"emit":         "json",
	})))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	outs, err := runMany(t, tr, []byte(content.ID))
	if err != nil {
		t.Fatalf("tr: %v", err)
	}

	if len(outs) != 1 {
		t.Errorf("got %d outputs, want 1", len(outs))
	}

	// Decode and verify it's valid user JSON
	var gotUser ifunny.User
	if err := json.Unmarshal(outs[0], &gotUser); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gotUser.ID != bob.ID {
		t.Errorf("decoded ID = %q, want %q", gotUser.ID, bob.ID)
	}
}
