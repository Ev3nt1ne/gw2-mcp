package server

import (
	"reflect"
	"testing"
)

func sampleHierarchy() ([]achievementGroup, []achievementCategory) {
	categories := []achievementCategory{
		{ID: 1, Name: "Slayer", Achievements: []struct {
			ID int `json:"id"`
		}{{ID: 10}, {ID: 11}}},
		{ID: 2, Name: "Festival Hats", Achievements: []struct {
			ID int `json:"id"`
		}{{ID: 20}}},
		{ID: 3, Name: "Hero", Achievements: nil},
	}
	groups := []achievementGroup{
		{ID: "A", Name: "General", Categories: []int{1, 3}},
		{ID: "B", Name: "Festival", Categories: []int{2}},
	}
	return groups, categories
}

func TestSearchAchievementHierarchy_CategoryNameMatch(t *testing.T) {
	groups, categories := sampleHierarchy()
	res := searchAchievementHierarchy("slayer", groups, categories)

	if len(res.MatchedCategories) != 1 {
		t.Fatalf("matched categories = %d, want 1", len(res.MatchedCategories))
	}
	got := res.MatchedCategories[0]
	if got.ID != 1 || got.Name != "Slayer" {
		t.Errorf("matched category = %+v, want id 1 Slayer", got)
	}
	if !reflect.DeepEqual(got.AchievementIDs, []int{10, 11}) {
		t.Errorf("achievement ids = %v, want [10 11]", got.AchievementIDs)
	}
	if len(res.MatchedGroups) != 0 {
		t.Errorf("matched groups = %d, want 0", len(res.MatchedGroups))
	}
}

func TestSearchAchievementHierarchy_GroupNameMatchFansOutCategories(t *testing.T) {
	groups, categories := sampleHierarchy()
	res := searchAchievementHierarchy("festival", groups, categories)

	// "Festival" matches both the group "Festival" and the category
	// "Festival Hats".
	if len(res.MatchedGroups) != 1 {
		t.Fatalf("matched groups = %d, want 1", len(res.MatchedGroups))
	}
	g := res.MatchedGroups[0]
	if g.ID != "B" || g.Name != "Festival" {
		t.Errorf("matched group = %+v, want id B Festival", g)
	}
	if len(g.Categories) != 1 || g.Categories[0].ID != 2 {
		t.Errorf("group categories = %+v, want category id 2", g.Categories)
	}
	if len(res.MatchedCategories) != 1 || res.MatchedCategories[0].ID != 2 {
		t.Errorf("matched categories = %+v, want category id 2", res.MatchedCategories)
	}
}

func TestSearchAchievementHierarchy_CaseInsensitive(t *testing.T) {
	groups, categories := sampleHierarchy()
	res := searchAchievementHierarchy("HeRo", groups, categories)
	if len(res.MatchedCategories) != 1 || res.MatchedCategories[0].ID != 3 {
		t.Fatalf("matched categories = %+v, want category id 3", res.MatchedCategories)
	}
}

func TestSearchAchievementHierarchy_NoMatch(t *testing.T) {
	groups, categories := sampleHierarchy()
	res := searchAchievementHierarchy("nonexistent", groups, categories)
	if len(res.MatchedCategories) != 0 || len(res.MatchedGroups) != 0 {
		t.Errorf("expected no matches, got %+v", res)
	}
}

func TestToCategoryMatch_EmptyAchievements(t *testing.T) {
	c := achievementCategory{ID: 3, Name: "Hero"}
	got := toCategoryMatch(c)
	if got.ID != 3 || got.Name != "Hero" {
		t.Errorf("got %+v, want id 3 Hero", got)
	}
	if len(got.AchievementIDs) != 0 {
		t.Errorf("achievement ids = %v, want empty", got.AchievementIDs)
	}
}
