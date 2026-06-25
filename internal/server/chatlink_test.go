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
		logger: logger,
		cache:  cm,
		chatlinks: &chatlinksapi.Client{
			BaseURL:    apiURL,
			HTTPClient: newResolverHTTPClient(cm, logger),
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
