package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/charmbracelet/log"

	"github.com/AlyxPink/gw2-mcp/internal/cache"
)

// resolverUserAgent identifies this server's outbound public-API requests
// made by the chatlink resolver.
const resolverUserAgent = "github.com/AlyxPink/gw2-mcp"

// resolverTimeout bounds a single resolver HTTP request.
const resolverTimeout = 30 * time.Second

// rawHTTPCachePrefix namespaces cached resolver HTTP responses, keyed by URL.
const rawHTTPCachePrefix = "gw2api:http:"

// newResolverHTTPClient returns an *http.Client whose transport caches
// successful public GW2 API GET responses in the shared cache.Manager.
//
// It's used to back the chatlink resolver
// (github.com/Ev3nt1ne/gw2-chatlinks-go/api.Client), whose id->name lookups
// would otherwise hit http.DefaultClient uncached and bypass this project's
// "Smart Caching" entirely. Going through this client gives those lookups a
// consistent User-Agent, timeout, connection reuse, and the same cache as the
// rest of the server. The resolved data (skills/items/specializations/
// professions) is static, so responses are cached under StaticDataTTL.
func newResolverHTTPClient(cacheManager *cache.Manager, logger *log.Logger) *http.Client {
	return &http.Client{
		Timeout: resolverTimeout,
		Transport: &cachingTransport{
			base:   http.DefaultTransport,
			cache:  cacheManager,
			logger: logger,
		},
	}
}

// cachingTransport is an http.RoundTripper that caches successful JSON GET
// responses keyed by request URL.
type cachingTransport struct {
	base   http.RoundTripper
	cache  *cache.Manager
	logger *log.Logger
}

func (t *cachingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", resolverUserAgent)
	}

	// Only idempotent GETs are cacheable; anything else passes straight through.
	if req.Method != http.MethodGet {
		return t.base.RoundTrip(req)
	}

	cacheKey := rawHTTPCachePrefix + req.URL.String()
	var cached json.RawMessage
	if t.cache.GetJSON(cacheKey, &cached) {
		t.logger.Debug("Resolver HTTP cache hit", "url", req.URL.String())
		return newJSONResponse(req, cached), nil
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// Don't cache (or consume) non-200s — let the caller see the real status.
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}

	body, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.logger.Warn("Failed to close response body", "error", closeErr)
	}
	if err != nil {
		return nil, err
	}

	if json.Valid(body) {
		// Cache as json.RawMessage so SetJSON stores the bytes verbatim;
		// a plain []byte would be base64-encoded into a JSON string instead.
		if err := t.cache.SetJSON(cacheKey, json.RawMessage(body), cache.StaticDataTTL); err != nil {
			t.logger.Warn("Failed to cache resolver response", "url", req.URL.String(), "error", err)
		}
	}

	// The body was consumed to cache it; hand the caller a fresh reader.
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp, nil
}

// newJSONResponse builds a minimal 200 OK JSON response for a cache hit.
func newJSONResponse(req *http.Request, body []byte) *http.Response {
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
