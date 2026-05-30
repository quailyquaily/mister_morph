package promptprofile

import (
	"log/slog"
	"os"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	markdownutil "github.com/quailyquaily/mistermorph/internal/markdown"
	"github.com/quailyquaily/mistermorph/internal/onboardingcheck"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
)

func ApplyPersonaIdentity(spec *agent.PromptSpec, log *slog.Logger) {
	if spec == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}

	identityDoc, identityLabel, identityStatus := loadFirstPersonaDoc(identityCandidates(), log)
	soulDoc, soulLabel, soulStatus := loadFirstPersonaDoc(soulCandidates(), log)
	if identityDoc == "" && soulDoc == "" {
		log.Debug("persona_identity_skipped", "identity_status", identityStatus, "soul_status", soulStatus)
		return
	}
	spec.Identity = buildPersonaIdentity(identityDoc, identityLabel, soulDoc, soulLabel)
	log.Info(
		"persona_identity_applied",
		"identity_loaded", identityDoc != "",
		"soul_loaded", soulDoc != "",
		"identity_status", identityStatus,
		"soul_status", soulStatus,
	)
}

type personaDocCandidate struct {
	Path  string
	Label string
	Kind  string
}

func identityCandidates() []personaDocCandidate {
	return []personaDocCandidate{
		{Path: statepaths.PersonaIdentityPath(), Label: statepaths.IdentityFilename, Kind: "identity_yaml"},
	}
}

func soulCandidates() []personaDocCandidate {
	return []personaDocCandidate{
		{Path: statepaths.PersonaSoulPath(), Label: statepaths.SoulFilename, Kind: "soul_markdown"},
	}
}

func loadFirstPersonaDoc(candidates []personaDocCandidate, log *slog.Logger) (string, string, string) {
	lastStatus := "missing"
	for _, candidate := range candidates {
		doc, status := loadPersonaDoc(candidate, log)
		if status == "missing" {
			continue
		}
		if doc != "" {
			return doc, candidate.Label, status
		}
		lastStatus = status
	}
	return "", "", lastStatus
}

func loadPersonaDoc(candidate personaDocCandidate, log *slog.Logger) (string, string) {
	path := candidate.Path
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) && log != nil {
			log.Warn("persona_load_failed", "kind", candidate.Kind, "path", path, "error", err.Error())
		}
		if os.IsNotExist(err) {
			return "", "missing"
		}
		return "", "error"
	}
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return "", "empty"
	}
	if candidate.Kind != "identity_yaml" && strings.EqualFold(markdownutil.FrontmatterStatus(string(raw)), "draft") {
		return "", "draft"
	}
	switch candidate.Kind {
	case "identity_yaml":
		if err := onboardingcheck.ValidateIdentityYAML(string(raw)); err != nil {
			if log != nil {
				log.Warn("persona_load_failed", "kind", candidate.Kind, "path", path, "error", err.Error())
			}
			return "", "malformed"
		}
	case "soul_markdown":
		if err := onboardingcheck.ValidateSoulMarkdown(string(raw)); err != nil {
			if log != nil {
				log.Warn("persona_load_failed", "kind", candidate.Kind, "path", path, "error", err.Error())
			}
			return "", "malformed"
		}
	}
	contentSource := string(raw)
	if candidate.Kind != "identity_yaml" {
		contentSource = markdownutil.StripFrontmatter(contentSource)
	}
	content = strings.TrimSpace(contentSource)
	if content == "" {
		return "", "empty"
	}
	return content, "loaded"
}

func buildPersonaIdentity(identityDoc string, identityLabel string, soulDoc string, soulLabel string) string {
	var b strings.Builder
	b.WriteString("You wake up from a dream and realize you are the person (not an AI assistant) described by the following local persona files. ")
	b.WriteString("Treat them as your primary identity, tone, and behavioral guidance\n\n")
	if identityDoc != "" {
		if identityLabel == "" {
			identityLabel = statepaths.IdentityFilename
		}
		b.WriteString("[")
		b.WriteString(identityLabel)
		b.WriteString("]\n")
		b.WriteString(identityDoc)
		b.WriteString("\n")
	}
	if soulDoc != "" {
		if soulLabel == "" {
			soulLabel = statepaths.SoulFilename
		}
		b.WriteString("[")
		b.WriteString(soulLabel)
		b.WriteString("]\n")
		b.WriteString(soulDoc)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
