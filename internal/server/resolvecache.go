package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/charmbracelet/log"

	"github.com/AlyxPink/gw2-mcp/internal/cache"
	"github.com/AlyxPink/gw2-mcp/internal/ratelimit"
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
//
// tracker receives the X-Rate-Limit-Limit value observed on every live
// response (cache hits never reach the real API, so there's nothing new to
// observe for those) -- pass the same *ratelimit.Tracker given to
// gw2api.NewClient for one shared view of the per-IP budget.
func newResolverHTTPClient(cacheManager *cache.Manager, logger *log.Logger, tracker *ratelimit.Tracker) *http.Client {
	return &http.Client{
		Timeout: resolverTimeout,
		Transport: &cachingTransport{
			base:             http.DefaultTransport,
			cache:            cacheManager,
			logger:           logger,
			rateLimitTracker: tracker,
		},
	}
}

// cachingTransport is an http.RoundTripper that caches successful JSON GET
// responses keyed by request URL.
type cachingTransport struct {
	base             http.RoundTripper
	cache            *cache.Manager
	logger           *log.Logger
	rateLimitTracker *ratelimit.Tracker
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

	// The API sends X-Rate-Limit-Limit on every live response, not just
	// 429s. Read-and-discard it like every other header would otherwise
	// leave no way to see the live ceiling before actually getting rate
	// limited.
	if limit := ratelimit.ParseLimitHeader(resp.Header.Get("X-Rate-Limit-Limit")); limit > 0 {
		t.rateLimitTracker.Observe(limit)
		t.logger.Debug("Observed GW2 API rate limit ceiling", "limit", limit, "url", req.URL.String())
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
		Header:        http.Header{"Content-Type": []string{jsonMIMEType}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
