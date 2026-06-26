package gw2api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ErrRateLimited is the sentinel a *RateLimitError wraps, for callers that
// only want to check "was this a 429" via errors.Is without inspecting
// RetryAfter/Limit.
var ErrRateLimited = errors.New("gw2api: rate limited (HTTP 429)")

// RateLimitError carries details of an HTTP 429 response from the public GW2
// API. RetryAfter/Limit are 0 ("unknown") when the corresponding response
// header was absent or unparseable -- never an error, since this is
// best-effort.
//
// This mirrors gw2-chatlinks-go's api.RateLimitError (a separate dependency
// this project also uses, for chat-link name resolution) in shape, but is
// its own type: gw2api is otherwise independent of that dependency, and
// reusing its type here would create a coupling between two unrelated
// subsystems for no real benefit.
type RateLimitError struct {
	URL        string
	RetryAfter time.Duration
	Limit      int
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("gw2api: %s rate limited (HTTP 429), retry after %s", e.URL, e.RetryAfter)
	}
	return fmt.Sprintf("gw2api: %s rate limited (HTTP 429)", e.URL)
}

func (e *RateLimitError) Unwrap() error {
	return ErrRateLimited
}

// rateLimitErrorFromResponse builds a *RateLimitError from a 429 response.
func rateLimitErrorFromResponse(url string, resp *http.Response) error {
	return &RateLimitError{
		URL:        url,
		RetryAfter: parseRetryAfterHeader(resp.Header.Get("Retry-After")),
		Limit:      parseRateLimitHeader(resp.Header.Get("X-Rate-Limit-Limit")),
	}
}

// parseRetryAfterHeader parses GW2's Retry-After header, sent as an integer
// number of seconds. Returns 0 ("unknown") if absent or not a parseable
// non-negative integer.
func parseRetryAfterHeader(v string) time.Duration {
	seconds, err := strconv.Atoi(v)
	if v == "" || err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// parseRateLimitHeader parses GW2's X-Rate-Limit-Limit header. Returns 0
// ("unknown") if absent or not a parseable non-negative integer.
func parseRateLimitHeader(v string) int {
	limit, err := strconv.Atoi(v)
	if v == "" || err != nil || limit < 0 {
		return 0
	}
	return limit
}
