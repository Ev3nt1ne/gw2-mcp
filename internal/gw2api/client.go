// Package gw2api provides functionality for interacting with the Guild Wars 2 API.
package gw2api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"github.com/AlyxPink/gw2-mcp/internal/cache"
)

const (
	baseURL        = "https://api.guildwars2.com/v2"
	userAgent      = "github.com/AlyxPink/gw2-mcp"
	requestTimeout = 30 * time.Second

	// schemaVersion is passed on every GetRaw request. The unversioned
	// default schema gives some endpoints stale/legacy field shapes; see
	// the comment in GetRaw for a concrete example.
	schemaVersion = "latest"
)

// Client handles GW2 API requests
type Client struct {
	httpClient *http.Client
	cache      *cache.Manager
	logger     *log.Logger
	// apiBaseURL is the GW2 API root. Defaults to baseURL; overridable in
	// tests to point request building at an httptest server.
	apiBaseURL string
}

// WalletEntry represents a single currency in the wallet
type WalletEntry struct {
	ID    int `json:"id"`
	Value int `json:"value"`
}

// Currency represents currency metadata
type Currency struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	ID          int    `json:"id"`
	Order       int    `json:"order"`
}

// WalletInfo combines wallet entries with currency metadata
type WalletInfo struct {
	UpdatedAt  time.Time        `json:"updated_at"`
	Currencies map[int]Currency `json:"currencies"`
	Entries    []WalletEntry    `json:"entries"`
	Total      int              `json:"total_currencies"`
}

// NewClient creates a new GW2 API client
func NewClient(cacheManager *cache.Manager, logger *log.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		cache:      cacheManager,
		logger:     logger,
		apiBaseURL: baseURL,
	}
}

// queryableEndpoints are the public, keyless /v2/* collections GetRaw is
// allowed to proxy to. An explicit allow-list, not a passthrough of any
// caller-supplied path, so this can't be used to reach authenticated
// endpoints (/account/*, /characters/*, etc.) that need an API key this
// tool doesn't accept, or any path outside the public API's collections.
var queryableEndpoints = map[string]bool{
	"items":                   true,
	"skills":                  true,
	"traits":                  true,
	"specializations":         true,
	"recipes":                 true,
	"maps":                    true,
	"achievements":            true,
	"achievements/groups":     true,
	"achievements/categories": true,
	"colors":                  true,
	"legends":                 true,
	"professions":             true,
	"continents":              true,
}

// wholeCollectionEndpoints are the queryableEndpoints small enough (under
// a few hundred entries) that fetching the whole collection on an empty
// ids list is reasonable. Everything else requires explicit ids, since
// e.g. /v2/items has 70,000+ entries and "ids=all" would be a multi-MB
// response.
var wholeCollectionEndpoints = map[string]bool{
	"legends":                 true,
	"professions":             true,
	"continents":              true,
	"achievements/groups":     true, // 19 entries
	"achievements/categories": true, // 355 entries
}

// IsQueryableEndpoint reports whether endpoint is in GetRaw's allow-list.
func IsQueryableEndpoint(endpoint string) bool {
	return queryableEndpoints[endpoint]
}

