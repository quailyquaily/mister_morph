package llmselect

import (
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
)

const (
	RouteTypeProfile    = "profile"
	RouteTypeCandidates = "candidates"
)

type ProfileInfo struct {
	Name              string
	Source            string
	InferenceProvider string
	Provider          string
	ModelName         string
	APIBase           string
}

type CandidateInfo struct {
	ProfileInfo
	Weight int
}

type SelectionView struct {
	Mode             string
	ManualProfile    string
	RouteType        string
	Current          *ProfileInfo
	Candidates       []CandidateInfo
	FallbackProfiles []ProfileInfo
}

func ListProfiles(values llmutil.RuntimeValues) ([]ProfileInfo, error) {
	profiles, err := llmutil.ListProfiles(values)
	if err != nil {
		return nil, err
	}
	out := make([]ProfileInfo, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, profileInfoFromResolved(profile.Name, profile.Source, profile.Values, profile.ClientConfig))
	}
	return out, nil
}

func ResolveMainRoute(values llmutil.RuntimeValues, selection MainSelection) (llmutil.ResolvedRoute, error) {
	selection = NormalizeSelection(selection)
	if selection.Mode == ModeManual {
		return llmutil.ResolveRouteWithProfileOverride(values, llmutil.RoutePurposeMainLoop, selection.ManualProfile)
	}
	return llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
}

func GetSelection(values llmutil.RuntimeValues, selection MainSelection) (SelectionView, error) {
	selection = NormalizeSelection(selection)
	view := SelectionView{
		Mode:          selection.Mode,
		ManualProfile: selection.ManualProfile,
	}
	route, err := ResolveMainRoute(values, selection)
	if err != nil {
		return SelectionView{}, err
	}
	if len(route.Candidates) > 0 {
		view.RouteType = RouteTypeCandidates
		view.Candidates = make([]CandidateInfo, 0, len(route.Candidates))
		for _, candidate := range route.Candidates {
			view.Candidates = append(view.Candidates, CandidateInfo{
				ProfileInfo: profileInfoFromResolved(candidate.Profile, candidate.Source, candidate.Values, candidate.ClientConfig),
				Weight:      candidate.Weight,
			})
		}
	} else {
		view.RouteType = RouteTypeProfile
		current := profileInfoFromResolved(route.Profile, route.Source, route.Values, route.ClientConfig)
		view.Current = &current
	}
	view.FallbackProfiles = fallbackInfos(route.Fallbacks)
	return view, nil
}

func ValidateProfile(values llmutil.RuntimeValues, profileName string) (ProfileInfo, error) {
	profile, err := llmutil.ResolveProfile(values, profileName)
	if err != nil {
		return ProfileInfo{}, err
	}
	return profileInfoFromResolved(profile.Name, profile.Source, profile.Values, profile.ClientConfig), nil
}

func RenderSelectionText(view SelectionView) string {
	lines := []string{fmt.Sprintf("Current LLM selection: %s", strings.TrimSpace(view.Mode))}
	if strings.TrimSpace(view.ManualProfile) != "" {
		lines = append(lines, "Manual profile: "+strings.TrimSpace(view.ManualProfile))
	}
	switch view.RouteType {
	case RouteTypeCandidates:
		lines = append(lines, "Main route uses weighted candidates:")
		for _, candidate := range view.Candidates {
			lines = append(lines, "- "+formatCandidate(candidate))
		}
	default:
		if view.Current != nil {
			lines = append(lines, "Active profile:")
			lines = append(lines, "- "+formatProfile(*view.Current))
		}
	}
	if len(view.FallbackProfiles) > 0 {
		lines = append(lines, "Fallback profiles:")
		for _, profile := range view.FallbackProfiles {
			lines = append(lines, "- "+formatProfile(profile))
		}
	}
	return strings.Join(lines, "\n")
}

func RenderProfilesText(profiles []ProfileInfo) string {
	lines := []string{"Available LLM profiles:"}
	for _, profile := range profiles {
		lines = append(lines, "- "+formatProfile(profile))
	}
	return strings.Join(lines, "\n")
}

func RenderSetText(profile ProfileInfo) string {
	return "Primary LLM profile set to " + formatProfile(profile)
}

func RenderResetText(view SelectionView) string {
	return "LLM profile selection reset.\n" + RenderSelectionText(view)
}

func UsageText() string {
	return "usage: /models | /models list | /models set <profile_name> | /models reset"
}

func fallbackInfos(fallbacks []llmutil.ResolvedFallback) []ProfileInfo {
	if len(fallbacks) == 0 {
		return nil
	}
	out := make([]ProfileInfo, 0, len(fallbacks))
	for _, fallback := range fallbacks {
		out = append(out, profileInfoFromResolved(fallback.Profile, fallback.Source, fallback.Values, fallback.ClientConfig))
	}
	return out
}

func profileInfoFromResolved(name string, source string, values llmutil.RuntimeValues, cfg llmconfig.ClientConfig) ProfileInfo {
	return ProfileInfo{
		Name:              strings.TrimSpace(name),
		Source:            strings.TrimSpace(source),
		InferenceProvider: strings.TrimSpace(values.InferenceProvider),
		Provider:          strings.TrimSpace(cfg.Provider),
		ModelName:         strings.TrimSpace(cfg.Model),
		APIBase:           strings.TrimSpace(cfg.Endpoint),
	}
}

func formatProfile(profile ProfileInfo) string {
	parts := []string{
		fmt.Sprintf("%s | inference_provider=%s | provider=%s | model_name=%s", profile.Name, emptyDash(profile.InferenceProvider), emptyDash(profile.Provider), emptyDash(profile.ModelName)),
	}
	if strings.TrimSpace(profile.Source) != "" {
		parts = append(parts, "source="+strings.TrimSpace(profile.Source))
	}
	if strings.TrimSpace(profile.APIBase) != "" {
		parts = append(parts, "api_base="+strings.TrimSpace(profile.APIBase))
	}
	return strings.Join(parts, " | ")
}

func formatCandidate(candidate CandidateInfo) string {
	return formatProfile(candidate.ProfileInfo) + fmt.Sprintf(" | weight=%d", candidate.Weight)
}

func emptyDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}
