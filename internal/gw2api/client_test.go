package gw2api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/AlyxPink/gw2-mcp/internal/cache"
)

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	logger := log.New(os.Stderr)
	logger.SetLevel(log.ErrorLevel)
	c := NewClient(cache.NewManager(), logger)
	c.apiBaseURL = baseURL
	return c
}

func TestIsQueryableEndpoint(t *testing.T) {
	if !IsQueryableEndpoint("items") {
		t.Error("items should be queryable")
	}
	if IsQueryableEndpoint("account/wallet") {
		t.Error("account/wallet must not be queryable (authenticated endpoint)")
	}
	if IsQueryableEndpoint("../characters") {
		t.Error("arbitrary paths must not be queryable")
	}
}

func TestRequiresIDs(t *testing.T) {
	if !RequiresIDs("items") {
		t.Error("items (70k+ entries) should require ids")
	}
	if RequiresIDs("professions") {
		t.Error("professions is a whole-collection endpoint and must not require ids")
	}
	if RequiresIDs("achievements/categories") {
		t.Error("achievements/categories is a whole-collection endpoint and must not require ids")
	}
}

func TestQueryableEndpointNames_Sorted(t *testing.T) {
	names := QueryableEndpointNames()
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("QueryableEndpointNames not sorted: %v", names)
		}
	}
	if len(names) != len(queryableEndpoints) {
		t.Errorf("got %d names, want %d", len(names), len(queryableEndpoints))
	}
}

func TestWholeCollectionEndpointNames(t *testing.T) {
	names := WholeCollectionEndpointNames()
	for _, n := range names {
		if RequiresIDs(n) {
			t.Errorf("%q is reported as a whole-collection endpoint but RequiresIDs is true", n)
		}
	}
	if len(names) != len(wholeCollectionEndpoints) {
		t.Errorf("got %d names, want %d", len(names), len(wholeCollectionEndpoints))
	}
}

func TestGetRaw_RejectsNonAllowlistedEndpoint(t *testing.T) {
	c := newTestClient(t, "http://127.0.0.1:0") // must never be dialed
	_, err := c.GetRaw(context.Background(), "account/wallet", []int{1})
	if err == nil {
		t.Fatal("expected an error for a non-allow-listed endpoint")
	}
}

func TestGetRaw_RequiresIDsWhenNeeded(t *testing.T) {
	c := newTestClient(t, "http://127.0.0.1:0") // must never be dialed
	_, err := c.GetRaw(context.Background(), "items", nil)
	if err == nil {
		t.Fatal("expected an error when a large collection is queried without ids")
	}
}

func TestGetRaw_ExplicitIDsAndSchemaParam(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"name":"Mystic Coin"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	body, err := c.GetRaw(context.Background(), "items", []int{1})
	if err != nil {
		t.Fatalf("GetRaw: %v", err)
	}
	if gotQuery.Get("ids") != "1" {
		t.Errorf("ids param = %q, want %q", gotQuery.Get("ids"), "1")
	}
	if gotQuery.Get("v") != schemaVersion {
		t.Errorf("v param = %q, want %q", gotQuery.Get("v"), schemaVersion)
	}
	// Body must be the verbatim JSON array, not a quoted/base64 string.
	var parsed []map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body is not a JSON array: %v (body=%s)", err, body)
	}
}

func TestGetRaw_WholeCollectionUsesIDsAll(t *testing.T) {
	var gotIDs string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIDs = r.URL.Query().Get("ids")
		_, _ = w.Write([]byte(`[{"id":"Guardian"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if _, err := c.GetRaw(context.Background(), "professions", nil); err != nil {
		t.Fatalf("GetRaw: %v", err)
	}
	if gotIDs != "all" {
		t.Errorf("ids param = %q, want %q (omitting ids changes the API response shape)", gotIDs, "all")
	}
}

func TestGetRaw_CachesVerbatimJSON(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`[{"id":1,"name":"Mystic Coin"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	first, err := c.GetRaw(context.Background(), "items", []int{1})
	if err != nil {
		t.Fatalf("first GetRaw: %v", err)
	}
	second, err := c.GetRaw(context.Background(), "items", []int{1})
	if err != nil {
		t.Fatalf("second GetRaw: %v", err)
	}
	if calls != 1 {
		t.Errorf("server called %d times, want 1 (second call should hit the cache)", calls)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("cached body differs from fresh body:\n fresh=%s\ncached=%s", first, second)
	}
	// The cached value must round-trip as a JSON array, not a base64 string.
	var parsed []map[string]any
	if err := json.Unmarshal(second, &parsed); err != nil {
		t.Fatalf("cached body is not a JSON array: %v (body=%s)", err, second)
	}
}

func TestGetRaw_PropagatesNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"text":"not found"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if _, err := c.GetRaw(context.Background(), "items", []int{999999999}); err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
}
