package ifunnymock

import (
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
	mu           sync.RWMutex
	users        map[string]*ifunny.User
	content      map[string]*ifunny.Content
	comments     map[string][]*ifunny.Comment
	smiles       map[string][]*ifunny.User
	republishers map[string][]*ifunny.User

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
			users:        make(map[string]*ifunny.User),
			content:      make(map[string]*ifunny.Content),
			comments:     make(map[string][]*ifunny.Comment),
			smiles:       make(map[string][]*ifunny.User),
			republishers: make(map[string][]*ifunny.User),
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

	// Find the next free sequence number for this author.
	seq := 1
	for {
		if _, exists := s.store.content[fmt.Sprintf("c-%s-%d", author.Nick, seq)]; !exists {
			break
		}
		seq++
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

	seq := len(s.store.comments[content.ID]) + 1
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

	seq := len(s.store.comments[parent.CID]) + 1
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

	// Increment parent's reply count.
	for _, c := range s.store.comments[parent.CID] {
		if c.ID == parent.ID {
			c.Num.Replies++
			break
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
	s.store.republishers[c.ID] = append(s.store.republishers[c.ID], u)
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
	writeData(w, &ifunny.User{ID: "mock-self", Nick: "mock-user"})
}

// handleCounters returns an empty OK response.
func (s *Server) handleCounters(w http.ResponseWriter, r *http.Request) {
	writeData(w, map[string]any{})
}

// handleDynamic routes to specific endpoints based on the path.
func (s *Server) handleDynamic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if rest, ok := strings.CutPrefix(path, "/users/"); ok {
		if nick, ok := strings.CutPrefix(rest, "by_nick/"); ok {
			s.handleUserByNick(w, nick)
			return
		}
		s.handleUserByID(w, rest)
		return
	}

	if rest, ok := strings.CutPrefix(path, "/timelines/users/"); ok {
		if nick, ok := strings.CutPrefix(rest, "by_nick/"); ok {
			s.handleTimelineByNick(w, r, nick)
			return
		}
		s.handleTimeline(w, r, rest)
		return
	}

	if rest, ok := strings.CutPrefix(path, "/content/"); ok {
		contentID, sub, hasSub := strings.Cut(rest, "/")
		switch {
		case !hasSub:
			s.handleContent(w, contentID)
		case sub == "smiles":
			s.handleSmiles(w, r, contentID)
		case sub == "republished":
			s.handleRepublished(w, r, contentID)
		case sub == "comments":
			s.handleComments(w, r, contentID)
		case strings.HasPrefix(sub, "comments/"):
			commentID, tail, hasTail := strings.Cut(strings.TrimPrefix(sub, "comments/"), "/")
			if hasTail && strings.HasPrefix(tail, "replies") {
				s.handleReplies(w, r, contentID, commentID)
			} else {
				s.handleComments(w, r, contentID)
			}
		default:
			http.NotFound(w, r)
		}
		return
	}

	http.NotFound(w, r)
}

// handleUserByID returns a user by ID.
func (s *Server) handleUserByID(w http.ResponseWriter, id string) {
	s.store.mu.RLock()
	u, ok := s.store.users[id]
	var user ifunny.User
	if ok {
		user = *u
	}
	s.store.mu.RUnlock()

	if !ok {
		writeNotFound(w, "user not found")
		return
	}
	writeData(w, &user)
}

// handleUserByNick returns a user by nick.
func (s *Server) handleUserByNick(w http.ResponseWriter, nick string) {
	s.store.mu.RLock()
	var user ifunny.User
	found := false
	for _, u := range s.store.users {
		if u.Nick == nick {
			user, found = *u, true
			break
		}
	}
	s.store.mu.RUnlock()

	if !found {
		writeNotFound(w, "user not found")
		return
	}
	writeData(w, &user)
}

// handleContent returns content by ID.
func (s *Server) handleContent(w http.ResponseWriter, id string) {
	s.store.mu.RLock()
	c, ok := s.store.content[id]
	var content ifunny.Content
	if ok {
		content = *c
	}
	s.store.mu.RUnlock()

	if !ok {
		writeNotFound(w, "content not found")
		return
	}
	writeData(w, &content)
}

// handleTimeline returns paginated content for a user by ID.
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request, userID string) {
	s.store.mu.RLock()
	var contents []ifunny.Content
	for _, c := range s.store.content {
		if c.Creator.ID == userID {
			contents = append(contents, *c)
		}
	}
	s.store.mu.RUnlock()

	servePage(w, r, contents, "content")
}

// handleTimelineByNick returns paginated content for a user by nick.
func (s *Server) handleTimelineByNick(w http.ResponseWriter, r *http.Request, nick string) {
	s.store.mu.RLock()
	var contents []ifunny.Content
	for _, c := range s.store.content {
		if c.Creator.Nick == nick {
			contents = append(contents, *c)
		}
	}
	s.store.mu.RUnlock()

	servePage(w, r, contents, "content")
}

// handleComments returns paginated comments for content (top-level only).
func (s *Server) handleComments(w http.ResponseWriter, r *http.Request, contentID string) {
	s.store.mu.RLock()
	var topLevel []ifunny.Comment
	for _, c := range s.store.comments[contentID] {
		if !c.IsReply {
			topLevel = append(topLevel, *c)
		}
	}
	s.store.mu.RUnlock()

	servePage(w, r, topLevel, "comments")
}

// handleReplies returns paginated replies to a comment.
func (s *Server) handleReplies(w http.ResponseWriter, r *http.Request, contentID, commentID string) {
	s.store.mu.RLock()
	var replies []ifunny.Comment
	for _, c := range s.store.comments[contentID] {
		if c.IsReply && c.ParentCommID == commentID {
			replies = append(replies, *c)
		}
	}
	s.store.mu.RUnlock()

	servePage(w, r, replies, "replies")
}

// handleSmiles returns paginated users who smiled content.
func (s *Server) handleSmiles(w http.ResponseWriter, r *http.Request, contentID string) {
	s.store.mu.RLock()
	users := copyUsers(s.store.smiles[contentID])
	s.store.mu.RUnlock()

	servePage(w, r, users, "users")
}

// handleRepublished returns paginated users who republished content.
func (s *Server) handleRepublished(w http.ResponseWriter, r *http.Request, contentID string) {
	s.store.mu.RLock()
	users := copyUsers(s.store.republishers[contentID])
	s.store.mu.RUnlock()

	servePage(w, r, users, "users")
}

// copyUsers dereferences a slice of user pointers into values. Callers hold
// s.store.mu so the copy is race-free against concurrent fixture mutation.
func copyUsers(src []*ifunny.User) []ifunny.User {
	out := make([]ifunny.User, len(src))
	for i, u := range src {
		out[i] = *u
	}
	return out
}

// servePage writes a paginated response wrapping items under key. The cursor
// is a plain offset into items; HasNext is set when more items remain.
func servePage[T ifunny.Comment | ifunny.Content | ifunny.User | ifunny.ChatChannel](w http.ResponseWriter, r *http.Request, items []T, key string) {
	limit, start := parsePagination(r)

	page := &ifunny.Page[T]{Items: []T{}}
	if start < len(items) {
		end := min(start+limit, len(items))
		page.Items = items[start:end]
		if end < len(items) {
			page.Paging.Cursors.Next = strconv.Itoa(end)
			page.Paging.HasNext = true
		}
	}

	writeData(w, map[string]any{key: page})
}

// parsePagination extracts the page limit and start offset from query params.
func parsePagination(r *http.Request) (limit, start int) {
	limit = 3 // Default page size for testing.
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if off, err := strconv.Atoi(r.URL.Query().Get("next")); err == nil {
		start = off
	}
	return
}

// writeData encodes data wrapped in the API's {"data": ...} envelope.
func writeData(w http.ResponseWriter, data any) {
	json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// writeNotFound writes a 404 with a JSON error body.
func writeNotFound(w http.ResponseWriter, msg string) {
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]any{"error": msg})
}
