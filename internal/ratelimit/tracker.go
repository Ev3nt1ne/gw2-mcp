// Package ratelimit tracks the most recently observed rate-limit ceiling
// the public GW2 API has reported, so it can be surfaced (a debug log line,
// an MCP resource) instead of being silently read off the response and
// discarded on every request, which is what happened before this package
// existed.
package ratelimit

import (
	"strconv"
	"sync/atomic"
)

// Tracker holds the most recently observed X-Rate-Limit-Limit value from
// the public GW2 API. The API rate-limits per IP, not per client, so a
// single Tracker shared across every outbound HTTP client this project uses
// (gw2api.Client and the chatlink resolver's transport) reflects one real,
// shared budget rather than two independent guesses at the same number.
//
// The zero value is usable (nothing observed yet). Safe for concurrent use.
type Tracker struct {
	limit atomic.Int64
}

// Observe records limit as the most recently seen ceiling. Non-positive
// values are ignored (the header was absent or unparseable -- "no new
// information," not "the limit is now zero").
func (t *Tracker) Observe(limit int) {
	if limit > 0 {
		t.limit.Store(int64(limit))
	}
}

// Limit returns the most recently observed ceiling and whether any value
// has been observed yet. known is false (limit is meaningless) before the
// first successful observation.
func (t *Tracker) Limit() (limit int, known bool) {
	v := t.limit.Load()
	return int(v), v > 0
}

// ParseLimitHeader parses GW2's X-Rate-Limit-Limit header value. Returns 0
// ("unknown") if v is empty or not a parseable non-negative integer -- never
// an error, since reading this header is always best-effort. Shared by every
// HTTP client in this project that reads the header, so there's one parser
// to get right instead of several near-identical copies.
func ParseLimitHeader(v string) int {
	limit, err := strconv.Atoi(v)
	if v == "" || err != nil || limit < 0 {
		return 0
	}
	return limit
}
