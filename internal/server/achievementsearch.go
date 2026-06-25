package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// achievementGroup mirrors one entry of /v2/achievements/groups.
type achievementGroup struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Categories []int  `json:"categories"`
}

// achievementCategory mirrors one entry of /v2/achievements/categories.
type achievementCategory struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Achievements []struct {
		ID int `json:"id"`
	} `json:"achievements"`
}

type achievementCategoryMatch struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	AchievementIDs []int  `json:"achievement_ids"`
}

type achievementGroupMatch struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Categories []achievementCategoryMatch `json:"categories,omitempty"`
}

type achievementSearchResult struct {
	MatchedGroups     []achievementGroupMatch    `json:"matched_groups,omitempty"`
	MatchedCategories []achievementCategoryMatch `json:"matched_categories,omitempty"`
}

// registerAchievementSearchTool registers the achievement_search tool.
func (s *MCPServer) registerAchievementSearchTool() {
	tool := mcp.NewTool(
		"achievement_search",
		mcp.WithDescription("Search Guild Wars 2 achievement groups/categories by name (case-insensitive "+
			"substring match). The public API has no text search, only lookup-by-ID, so this walks the "+
			"small groups/categories hierarchy (19 groups, 355 categories) instead of the full ~8,000-"+
			"achievement catalog. Matches broad themes (category/group names like \"Festival\" or "+
			"\"Slayer\"), not necessarily an individual achievement's exact title. Use gw2_lookup with "+
			"endpoint=achievements and the returned achievement_ids to get full achievement details"),
		mcp.WithString(
			"query",
			mcp.Required(),
			mcp.Description("Case-insensitive substring to match against achievement group/category names"),
		),
	)
	s.mcp.AddTool(tool, s.handleAchievementSearch)
}

func (s *MCPServer) handleAchievementSearch(
	ctx context.Context, request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	query, err := request.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid query parameter: %v", err)), nil
	}

	s.logger.Debug("Achievement search request", "query", query)

	groups, categories, err := s.fetchAchievementHierarchy(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := searchAchievementHierarchy(query, groups, categories)

	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(resultJSON)), nil
}

func (s *MCPServer) fetchAchievementHierarchy(
	ctx context.Context,
) ([]achievementGroup, []achievementCategory, error) {
	groupsRaw, err := s.gw2API.GetRaw(ctx, "achievements/groups", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch achievement groups: %w", err)
	}
	categoriesRaw, err := s.gw2API.GetRaw(ctx, "achievements/categories", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch achievement categories: %w", err)
	}

	var groups []achievementGroup
	if err := json.Unmarshal(groupsRaw, &groups); err != nil {
		return nil, nil, fmt.Errorf("failed to parse achievement groups: %w", err)
	}
	var categories []achievementCategory
	if err := json.Unmarshal(categoriesRaw, &categories); err != nil {
		return nil, nil, fmt.Errorf("failed to parse achievement categories: %w", err)
	}

	return groups, categories, nil
}

func searchAchievementHierarchy(
	query string, groups []achievementGroup, categories []achievementCategory,
) achievementSearchResult {
	categoryByID := make(map[int]achievementCategory, len(categories))
	for _, c := range categories {
		categoryByID[c.ID] = c
	}

	needle := strings.ToLower(query)
	var result achievementSearchResult

	for _, c := range categories {
		if strings.Contains(strings.ToLower(c.Name), needle) {
			result.MatchedCategories = append(result.MatchedCategories, toCategoryMatch(c))
		}
	}

	for _, g := range groups {
		if !strings.Contains(strings.ToLower(g.Name), needle) {
			continue
		}
		match := achievementGroupMatch{ID: g.ID, Name: g.Name}
		for _, catID := range g.Categories {
			if c, ok := categoryByID[catID]; ok {
				match.Categories = append(match.Categories, toCategoryMatch(c))
			}
		}
		result.MatchedGroups = append(result.MatchedGroups, match)
	}

	return result
}

func toCategoryMatch(c achievementCategory) achievementCategoryMatch {
	ids := make([]int, len(c.Achievements))
	for i, a := range c.Achievements {
		ids[i] = a.ID
	}
	return achievementCategoryMatch{ID: c.ID, Name: c.Name, AchievementIDs: ids}
}
