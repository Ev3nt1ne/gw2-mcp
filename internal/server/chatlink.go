package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	chatlinksapi "github.com/Ev3nt1ne/gw2-chatlinks-go/api"
	"github.com/Ev3nt1ne/gw2-chatlinks-go/chatlinks"
)

// buildTemplateSkillSlots names a build template's 10 skill palette slots,
// in the order chatlinks.BuildTemplate.SkillPaletteIDs stores them.
var buildTemplateSkillSlots = [10]string{
	"heal_terrestrial", "heal_aquatic",
	"util1_terrestrial", "util1_aquatic",
	"util2_terrestrial", "util2_aquatic",
	"util3_terrestrial", "util3_aquatic",
	"elite_terrestrial", "elite_aquatic",
}

type simpleIDLinkResult struct {
	Type     string `json:"type"`
	ID       int    `json:"id"`
	Quantity int    `json:"quantity,omitempty"`
	Name     string `json:"name,omitempty"`
	// ResolveWarnings collects non-fatal name-resolution failures when
	// resolve=true. The decode itself succeeded; only the optional name
	// lookup didn't, so the structured result is still returned.
	ResolveWarnings []string `json:"resolve_warnings,omitempty"`
}

type specializationResult struct {
	SpecializationID int    `json:"specialization_id"`
	Name             string `json:"name,omitempty"`
	Adept            int    `json:"adept"`
	Master           int    `json:"master"`
	Grandmaster      int    `json:"grandmaster"`
}

type skillSlotResult struct {
	Slot      string `json:"slot"`
	PaletteID int    `json:"palette_id"`
	SkillID   int    `json:"skill_id,omitempty"`
	Name      string `json:"name,omitempty"`
}

type rangerPetsResult struct {
	TerrestrialPet1 int `json:"terrestrial_pet_1"`
	TerrestrialPet2 int `json:"terrestrial_pet_2"`
	AquaticPet1     int `json:"aquatic_pet_1"`
	AquaticPet2     int `json:"aquatic_pet_2"`
}

type revenantLegendsResult struct {
	TerrestrialActive   string `json:"terrestrial_active,omitempty"`
	TerrestrialInactive string `json:"terrestrial_inactive,omitempty"`
	AquaticActive       string `json:"aquatic_active,omitempty"`
	AquaticInactive     string `json:"aquatic_inactive,omitempty"`
}

type weaponResult struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type skillOverrideResult struct {
	SkillID int    `json:"skill_id"`
	Name    string `json:"name,omitempty"`
}

type buildTemplateResult struct {
	Profession      string                  `json:"profession"`
	Specializations [3]specializationResult `json:"specializations"`
	Skills          []skillSlotResult       `json:"skills,omitempty"`
	RangerPets      *rangerPetsResult       `json:"ranger_pets,omitempty"`
	RevenantLegends *revenantLegendsResult  `json:"revenant_legends,omitempty"`
	Weapons         []weaponResult          `json:"weapons,omitempty"`
	SkillOverrides  []skillOverrideResult   `json:"skill_overrides,omitempty"`
	// ResolveWarnings collects non-fatal name-resolution failures when
	// resolve=true (see simpleIDLinkResult.ResolveWarnings).
	ResolveWarnings []string `json:"resolve_warnings,omitempty"`
}

// registerChatlinkTools registers the chatlink_decode/chatlink_encode tools.
func (s *MCPServer) registerChatlinkTools() {
	chatlinkDecodeTool := mcp.NewTool(
		"chatlink_decode",
		mcp.WithDescription("Decode a Guild Wars 2 chat link ([&...] code) into its structured contents "+
			"(build template, skill, trait, item, recipe, achievement, or map point of interest)"),
		mcp.WithString(
			"code",
			mcp.Required(),
			mcp.Description("The chat link code, e.g. \"[&AgEAAAA=]\""),
		),
		mcp.WithBoolean(
			"resolve",
			mcp.Description("Resolve IDs to human-readable names via the public GW2 API (default: false)"),
		),
	)
	s.mcp.AddTool(chatlinkDecodeTool, s.handleChatlinkDecode)

	chatlinkEncodeTool := mcp.NewTool(
		"chatlink_encode",
		mcp.WithDescription("Encode a Guild Wars 2 skill/trait/item/recipe/achievement/map ID into a "+
			"chat link ([&...] code). Build templates aren't supported here yet — only single-ID links"),
		mcp.WithString(
			"link_type",
			mcp.Required(),
			mcp.Enum("skill", "trait", "item", "recipe", "achievement", "map"),
			mcp.Description("The kind of link to produce"),
		),
		mcp.WithNumber(
			"id",
			mcp.Required(),
			mcp.Description("The public GW2 API ID for the skill/trait/item/recipe/achievement/map point"),
		),
		mcp.WithNumber(
			"quantity",
			mcp.Description("Stack size to encode (only meaningful for link_type \"item\"; default: 1)"),
		),
	)
	s.mcp.AddTool(chatlinkEncodeTool, s.handleChatlinkEncode)
}

