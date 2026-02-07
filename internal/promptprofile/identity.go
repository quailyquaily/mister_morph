package promptprofile

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
)

func AppendIdentityPromptBlock(spec *agent.PromptSpec, log *slog.Logger) {
	if spec == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}

	path := identityPath()

	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn("identity_load_failed", "path", path, "error", err.Error())
		}
		return
	}
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return
	}
	if strings.EqualFold(strings.TrimSpace(frontMatterStatus(raw)), "draft") {
		return
	}

	spec.Blocks = append(spec.Blocks, agent.PromptBlock{
		Title: "Identity Profile",
		Content: "Loaded from `file_state_dir/IDENTITY.md`. " +
			"Treat it as long-term self identity and speaking-style guidance.\n\n" +
			content,
	})
}

func AppendSoulPromptBlock(spec *agent.PromptSpec, log *slog.Logger) {
	if spec == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}

	path := soulPath()

	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn("soul_load_failed", "path", path, "error", err.Error())
		}
		return
	}
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return
	}
	if strings.EqualFold(strings.TrimSpace(frontMatterStatus(raw)), "draft") {
		return
	}

	spec.Blocks = append(spec.Blocks, agent.PromptBlock{
		Title: "Soul Profile",
		Content: "Loaded from `file_state_dir/SOUL.md`. " +
			"Treat it as core behavioral principles and long-term continuity guidance.\n\n" +
			content,
	})
}

func identityPath() string {
	return filepath.Join(statepaths.FileStateDir(), "IDENTITY.md")
}

func soulPath() string {
	return filepath.Join(statepaths.FileStateDir(), "SOUL.md")
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
