package ifunnymock

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ifunny "github.com/open-ifunny/ifunny-go"
)

// Server is a mock HTTP server that simulates the iFunny API for testing.
// It uses httptest.Server under the hood and is safe for concurrent access.
type Server struct {
	srv   *httptest.Server
	store *store
}

// store holds all fixtures and manages concurrent access.
type store struct {
	mu       sync.RWMutex
	users    map[string]*ifunny.User
	content  map[string]*ifunny.Content
	comments map[string][]*ifunny.Comment
	smiles   map[string][]*ifunny.User
	replublishers map[string][]*ifunny.User

	// latency, when >0, is applied to every request before dispatch.
	latency time.Duration
	// errRules are checked in order; the first rule whose pathContains is
	// a substring of the request path fires and, if count != 0, is
	// decremented (count = -1 means permanent).
	errRules []*errRule
}

// errRule injects an HTTP error status for requests whose path contains a
// substring. count > 0 fires that many times then disables; count == -1
// fires forever; count == 0 is a no-op (used to signal a spent rule).
type errRule struct {
	pathContains string
	status       int
	count        int
}

// New creates a new mock server and registers Close() in t.Cleanup.
func New(t testing.TB) *Server {
	s := &Server{
		store: &store{
			users:         make(map[string]*ifunny.User),
			content:       make(map[string]*ifunny.Content),
			comments:      make(map[string][]*ifunny.Comment),
			smiles:        make(map[string][]*ifunny.User),
			replublishers: make(map[string][]*ifunny.User),
		},
	}
	s.srv = httptest.NewServer(s.router())
	t.Cleanup(func() { s.srv.Close() })
	return s
}

// URL returns the base URL of the mock server.
func (s *Server) URL() string {
	return s.srv.URL
}

// AddUser adds a user fixture with a stable ID derived from nick.
// Calling AddUser with the same nick returns the same user (idempotent).
func (s *Server) AddUser(nick string) *ifunny.User {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	id := "u-" + nick
	if u, ok := s.store.users[id]; ok {
		return u
	}

	u := &ifunny.User{
		ID:   id,
		Nick: nick,
	}
	s.store.users[id] = u
	return u
}

// AddContent adds content to a user's timeline.
// ID is "c-<nick>-<seq>" where seq is 1-based per author.
func (s *Server) AddContent(author *ifunny.User) *ifunny.Content {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	// Count existing content by this author
	seq := 1
	for id := range s.store.content {
		// Check if this content belongs to the author
		if len(id) > len(author.Nick)+3 && id[:2] == "c-" {
			// Rough check; real production code would track per-author
		}
	}

	// Find next sequence number for this author
	for i := 1; i <= 1000; i++ {
		testID := fmt.Sprintf("c-%s-%d", author.Nick, i)
		if _, exists := s.store.content[testID]; !exists {
			seq = i
			break
		}
	}

	id := fmt.Sprintf("c-%s-%d", author.Nick, seq)
	c := &ifunny.Content{
		ID:          id,
		DateCreated: 1000000 + int64(seq),
	}
	c.Creator.ID = author.ID
	c.Creator.Nick = author.Nick
	s.store.content[id] = c
	return c
}

// AddComment adds a top-level comment to content.
// ID is "cm-<contentID>-<seq>".
func (s *Server) AddComment(content *ifunny.Content, author *ifunny.User, text string) *ifunny.Comment {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	// Find next sequence number for this content
	seq := 1
	for {
		testID := fmt.Sprintf("cm-%s-%d", content.ID, seq)
		found := false
		if comments, ok := s.store.comments[content.ID]; ok {
			for _, c := range comments {
				if c.ID == testID {
					found = true
					break
				}
			}
		}
		if !found {
			break
		}
		seq++
	}

	id := fmt.Sprintf("cm-%s-%d", content.ID, seq)
	cm := &ifunny.Comment{
		ID:   id,
		CID:  content.ID,
		Text: text,
		User: *author,
	}
	s.store.comments[content.ID] = append(s.store.comments[content.ID], cm)
	return cm
}

// AddReply adds a reply to a comment.
func (s *Server) AddReply(parent *ifunny.Comment, author *ifunny.User, text string) *ifunny.Comment {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	// Find next sequence number for replies
	seq := 1
	for {
		testID := fmt.Sprintf("cm-%s-%s-r%d", parent.CID, parent.ID, seq)
		found := false
		if comments, ok := s.store.comments[parent.CID]; ok {
			for _, c := range comments {
				if c.ID == testID {
					found = true
					break
				}
			}
		}
		if !found {
			break
		}
		seq++
	}

	id := fmt.Sprintf("cm-%s-%s-r%d", parent.CID, parent.ID, seq)
	reply := &ifunny.Comment{
		ID:           id,
		CID:          parent.CID,
		Text:         text,
		User:         *author,
		IsReply:      true,
		ParentCommID: parent.ID,
		RootCommID:   parent.ID,
	}

	// Increment parent's reply count
	if comments, ok := s.store.comments[parent.CID]; ok {
		for _, c := range comments {
			if c.ID == parent.ID {
				c.Num.Replies++
				break
			}
		}
	}

	s.store.comments[parent.CID] = append(s.store.comments[parent.CID], reply)
	return reply
}

