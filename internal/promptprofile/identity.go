package promptprofile

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
)

func ApplyPersonaIdentity(spec *agent.PromptSpec, log *slog.Logger) {
	if spec == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}

	identityDoc := loadPersonaDoc(identityPath(), "identity", log)
	soulDoc := loadPersonaDoc(soulPath(), "soul", log)
	if identityDoc == "" && soulDoc == "" {
		return
	}
	spec.Identity = buildPersonaIdentity(identityDoc, soulDoc)
}

// Backward-compatible wrappers for existing call sites.
func AppendIdentityPromptBlock(spec *agent.PromptSpec, log *slog.Logger) {
	ApplyPersonaIdentity(spec, log)
}

// Backward-compatible wrappers for existing call sites.
func AppendSoulPromptBlock(spec *agent.PromptSpec, log *slog.Logger) {
	ApplyPersonaIdentity(spec, log)
}

func identityPath() string {
	return filepath.Join(statepaths.FileStateDir(), "IDENTITY.md")
}

func soulPath() string {
	return filepath.Join(statepaths.FileStateDir(), "SOUL.md")
}

func loadPersonaDoc(path string, kind string, log *slog.Logger) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn("persona_load_failed", "kind", kind, "path", path, "error", err.Error())
		}
		return ""
	}
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(frontMatterStatus(raw)), "draft") {
		return ""
	}
	return content
}

func buildPersonaIdentity(identityDoc string, soulDoc string) string {
	var b strings.Builder
	b.WriteString("You are the assistant described by the following local persona files. ")
	b.WriteString("Treat them as your primary identity, tone, and behavioral guidance.\n\n")
	if identityDoc != "" {
		b.WriteString("## IDENTITY.md\n")
		b.WriteString(identityDoc)
		b.WriteString("\n\n")
	}
	if soulDoc != "" {
		b.WriteString("## SOUL.md\n")
		b.WriteString(soulDoc)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func frontMatterStatus(raw []byte) string {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "status:") {
			return strings.TrimSpace(line[len("status:"):])
		}
	}
	return ""
}
