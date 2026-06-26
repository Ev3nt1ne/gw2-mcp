package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"

	chatlinksapi "github.com/Ev3nt1ne/gw2-chatlinks-go/api"
	"github.com/Ev3nt1ne/gw2-chatlinks-go/chatlinks"

	"github.com/AlyxPink/gw2-mcp/internal/cache"
	"github.com/AlyxPink/gw2-mcp/internal/ratelimit"
)

const linkTypeItem = "item"

// newResolveTestServer wires an MCPServer whose chatlink resolver points at a
// fake GW2 API, sharing the project's caching transport.
func newResolveTestServer(t *testing.T, apiURL string) *MCPServer {
	t.Helper()
	logger := log.New(os.Stderr)
	logger.SetLevel(log.ErrorLevel)
	cm := cache.NewManager()
	return &MCPServer{
		logger:           logger,
		cache:            cm,
		rateLimitTracker: &ratelimit.Tracker{},
		chatlinks: &chatlinksapi.Client{
			BaseURL:    apiURL,
			UserAgent:  resolverUserAgent,
			HTTPClient: newResolverHTTPClient(cm, logger, &ratelimit.Tracker{}),
		},
	}
}

func mustEncode(t *testing.T, linkType string, id int) string {
	t.Helper()
	code, err := chatlinks.EncodeSimpleIDLink(chatlinks.SimpleIDLink{LinkType: linkType, ID: id})
	if err != nil {
		t.Fatalf("encode %s %d: %v", linkType, id, err)
	}
	return code
}

func TestDecodeSimpleIDLink_ResolveSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/skills/") {
			_, _ = w.Write([]byte(`{"name":"Meteor Shower"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := newResolveTestServer(t, srv.URL)
	code := mustEncode(t, "skill", 5491)

	res, err := s.decodeSimpleIDLink(context.Background(), code, "skill", true)
	if err != nil {
		t.Fatalf("decodeSimpleIDLink: %v", err)
	}
	if res.Name != "Meteor Shower" {
		t.Errorf("Name = %q, want %q", res.Name, "Meteor Shower")
	}
	if len(res.ResolveWarnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.ResolveWarnings)
	}
}

// A resolution failure must NOT discard the successful decode (S3).
func TestDecodeSimpleIDLink_ResolveFailureIsBestEffort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound) // every lookup 404s
	}))
	defer srv.Close()

	s := newResolveTestServer(t, srv.URL)
	code := mustEncode(t, linkTypeItem, 0)

	res, err := s.decodeSimpleIDLink(context.Background(), code, linkTypeItem, true)
	if err != nil {
		t.Fatalf("decode should succeed even if resolution fails, got err: %v", err)
	}
	if res == nil {
		t.Fatal("expected a decoded result, got nil")
	}
	if res.ID != 0 || res.Type != linkTypeItem {
		t.Errorf("decoded result = %+v, want item id 0", res)
	}
	if res.Name != "" {
		t.Errorf("Name = %q, want empty on resolution failure", res.Name)
	}
	if len(res.ResolveWarnings) == 0 {
		t.Error("expected a resolve_warnings entry describing the failed lookup")
	}
}

// TestNewMCPServer_ChatlinksClientUsesResolverUserAgent exercises the real
// NewMCPServer construction in server.go directly (not the newResolveTestServer
// fixture, which builds its own MCPServer struct literal and would not catch
// a regression here). Guards against gw2-chatlinks-go's own default
// User-Agent silently winning over resolverUserAgent: that library always
// sends a non-empty User-Agent of its own now, so cachingTransport's "if
// empty" fallback (resolvecache.go) no longer gets a chance to apply
// resolverUserAgent unless chatlinksapi.Client.UserAgent is set explicitly.
func TestNewMCPServer_ChatlinksClientUsesResolverUserAgent(t *testing.T) {
	logger := log.New(os.Stderr)
	logger.SetLevel(log.ErrorLevel)
	s, err := NewMCPServer(logger)
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}
	if s.chatlinks.UserAgent != resolverUserAgent {
		t.Errorf("chatlinks.UserAgent = %q, want %q", s.chatlinks.UserAgent, resolverUserAgent)
	}
}

// TestChatlinksClient_SendsResolverUserAgent confirms that, given a correctly
// configured Client.UserAgent (as the fixture above sets, mirroring
// NewMCPServer), the header actually reaches the wire through the full
// caching-transport round trip.
func TestChatlinksClient_SendsResolverUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"name":"X"}`))
	}))
	defer srv.Close()

	s := newResolveTestServer(t, srv.URL)
	code := mustEncode(t, "skill", 1)
	if _, err := s.decodeSimpleIDLink(context.Background(), code, "skill", true); err != nil {
		t.Fatalf("decodeSimpleIDLink: %v", err)
	}
	if gotUA != resolverUserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, resolverUserAgent)
	}
}

func TestDecodeSimpleIDLink_NoResolveSkipsNetwork(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"name":"should not be fetched"}`))
	}))
	defer srv.Close()

	s := newResolveTestServer(t, srv.URL)
	code := mustEncode(t, "skill", 5491)

	res, err := s.decodeSimpleIDLink(context.Background(), code, "skill", false)
	if err != nil {
		t.Fatalf("decodeSimpleIDLink: %v", err)
	}
	if called {
		t.Error("resolve=false must not call the API")
	}
	if res.Name != "" || len(res.ResolveWarnings) != 0 {
		t.Errorf("unexpected resolution output: %+v", res)
	}
}