// AddSmiler adds a user to the smiles list for content.
func (s *Server) AddSmiler(c *ifunny.Content, u *ifunny.User) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.store.smiles[c.ID] = append(s.store.smiles[c.ID], u)
}

// AddRepublisher adds a user to the republishers list for content.
func (s *Server) AddRepublisher(c *ifunny.Content, u *ifunny.User) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.store.replublishers[c.ID] = append(s.store.replublishers[c.ID], u)
}

// SetLatency configures a per-response delay applied to every request
// before dispatch. Pass 0 to disable. The delay respects the request
// context so tests exercising cancellation return promptly.
func (s *Server) SetLatency(d time.Duration) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.store.latency = d
}

// SetError arranges for the next count requests whose path contains
// pathContains to return the given HTTP status with an empty JSON body.
// count == -1 makes the rule permanent. Rules are matched in FIFO order;
// the first matching rule fires and is decremented.
func (s *Server) SetError(pathContains string, status int, count int) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.store.errRules = append(s.store.errRules, &errRule{
		pathContains: pathContains,
		status:       status,
		count:        count,
	})
}

// consumeErrRule returns the status of the first matching rule (if any)
// and decrements/removes it. Callers hold s.store.mu.
func (s *store) consumeErrRule(path string) (int, bool) {
	for i, r := range s.errRules {
		if !strings.Contains(path, r.pathContains) {
			continue
		}
		status := r.status
		if r.count > 0 {
			r.count--
			if r.count == 0 {
				s.errRules = append(s.errRules[:i], s.errRules[i+1:]...)
			}
		}
		return status, true
	}
	return 0, false
}

// router returns the HTTP handler for the mock server.
func (s *Server) router() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Snapshot latency then sleep (with ctx guard) outside the lock
		// so we don't block other requests during the delay.
		s.store.mu.RLock()
		latency := s.store.latency
		s.store.mu.RUnlock()
		if latency > 0 {
			select {
			case <-time.After(latency):
			case <-r.Context().Done():
				return
			}
		}

		// Error injection wins over normal dispatch.
		s.store.mu.Lock()
		status, injected := s.store.consumeErrRule(r.URL.Path)
		s.store.mu.Unlock()
		if injected {
			http.Error(w, `{"error":"injected","status":`+strconv.Itoa(status)+`}`, status)
			return
		}

		switch r.URL.Path {
		case "/account":
			s.handleAccount(w, r)
		case "/counters":
			s.handleCounters(w, r)
		default:
			s.handleDynamic(w, r)
		}
	})
}

// handleAccount returns the authenticated user (mock).
func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	s.store.mu.RLock()
	mockUser := &ifunny.User{
		ID:   "mock-self",
		Nick: "mock-user",
	}
	s.store.mu.RUnlock()

	resp := map[string]interface{}{
		"data": mockUser,
	}
	json.NewEncoder(w).Encode(resp)
}

// handleCounters returns an empty OK response.
func (s *Server) handleCounters(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"data": map[string]interface{}{},
	}
	json.NewEncoder(w).Encode(resp)
}

// handleDynamic routes to specific endpoints based on the path.
func (s *Server) handleDynamic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Parse /users/{id}
	if len(path) > 7 && path[:7] == "/users/" {
		rest := path[7:]
		if len(rest) > 8 && rest[:8] == "by_nick/" {
			s.handleUserByNick(w, rest[8:])
			return
		}
		s.handleUserByID(w, rest)
		return
	}

	// Parse /content/{id}
	if len(path) > 9 && path[:9] == "/content/" {
		rest := path[9:]
		// Find the content ID (everything before the next /)
		slashIdx := -1
		for i := 0; i < len(rest); i++ {
			if rest[i] == '/' {
				slashIdx = i
				break
			}
		}

		if slashIdx == -1 {
			// Just /content/{id}
			s.handleContent(w, rest)
			return
		}

		contentID := rest[:slashIdx]
		remainder := rest[slashIdx+1:]

		// /content/{id}/comments or /content/{id}/comments/{cid}/replies
		if strings.HasPrefix(remainder, "comments") {
			if remainder == "comments" {
				s.handleComments(w, r, contentID)
				return
			}
			if len(remainder) > 9 && remainder[:9] == "comments/" {
				commentID := remainder[9:]
				// Check if this is /content/{id}/comments/{cid}/replies
				if idx := findSlash(commentID); idx != -1 {
					cid := commentID[:idx]
					rest2 := commentID[idx+1:]
					if strings.HasPrefix(rest2, "replies") {
						s.handleReplies(w, r, contentID, cid)
						return
					}
				}
				// Just /content/{id}/comments
				s.handleComments(w, r, contentID)
				return
			}
		}

		// /content/{id}/smiles
		if remainder == "smiles" {
			s.handleSmiles(w, r, contentID)
			return
		}

		// /content/{id}/republished
		if remainder == "republished" {
			s.handleRepublished(w, r, contentID)
			return
		}
	}

	// /timelines/users/{id}
	if len(path) > 17 && path[:17] == "/timelines/users/" {
		rest := path[17:]
		if len(rest) > 8 && rest[:8] == "by_nick/" {
			s.handleTimelineByNick(w, r, rest[8:])
			return
		}
		s.handleTimeline(w, r, rest)
		return
	}

	http.NotFound(w, r)
}