// handleChatlinkDecode handles chat-link decode requests.
func (s *MCPServer) handleChatlinkDecode(
	ctx context.Context, request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	code, err := request.RequireString("code")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid code parameter: %v", err)), nil
	}
	resolve := request.GetBool("resolve", false)

	s.logger.Debug("Chat-link decode request", "resolve", resolve)

	raw, err := chatlinks.DecodeRaw(code)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to decode chat link: %v", err)), nil
	}

	linkType := chatlinks.HeaderType(raw)

	var result any
	switch linkType {
	case "build_template":
		result, err = s.decodeBuildTemplate(ctx, code, resolve)
	case "skill", "trait", "item", "recipe", "achievement", "map":
		result, err = s.decodeSimpleIDLink(ctx, code, linkType, resolve)
	default:
		return mcp.NewToolResultError(fmt.Sprintf("Decoding link type %q is not implemented", linkType)), nil
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to decode chat link: %v", err)), nil
	}

	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(resultJSON)), nil
}

func (s *MCPServer) decodeSimpleIDLink(
	ctx context.Context, code, linkType string, resolve bool,
) (*simpleIDLinkResult, error) {
	link, err := chatlinks.DecodeSimpleIDLink(code)
	if err != nil {
		return nil, err
	}
	result := &simpleIDLinkResult{Type: link.LinkType, ID: link.ID, Quantity: link.Quantity}
	if !resolve {
		return result, nil
	}

	var name string
	switch linkType {
	case "skill":
		name, err = s.chatlinks.ResolveSkillName(ctx, link.ID)
	case "trait":
		name, err = s.chatlinks.ResolveTraitName(ctx, link.ID)
	case "item":
		name, err = s.chatlinks.ResolveItemName(ctx, link.ID)
	default:
		result.ResolveWarnings = append(result.ResolveWarnings,
			fmt.Sprintf("name resolution is not available for link type %q", linkType))
		return result, nil
	}
	// Resolution is best-effort: a lookup failure (404, removed id, API blip)
	// must not discard the successful decode, which is the actual answer.
	if err != nil {
		result.ResolveWarnings = append(result.ResolveWarnings,
			fmt.Sprintf("could not resolve %s name for id %d: %v", linkType, link.ID, err))
		return result, nil
	}
	result.Name = name
	return result, nil
}

func (s *MCPServer) decodeBuildTemplate(ctx context.Context, code string, resolve bool) (*buildTemplateResult, error) {
	bt, err := chatlinks.DecodeBuildTemplate(code)
	if err != nil {
		return nil, err
	}

	result := &buildTemplateResult{Profession: bt.Profession}

	// Resolve all of the build's API-backed names (skills + specializations)
	// in at most three batched requests via the library orchestrator, instead
	// of the ~25 sequential single-ID calls this used to make. Decode is the
	// contract: resolution is best-effort, so a failure records a warning and
	// we carry on with whatever resolved (see S3). The orchestrator's lookups
	// are independent, so a single aggregate warning here (the joined error,
	// which names the failing endpoint) replaces the old per-ID warnings;
	// IDs the API simply doesn't recognize come back absent from the maps and
	// are not warned about, matching the previous not-found behavior.
	var resolved chatlinksapi.ResolvedBuildTemplate
	if resolve {
		var resolveErr error
		resolved, resolveErr = s.chatlinks.ResolveBuildTemplate(ctx, bt)
		if resolveErr != nil {
			result.ResolveWarnings = append(result.ResolveWarnings,
				fmt.Sprintf("some names could not be resolved: %v", resolveErr))
		}
	}

	result.setSpecializations(bt.Specializations, resolve, resolved.SpecializationNames)
	result.setSkills(bt, resolve, resolved.PaletteToSkillID, resolved.SkillNames)
	result.setRangerPets(bt.RangerPets)
	result.setRevenantLegends(bt.RevenantLegends)
	result.setWeapons(bt.WeaponIDs)
	result.setSkillOverrides(bt.SkillOverrideIDs, resolve, resolved.SkillNames)

	return result, nil
}

