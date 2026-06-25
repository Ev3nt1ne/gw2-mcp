package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/AlyxPink/gw2-mcp/internal/cache"
)

func newTestResolverClient() *http.Client {
	logger := log.New(os.Stderr)
	logger.SetLevel(log.ErrorLevel)
	return newResolverHTTPClient(cache.NewManager(), logger)
}

func TestResolverHTTPClient_CachesGET(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"name":"Mystic Forge"}`))
	}))
	defer srv.Close()

	client := newTestResolverClient()

	for i := 0; i < 3; i++ {
		resp, err := client.Get(srv.URL + "/skills/1")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != `{"name":"Mystic Forge"}` {
			t.Fatalf("request %d body = %q", i, body)
		}
	}

	if calls != 1 {
		t.Errorf("origin server called %d times, want 1 (responses should be cached)", calls)
	}
}

func TestResolverHTTPClient_SetsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	resp, err := newTestResolverClient().Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = resp.Body.Close()
	if gotUA != resolverUserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, resolverUserAgent)
	}
}

func TestResolverHTTPClient_DoesNotCacheNon200(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"text":"not found"}`))
	}))
	defer srv.Close()

	client := newTestResolverClient()
	for i := 0; i < 2; i++ {
		resp, err := client.Get(srv.URL + "/items/0")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}
	if calls != 2 {
		t.Errorf("origin called %d times, want 2 (non-200 must not be cached)", calls)
	}
}