// QueryableEndpointNames returns GetRaw's endpoint allow-list, sorted.
func QueryableEndpointNames() []string {
	names := make([]string, 0, len(queryableEndpoints))
	for name := range queryableEndpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RequiresIDs reports whether endpoint needs an explicit, non-empty ids
// list (true for every queryable endpoint except the handful small enough
// to fetch in full).
func RequiresIDs(endpoint string) bool {
	return queryableEndpoints[endpoint] && !wholeCollectionEndpoints[endpoint]
}

// WholeCollectionEndpointNames returns the endpoints that may be fetched in
// full without ids (i.e. RequiresIDs is false), sorted. Callers use this to
// describe the no-ids-allowed set without hardcoding it.
func WholeCollectionEndpointNames() []string {
	names := make([]string, 0, len(wholeCollectionEndpoints))
	for name := range wholeCollectionEndpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetRaw fetches one of the allow-listed public /v2/{endpoint} collections,
// optionally filtered to specific ids, and returns the raw JSON response
// unparsed — the schema varies per endpoint, so this intentionally doesn't
// maintain a typed Go struct for every one of them.
func (c *Client) GetRaw(ctx context.Context, endpoint string, ids []int) (json.RawMessage, error) {
	if !IsQueryableEndpoint(endpoint) {
		return nil, fmt.Errorf("endpoint %q is not in the allow-list", endpoint)
	}
	if len(ids) == 0 && RequiresIDs(endpoint) {
		return nil, fmt.Errorf("endpoint %q requires at least one id", endpoint)
	}

	idsParam := "all"
	if len(ids) > 0 {
		idStrs := make([]string, len(ids))
		for i, id := range ids {
			idStrs[i] = strconv.Itoa(id)
		}
		idsParam = strings.Join(idStrs, ",")
	}

	cacheKey := c.cache.GetRawQueryKey(endpoint, idsParam)
	var cached json.RawMessage
	if c.cache.GetJSON(cacheKey, &cached) {
		c.logger.Debug("Raw API query cache hit", "endpoint", endpoint, "ids", idsParam)
		return cached, nil
	}

	c.logger.Debug("Raw API query cache miss, fetching from API", "endpoint", endpoint, "ids", idsParam)

	// Always pass ids explicitly (defaulting to "all" above) - omitting
	// the ids param entirely changes the API's response shape to a bare
	// list of IDs instead of full detail objects, which is not what
	// "fetch the whole collection" is supposed to mean here. Also always
	// pass an explicit schema version: the unversioned default schema
	// gives some endpoints stale field shapes - e.g.
	// /v2/achievements/categories' "achievements" field is a bare array
	// of ints on the default schema, but [{"id": N}, ...] on "latest"
	// (confirmed empirically; same quirk gw2-chatlinks-go's api package
	// already documents for /v2/professions' skills_by_palette field).
	path := fmt.Sprintf("%s/%s?ids=%s&v=%s", c.apiBaseURL, endpoint, idsParam, schemaVersion)

	body, err := c.fetchRaw(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to query %s: %w", endpoint, err)
	}

	// Cache as json.RawMessage, not the plain []byte body - cache.SetJSON
	// calls json.Marshal on whatever it's given, and plain []byte doesn't
	// implement json.Marshaler, so it gets base64-encoded into a JSON
	// string instead of stored verbatim. json.RawMessage's MarshalJSON
	// returns its bytes as-is, which is what we actually want cached.
	if err := c.cache.SetJSON(cacheKey, json.RawMessage(body), cache.StaticDataTTL); err != nil {
		c.logger.Warn("Failed to cache raw API query", "endpoint", endpoint, "error", err)
	}

	return body, nil
}

// doGET issues a GET and returns the response body and status code. The body
// is always read in full (even on non-200) so callers can surface it in error
// messages; transport/read failures and HTTP 429 are the only cases that
// return a non-nil error here. A 429 returns *RateLimitError instead of
// (body, 429, nil) -- every caller below (GetRaw, GetWallet, GetCurrencies)
// gets consistent rate-limit handling without its own 429 special-casing,
// since each already checks `if err != nil` before `if status != http.StatusOK`.
// Callers that want RetryAfter/Limit can use errors.As; this package never
// retries automatically, so a hidden retry loop can't surprise a caller with
// unexpected latency. extraHeaders may be nil. A User-Agent is always set.
func (c *Client) doGET(ctx context.Context, url string, extraHeaders http.Header) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	for key, values := range extraHeaders {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warn("Failed to close response body", "error", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return body, resp.StatusCode, rateLimitErrorFromResponse(url, resp)
	}
	return body, resp.StatusCode, nil
}

// fetchRaw performs a GET request and returns the raw, validated-JSON
// response body.
func (c *Client) fetchRaw(ctx context.Context, url string) ([]byte, error) {
	body, status, err := c.doGET(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", status, string(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API returned non-JSON response")
	}
	return body, nil
}

// GetWallet retrieves wallet information for the given API key
func (c *Client) GetWallet(ctx context.Context, apiKey string) (*WalletInfo, error) {
	// Create a hash of the API key for caching (security)
	hash := sha256.Sum256([]byte(apiKey))
	apiKeyHash := fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes of hash

	cacheKey := c.cache.GetWalletKey(apiKeyHash)

	// Try to get from cache first
	var walletInfo WalletInfo
	if c.cache.GetJSON(cacheKey, &walletInfo) {
		c.logger.Debug("Wallet cache hit", "api_key_hash", apiKeyHash)
		return &walletInfo, nil
	}

	c.logger.Debug("Wallet cache miss, fetching from API", "api_key_hash", apiKeyHash)

	// Fetch wallet data from API
	walletEntries, err := c.fetchWallet(ctx, apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch wallet: %w", err)
	}

	// Get currency metadata for all currencies in wallet
	currencyIDs := make([]int, len(walletEntries))
	for i, entry := range walletEntries {
		currencyIDs[i] = entry.ID
	}

	currencies, err := c.GetCurrencies(ctx, currencyIDs)
	if err != nil {
		c.logger.Warn("Failed to get currency metadata", "error", err)
		// Continue without metadata
		currencies = make(map[int]Currency)
	}

	// Create wallet info
	walletInfo = WalletInfo{
		Entries:    walletEntries,
		Currencies: currencies,
		Total:      len(walletEntries),
		UpdatedAt:  time.Now(),
	}

	// Cache the result
	if err := c.cache.SetJSON(cacheKey, walletInfo, cache.WalletDataTTL); err != nil {
		c.logger.Warn("Failed to cache wallet data", "error", err)
	}

	return &walletInfo, nil
}

// GetCurrencies retrieves currency metadata
func (c *Client) GetCurrencies(ctx context.Context, ids []int) (map[int]Currency, error) {
	// If no specific IDs requested, get all currencies
	if len(ids) == 0 {
		return c.getAllCurrencies(ctx)
	}

	// Get specific currencies
	currencies := make(map[int]Currency)
	var missingIDs []int

	// Check cache for each currency
	for _, id := range ids {
		cacheKey := c.cache.GetCurrencyDetailKey(id)
		var currency Currency
		if c.cache.GetJSON(cacheKey, &currency) {
			currencies[id] = currency
		} else {
			missingIDs = append(missingIDs, id)
		}
	}

	// Fetch missing currencies from API
	if len(missingIDs) > 0 {
		fetchedCurrencies, err := c.fetchCurrencies(ctx, missingIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch currencies: %w", err)
		}

		// Add fetched currencies to result and cache
		for _, currency := range fetchedCurrencies {
			currencies[currency.ID] = currency
			cacheKey := c.cache.GetCurrencyDetailKey(currency.ID)
			if err := c.cache.SetJSON(cacheKey, currency, cache.StaticDataTTL); err != nil {
				c.logger.Warn("Failed to cache currency", "id", currency.ID, "error", err)
			}
		}
	}

	return currencies, nil
}

// getAllCurrencies retrieves all available currencies
func (c *Client) getAllCurrencies(ctx context.Context) (map[int]Currency, error) {
	cacheKey := c.cache.GetCurrencyListKey()

	// Try cache first
	var currencies map[int]Currency
	if c.cache.GetJSON(cacheKey, &currencies) {
		c.logger.Debug("Currency list cache hit")
		return currencies, nil
	}

	c.logger.Debug("Currency list cache miss, fetching from API")

	// Fetch all currency IDs first
	currencyIDs, err := c.fetchCurrencyIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch currency IDs: %w", err)
	}

	// Fetch all currency details
	currencyList, err := c.fetchCurrencies(ctx, currencyIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch currency details: %w", err)
	}

	// Convert to map
	currencies = make(map[int]Currency)
	for _, currency := range currencyList {
		currencies[currency.ID] = currency
	}

	// Cache the result
	if err := c.cache.SetJSON(cacheKey, currencies, cache.StaticDataTTL); err != nil {
		c.logger.Warn("Failed to cache currency list", "error", err)
	}

	return currencies, nil
}

