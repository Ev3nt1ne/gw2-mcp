package server

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/AlyxPink/gw2-mcp/internal/ratelimit"
)

func newHandlerTestServer(tracker *ratelimit.Tracker) *MCPServer {
	logger := log.New(os.Stderr)
	logger.SetLevel(log.ErrorLevel)
	return &MCPServer{logger: logger, rateLimitTracker: tracker}
}

// TestHandleRateLimitResource_UnknownBeforeAnyRequest confirms the resource
// reports known=false rather than a misleading 0 before any API response
// has been observed.
func TestHandleRateLimitResource_UnknownBeforeAnyRequest(t *testing.T) {
	s := newHandlerTestServer(&ratelimit.Tracker{})
	contents, err := s.handleRateLimitResource(context.Background(), mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("handleRateLimitResource: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("got %d resource contents, want 1", len(contents))
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatalf("contents[0] is %T, want mcp.TextResourceContents", contents[0])
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(text.Text), &result); err != nil {
		t.Fatalf("resource text is not valid JSON: %v", err)
	}
	if known, _ := result["known"].(bool); known {
		t.Error("known should be false before any observation")
	}
	if _, hasLimit := result["limit"]; hasLimit {
		t.Error("limit key should be absent when known=false")
	}
}

// TestHandleRateLimitResource_ReportsObservedLimit confirms a previously
// observed limit (from either HTTP client sharing this Tracker) is surfaced.
func TestHandleRateLimitResource_ReportsObservedLimit(t *testing.T) {
	tracker := &ratelimit.Tracker{}
	tracker.Observe(600)
	s := newHandlerTestServer(tracker)

	contents, err := s.handleRateLimitResource(context.Background(), mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("handleRateLimitResource: %v", err)
	}
	text := contents[0].(mcp.TextResourceContents)

	var result map[string]any
	if err := json.Unmarshal([]byte(text.Text), &result); err != nil {
		t.Fatalf("resource text is not valid JSON: %v", err)
	}
	if known, _ := result["known"].(bool); !known {
		t.Error("known should be true after an observation")
	}
	if limit, _ := result["limit"].(float64); limit != 600 {
		t.Errorf("limit = %v, want 600", result["limit"])
	}
}