func findSlash(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

// handleUserByID returns a user by ID.
func (s *Server) handleUserByID(w http.ResponseWriter, id string) {
	s.store.mu.RLock()
	u, ok := s.store.users[id]
	s.store.mu.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "user not found",
		})
		return
	}

	resp := map[string]interface{}{
		"data": u,
	}
	json.NewEncoder(w).Encode(resp)
}

// handleUserByNick returns a user by nick.
func (s *Server) handleUserByNick(w http.ResponseWriter, nick string) {
	s.store.mu.RLock()
	var u *ifunny.User
	for _, user := range s.store.users {
		if user.Nick == nick {
			u = user
			break
		}
	}
	s.store.mu.RUnlock()

	if u == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "user not found",
		})
		return
	}

	resp := map[string]interface{}{
		"data": u,
	}
	json.NewEncoder(w).Encode(resp)
}

// handleContent returns content by ID.
func (s *Server) handleContent(w http.ResponseWriter, id string) {
	s.store.mu.RLock()
	c, ok := s.store.content[id]
	s.store.mu.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "content not found",
		})
		return
	}

	resp := map[string]interface{}{
		"data": c,
	}
	json.NewEncoder(w).Encode(resp)
}

// handleTimeline returns paginated content for a user by ID.
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request, userID string) {
	s.store.mu.RLock()
	// Find all content by this user
	var contents []*ifunny.Content
	for _, c := range s.store.content {
		if c.Creator.ID == userID {
			contents = append(contents, c)
		}
	}
	s.store.mu.RUnlock()

	s.serveContentPage(w, r, contents)
}

// handleTimelineByNick returns paginated content for a user by nick.
func (s *Server) handleTimelineByNick(w http.ResponseWriter, r *http.Request, nick string) {
	s.store.mu.RLock()
	// Find all content by this user
	var contents []*ifunny.Content
	for _, c := range s.store.content {
		if c.Creator.Nick == nick {
			contents = append(contents, c)
		}
	}
	s.store.mu.RUnlock()

	s.serveContentPage(w, r, contents)
}

// handleComments returns paginated comments for content (top-level only).
func (s *Server) handleComments(w http.ResponseWriter, r *http.Request, contentID string) {
	s.store.mu.RLock()
	allComments := s.store.comments[contentID]
	// Filter to top-level comments only (not replies)
	var topLevel []*ifunny.Comment
	for _, c := range allComments {
		if !c.IsReply {
			topLevel = append(topLevel, c)
		}
	}
	s.store.mu.RUnlock()

	s.serveCommentPage(w, r, topLevel)
}

// handleReplies returns paginated replies to a comment.
func (s *Server) handleReplies(w http.ResponseWriter, r *http.Request, contentID, commentID string) {
	s.store.mu.RLock()
	allComments := s.store.comments[contentID]
	var replies []*ifunny.Comment
	for _, c := range allComments {
		if c.IsReply && c.ParentCommID == commentID {
			replies = append(replies, c)
		}
	}
	s.store.mu.RUnlock()

	s.serveReplyPage(w, r, replies)
}

// handleSmiles returns paginated users who smiled content.
func (s *Server) handleSmiles(w http.ResponseWriter, r *http.Request, contentID string) {
	s.store.mu.RLock()
	smilers := s.store.smiles[contentID]
	s.store.mu.RUnlock()

	s.serveUserPage(w, r, smilers, "users")
}

// handleRepublished returns paginated users who republished content.
func (s *Server) handleRepublished(w http.ResponseWriter, r *http.Request, contentID string) {
	s.store.mu.RLock()
	republishers := s.store.replublishers[contentID]
	s.store.mu.RUnlock()

	s.serveUserPage(w, r, republishers, "users")
}

