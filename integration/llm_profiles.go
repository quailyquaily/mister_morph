package integration

import (
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmselect"
)

var errRuntimeNil = fmt.Errorf("runtime is nil")

type LLMProfile struct {
	Name              string `json:"name"`
	Source            string `json:"source,omitempty"`
	InferenceProvider string `json:"inference_provider,omitempty"`
	Provider          string `json:"provider"`
	ModelName         string `json:"model_name"`
	APIBase           string `json:"api_base,omitempty"`
}

type LLMProfileCandidate struct {
	LLMProfile
	Weight int `json:"weight"`
}

type LLMProfileSelection struct {
	Mode             string                `json:"mode"`
	ManualProfile    string                `json:"manual_profile,omitempty"`
	RouteType        string                `json:"route_type"`
	Current          *LLMProfile           `json:"current,omitempty"`
	Candidates       []LLMProfileCandidate `json:"candidates,omitempty"`
	FallbackProfiles []LLMProfile          `json:"fallback_profiles,omitempty"`
}

func (rt *Runtime) GetLLMProfileSelection() (LLMProfileSelection, error) {
	if rt == nil {
		return LLMProfileSelection{}, errRuntimeNil
	}
	view, err := llmselect.GetSelection(rt.snapshot().LLMValues, rt.currentSelection())
	if err != nil {
		return LLMProfileSelection{}, err
	}
	return selectionFromView(view), nil
}

func (rt *Runtime) ListLLMProfiles() ([]LLMProfile, error) {
	if rt == nil {
		return nil, errRuntimeNil
	}
	profiles, err := llmselect.ListProfiles(rt.snapshot().LLMValues)
	if err != nil {
		return nil, err
	}
	out := make([]LLMProfile, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, profileFromInfo(profile))
	}
	return out, nil
}

func (rt *Runtime) SetLLMProfile(profileName string) error {
	if rt == nil {
		return errRuntimeNil
	}
	profileName = strings.TrimSpace(profileName)
	if _, err := llmselect.ValidateProfile(rt.snapshot().LLMValues, profileName); err != nil {
		return err
	}
	rt.selection.SetProfile(profileName)
	return nil
}

func (rt *Runtime) ResetLLMProfile() {
	if rt == nil || rt.selection == nil {
		return
	}
	rt.selection.Reset()
}

func (rt *Runtime) currentSelection() llmselect.MainSelection {
	if rt == nil || rt.selection == nil {
		return llmselect.MainSelection{Mode: llmselect.ModeAuto}
	}
	return rt.selection.Get()
}

func selectionFromView(view llmselect.SelectionView) LLMProfileSelection {
	out := LLMProfileSelection{
		Mode:          strings.TrimSpace(view.Mode),
		ManualProfile: strings.TrimSpace(view.ManualProfile),
		RouteType:     strings.TrimSpace(view.RouteType),
	}
	if view.Current != nil {
		current := profileFromInfo(*view.Current)
		out.Current = &current
	}
	if len(view.Candidates) > 0 {
		out.Candidates = make([]LLMProfileCandidate, 0, len(view.Candidates))
		for _, candidate := range view.Candidates {
			out.Candidates = append(out.Candidates, LLMProfileCandidate{
				LLMProfile: profileFromInfo(candidate.ProfileInfo),
				Weight:     candidate.Weight,
			})
		}
	}
	if len(view.FallbackProfiles) > 0 {
		out.FallbackProfiles = make([]LLMProfile, 0, len(view.FallbackProfiles))
		for _, profile := range view.FallbackProfiles {
			out.FallbackProfiles = append(out.FallbackProfiles, profileFromInfo(profile))
		}
	}
	return out
}

func profileFromInfo(info llmselect.ProfileInfo) LLMProfile {
	return LLMProfile{
		Name:              strings.TrimSpace(info.Name),
		Source:            strings.TrimSpace(info.Source),
		InferenceProvider: strings.TrimSpace(info.InferenceProvider),
		Provider:          strings.TrimSpace(info.Provider),
		ModelName:         strings.TrimSpace(info.ModelName),
		APIBase:           strings.TrimSpace(info.APIBase),
	}
}