func (r *buildTemplateResult) setSpecializations(
	specs [3]chatlinks.SpecializationChoice, resolve bool, names map[int]string,
) {
	for i, spec := range specs {
		sr := specializationResult{
			SpecializationID: spec.SpecializationID,
			Adept:            spec.Adept,
			Master:           spec.Master,
			Grandmaster:      spec.Grandmaster,
		}
		if resolve && spec.SpecializationID != 0 {
			sr.Name = names[spec.SpecializationID] // empty if unresolved
		}
		r.Specializations[i] = sr
	}
}

func (r *buildTemplateResult) setSkills(
	bt chatlinks.BuildTemplate, resolve bool, paletteToSkill map[int]int, skillNames map[int]string,
) {
	for i, paletteID := range bt.SkillPaletteIDs {
		if i >= len(buildTemplateSkillSlots) {
			// Defensive: the dependency's SkillPaletteIDs is length-10 today,
			// matching buildTemplateSkillSlots. Guard against a future length
			// change rather than index out of range.
			break
		}
		if paletteID == 0 {
			continue
		}
		slot := skillSlotResult{Slot: buildTemplateSkillSlots[i], PaletteID: paletteID}
		if resolve {
			if skillID, ok := paletteToSkill[paletteID]; ok {
				slot.SkillID = skillID
				slot.Name = skillNames[skillID] // empty if unresolved
			}
		}
		r.Skills = append(r.Skills, slot)
	}
}

func (r *buildTemplateResult) setRangerPets(pets *chatlinks.RangerPets) {
	if pets == nil {
		return
	}
	r.RangerPets = &rangerPetsResult{
		TerrestrialPet1: pets.TerrestrialPet1,
		TerrestrialPet2: pets.TerrestrialPet2,
		AquaticPet1:     pets.AquaticPet1,
		AquaticPet2:     pets.AquaticPet2,
	}
}

func (r *buildTemplateResult) setRevenantLegends(legends *chatlinks.RevenantLegends) {
	if legends == nil {
		return
	}
	r.RevenantLegends = &revenantLegendsResult{
		TerrestrialActive:   legendName(legends.TerrestrialActive),
		TerrestrialInactive: legendName(legends.TerrestrialInactive),
		AquaticActive:       legendName(legends.AquaticActive),
		AquaticInactive:     legendName(legends.AquaticInactive),
	}
}

func legendName(code int) string {
	if code == 0 {
		return ""
	}
	if name, ok := chatlinks.LegendName(code); ok {
		return name
	}
	return fmt.Sprintf("unknown(%d)", code)
}

func (r *buildTemplateResult) setWeapons(weaponIDs []int) {
	for _, weaponID := range weaponIDs {
		name, ok := chatlinks.WeaponTypeName(weaponID)
		if !ok {
			name = fmt.Sprintf("unknown(%d)", weaponID)
		}
		r.Weapons = append(r.Weapons, weaponResult{ID: weaponID, Name: name})
	}
}

func (r *buildTemplateResult) setSkillOverrides(skillIDs []int, resolve bool, skillNames map[int]string) {
	for _, skillID := range skillIDs {
		so := skillOverrideResult{SkillID: skillID}
		if resolve {
			so.Name = skillNames[skillID] // empty if unresolved
		}
		r.SkillOverrides = append(r.SkillOverrides, so)
	}
}

// handleChatlinkEncode handles chat-link encode requests. Only single-ID
// links (skill/trait/item/recipe/achievement/map) are supported — build
// template encoding needs a much larger nested schema (specializations,
// skill palette IDs, weapons, profession-specific data) that isn't exposed
// here yet.
func (s *MCPServer) handleChatlinkEncode(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	linkType, err := request.RequireString("link_type")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid link_type parameter: %v", err)), nil
	}
	id, err := request.RequireInt("id")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid id parameter: %v", err)), nil
	}
	quantity := request.GetInt("quantity", 0)

	s.logger.Debug("Chat-link encode request", "link_type", linkType, "id", id)

	code, err := chatlinks.EncodeSimpleIDLink(chatlinks.SimpleIDLink{LinkType: linkType, ID: id, Quantity: quantity})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to encode chat link: %v", err)), nil
	}

	resultJSON, err := json.MarshalIndent(map[string]string{"code": code}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(resultJSON)), nil
}
