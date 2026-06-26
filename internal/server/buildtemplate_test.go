package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Ev3nt1ne/gw2-chatlinks-go/chatlinks"
)

func mustEncodeBuild(t *testing.T, bt chatlinks.BuildTemplate) string {
	t.Helper()
	code, err := chatlinks.EncodeBuildTemplate(bt)
	if err != nil {
		t.Fatalf("encode build template: %v", err)
	}
	return code
}

func skillSlotByName(res *buildTemplateResult, slot string) (skillSlotResult, bool) {
	for _, s := range res.Skills {
		if s.Slot == slot {
			return s, true
		}
	}
	return skillSlotResult{}, false
}

// TestDecodeBuildTemplate_ResolveUsesBatchedRequests proves the resolve path
// now goes through the library orchestrator: a fully-loaded build resolves in
// at most 3 requests (1 profession + 1 batched skills + 1 batched specs)
// rather than the ~25 sequential single-ID calls it made before.
func TestDecodeBuildTemplate_ResolveUsesBatchedRequests(t *testing.T) {
	var reqCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		switch r.URL.Path {
		case "/professions/Thief":
			_, _ = w.Write([]byte(`{"skills_by_palette": [[100,1001],[200,1002],[300,1003]]}`))
		case "/skills":
			_, _ = w.Write([]byte(`[{"id":1001,"name":"Heal"},{"id":1002,"name":"Util"},` +
				`{"id":1003,"name":"Elite"},{"id":400,"name":"Override"}]`))
		case "/specializations":
			_, _ = w.Write([]byte(`[{"id":7,"name":"Acrobatics"}]`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	s := newResolveTestServer(t, srv.URL)
	code := mustEncodeBuild(t, chatlinks.BuildTemplate{
		ProfessionID:     5, // Thief
		SkillPaletteIDs:  [10]int{100, 100, 200, 0, 0, 0, 0, 0, 0, 300},
		SkillOverrideIDs: []int{400},
		Specializations:  [3]chatlinks.SpecializationChoice{{SpecializationID: 7}},
	})

	res, err := s.decodeBuildTemplate(context.Background(), code, true)
	if err != nil {
		t.Fatalf("decodeBuildTemplate: %v", err)
	}
	if n := atomic.LoadInt32(&reqCount); n > 3 {
		t.Errorf("made %d requests, want <=3 (batched, not one per skill slot/override/spec)", n)
	}
	if len(res.ResolveWarnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.ResolveWarnings)
	}
	if heal, ok := skillSlotByName(res, "heal_terrestrial"); !ok || heal.SkillID != 1001 || heal.Name != "Heal" {
		t.Errorf("heal_terrestrial = %+v (ok=%v), want skill 1001 \"Heal\"", heal, ok)
	}
	if len(res.SkillOverrides) != 1 || res.SkillOverrides[0].Name != "Override" {
		t.Errorf("skill overrides = %+v, want one named \"Override\"", res.SkillOverrides)
	}
	if res.Specializations[0].Name != "Acrobatics" {
		t.Errorf("spec[0].Name = %q, want \"Acrobatics\"", res.Specializations[0].Name)
	}
}

// TestDecodeBuildTemplate_ResolveFailureIsBestEffort confirms a failing
// resolve category neither fails the decode nor blanks out the categories
// that did resolve, and surfaces a single aggregate warning naming the
// failing endpoint (S3).
func TestDecodeBuildTemplate_ResolveFailureIsBestEffort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/professions/Thief":
			_, _ = w.Write([]byte(`{"skills_by_palette": [[100,1001]]}`))
		case "/skills":
			_, _ = w.Write([]byte(`[{"id":1001,"name":"Heal"}]`))
		case "/specializations":
			w.WriteHeader(http.StatusInternalServerError) // this category fails
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	s := newResolveTestServer(t, srv.URL)
	code := mustEncodeBuild(t, chatlinks.BuildTemplate{
		ProfessionID:    5,
		SkillPaletteIDs: [10]int{100, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Specializations: [3]chatlinks.SpecializationChoice{{SpecializationID: 7}},
	})

	res, err := s.decodeBuildTemplate(context.Background(), code, true)
	if err != nil {
		t.Fatalf("decode must succeed even when resolution partly fails: %v", err)
	}
	if heal, ok := skillSlotByName(res, "heal_terrestrial"); !ok || heal.Name != "Heal" {
		t.Errorf("skill should still resolve despite the spec failure, got %+v (ok=%v)", heal, ok)
	}
	if res.Specializations[0].Name != "" {
		t.Errorf("failed spec lookup should leave Name empty, got %q", res.Specializations[0].Name)
	}
	var warned bool
	for _, w := range res.ResolveWarnings {
		if strings.Contains(w, "specializations") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected an aggregate warning naming the failing specializations endpoint, got: %v", res.ResolveWarnings)
	}
}

// TestDecodeBuildTemplate_NoResolveSkipsNetwork confirms resolve=false makes
// no API calls at all.
func TestDecodeBuildTemplate_NoResolveSkipsNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("resolve=false must not call the API, but got %s", r.URL.Path)
	}))
	defer srv.Close()

	s := newResolveTestServer(t, srv.URL)
	code := mustEncodeBuild(t, chatlinks.BuildTemplate{
		ProfessionID:    5,
		SkillPaletteIDs: [10]int{100, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Specializations: [3]chatlinks.SpecializationChoice{{SpecializationID: 7}},
	})

	res, err := s.decodeBuildTemplate(context.Background(), code, false)
	if err != nil {
		t.Fatalf("decodeBuildTemplate: %v", err)
	}
	if heal, ok := skillSlotByName(res, "heal_terrestrial"); !ok || heal.Name != "" || heal.SkillID != 0 {
		t.Errorf("resolve=false should leave names/skill ids unset, got %+v (ok=%v)", heal, ok)
	}
}