// fetchWallet makes the actual API call to get wallet data
func (c *Client) fetchWallet(ctx context.Context, apiKey string) ([]WalletEntry, error) {
	headers := http.Header{"Authorization": []string{"Bearer " + apiKey}}
	body, status, err := c.doGET(ctx, c.apiBaseURL+"/account/wallet", headers)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", status, string(body))
	}

	var wallet []WalletEntry
	if err := json.Unmarshal(body, &wallet); err != nil {
		return nil, err
	}
	return wallet, nil
}

// fetchCurrencyIDs fetches all available currency IDs
func (c *Client) fetchCurrencyIDs(ctx context.Context) ([]int, error) {
	body, status, err := c.doGET(ctx, c.apiBaseURL+"/currencies", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", status, string(body))
	}

	var ids []int
	if err := json.Unmarshal(body, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// fetchCurrencies fetches currency details for specific IDs
func (c *Client) fetchCurrencies(ctx context.Context, ids []int) ([]Currency, error) {
	// Convert IDs to comma-separated string
	idStrs := make([]string, len(ids))
	for i, id := range ids {
		idStrs[i] = strconv.Itoa(id)
	}
	idsParam := strings.Join(idStrs, ",")

	body, status, err := c.doGET(ctx, c.apiBaseURL+"/currencies?ids="+idsParam, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", status, string(body))
	}

	var currencies []Currency
	if err := json.Unmarshal(body, &currencies); err != nil {
		return nil, err
	}
	return currencies, nil
}
