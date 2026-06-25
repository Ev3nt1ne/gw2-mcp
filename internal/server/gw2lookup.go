package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/AlyxPink/gw2-mcp/internal/gw2api"
)

// registerGw2LookupTool registers the gw2_lookup tool.
func (s *MCPServer) registerGw2LookupTool() {
	// Build the "no ids needed" list from the source of truth so the
	// description can't drift from the allow-list (e.g. it also covers
	// achievements/groups and achievements/categories, which achievement_search
	// relies on).
	wholeCollections := strings.Join(gw2api.WholeCollectionEndpointNames(), ", ")

	lookupTool := mcp.NewTool(
		"gw2_lookup",
		mcp.WithDescription("Look up Guild Wars 2 game data by ID from the public API ("+
			strings.Join(gw2api.QueryableEndpointNames(), ", ")+"). "+
			"No API key needed. Returns the raw API response. Most endpoints require explicit ids "+
			"(their collections are too large to fetch in full); these allow omitting ids to fetch "+
			"the whole (small) collection: "+wholeCollections),
		mcp.WithString(
			"endpoint",
			mcp.Required(),
			mcp.Enum(gw2api.QueryableEndpointNames()...),
			mcp.Description("Which /v2/ collection to query"),
		),
		mcp.WithArray(
			"ids",
			mcp.Description("Integer IDs to fetch. Required except for these whole-collection "+
				"endpoints, where omitting it returns everything: "+wholeCollections+". "+
				"Note: only integer ids are supported, so string-keyed collections "+
				"(e.g. achievements/groups GUIDs, named professions) are reachable only via the "+
				"no-ids whole-collection fetch"),
		),
	)
	s.mcp.AddTool(lookupTool, s.handleGw2Lookup)
}

func (s *MCPServer) handleGw2Lookup(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	endpoint, err := request.RequireString("endpoint")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid endpoint parameter: %v", err)), nil
	}
	ids := request.GetIntSlice("ids", nil)

	s.logger.Debug("GW2 API lookup request", "endpoint", endpoint, "ids", ids)

	result, err := s.gw2API.GetRaw(ctx, endpoint, ids)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to query %s: %v", endpoint, err)), nil
	}

	return mcp.NewToolResultText(strings.TrimSpace(string(result))), nil
}