// serveContentPage returns a paginated content response.
func (s *Server) serveContentPage(w http.ResponseWriter, r *http.Request, contents []*ifunny.Content) {
	limit, cursor := parsePaginationParams(r)

	start := 0
	if cursor != "" {
		off, err := strconv.Atoi(cursor)
		if err == nil {
			start = off
		}
	}

	if start >= len(contents) {
		// Empty page
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"content": map[string]interface{}{
					"items":  []*ifunny.Content{},
					"paging": ifunny.Cursor{},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	end := start + limit
	if end > len(contents) {
		end = len(contents)
	}

	items := make([]ifunny.Content, len(contents[start:end]))
	for i, c := range contents[start:end] {
		items[i] = *c
	}

	page := &ifunny.Page[ifunny.Content]{
		Items: items,
	}

	if end < len(contents) {
		nextOffset := strconv.Itoa(end)
		page.Paging.Cursors.Next = base64.RawURLEncoding.EncodeToString([]byte(nextOffset))
		page.Paging.HasNext = true
	}

	resp := map[string]interface{}{
		"data": map[string]interface{}{
			"content": page,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

// serveCommentPage returns a paginated comments response.
func (s *Server) serveCommentPage(w http.ResponseWriter, r *http.Request, comments []*ifunny.Comment) {
	limit, cursor := parsePaginationParams(r)

	start := 0
	if cursor != "" {
		off, err := strconv.Atoi(cursor)
		if err == nil {
			start = off
		}
	}

	if start >= len(comments) {
		// Empty page
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"comments": map[string]interface{}{
					"items":  []*ifunny.Comment{},
					"paging": ifunny.Cursor{},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	end := start + limit
	if end > len(comments) {
		end = len(comments)
	}

	items := make([]ifunny.Comment, len(comments[start:end]))
	for i, c := range comments[start:end] {
		items[i] = *c
	}

	page := &ifunny.Page[ifunny.Comment]{
		Items: items,
	}

	if end < len(comments) {
		nextOffset := strconv.Itoa(end)
		page.Paging.Cursors.Next = base64.RawURLEncoding.EncodeToString([]byte(nextOffset))
		page.Paging.HasNext = true
	}

	resp := map[string]interface{}{
		"data": map[string]interface{}{
			"comments": page,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

// serveReplyPage returns a paginated replies response.
func (s *Server) serveReplyPage(w http.ResponseWriter, r *http.Request, replies []*ifunny.Comment) {
	limit, cursor := parsePaginationParams(r)

	start := 0
	if cursor != "" {
		off, err := strconv.Atoi(cursor)
		if err == nil {
			start = off
		}
	}

	if start >= len(replies) {
		// Empty page
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"replies": map[string]interface{}{
					"items":  []*ifunny.Comment{},
					"paging": ifunny.Cursor{},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	end := start + limit
	if end > len(replies) {
		end = len(replies)
	}

	items := make([]ifunny.Comment, len(replies[start:end]))
	for i, c := range replies[start:end] {
		items[i] = *c
	}

	page := &ifunny.Page[ifunny.Comment]{
		Items: items,
	}

	if end < len(replies) {
		nextOffset := strconv.Itoa(end)
		page.Paging.Cursors.Next = base64.RawURLEncoding.EncodeToString([]byte(nextOffset))
		page.Paging.HasNext = true
	}

	resp := map[string]interface{}{
		"data": map[string]interface{}{
			"replies": page,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

// serveUserPage returns a paginated users response.
func (s *Server) serveUserPage(w http.ResponseWriter, r *http.Request, users []*ifunny.User, key string) {
	limit, cursor := parsePaginationParams(r)

	start := 0
	if cursor != "" {
		off, err := strconv.Atoi(cursor)
		if err == nil {
			start = off
		}
	}

	if start >= len(users) {
		// Empty page
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				key: map[string]interface{}{
					"items":  []*ifunny.User{},
					"paging": ifunny.Cursor{},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	end := start + limit
	if end > len(users) {
		end = len(users)
	}

	items := make([]ifunny.User, len(users[start:end]))
	for i, u := range users[start:end] {
		items[i] = *u
	}

	page := &ifunny.Page[ifunny.User]{
		Items: items,
	}

	if end < len(users) {
		nextOffset := strconv.Itoa(end)
		page.Paging.Cursors.Next = base64.RawURLEncoding.EncodeToString([]byte(nextOffset))
		page.Paging.HasNext = true
	}

	resp := map[string]interface{}{
		"data": map[string]interface{}{
			key: page,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

// parsePaginationParams extracts limit and next cursor from query params.
func parsePaginationParams(r *http.Request) (limit int, cursor string) {
	limit = 3 // Default page size for testing
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	cursor = r.URL.Query().Get("next")
	return
}
