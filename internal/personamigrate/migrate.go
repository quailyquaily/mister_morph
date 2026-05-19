package personamigrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
	markdownutil "github.com/quailyquaily/mistermorph/internal/markdown"
	"github.com/quailyquaily/mistermorph/internal/onboardingcheck"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
)

type Result struct {
	IdentityMigrated bool
	SoulMigrated     bool
	Errors           []error
}

func (r Result) Err() error {
	if len(r.Errors) == 0 {
		return nil
	}
	return errors.Join(r.Errors...)
}

func Run(stateDir string) Result {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return Result{}
	}
	personaDir := filepath.Join(stateDir, statepaths.PersonaDirName)
	result := Result{}
	if err := fsstore.EnsureDir(personaDir, 0o700); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("ensure persona dir: %w", err))
		return result
	}
	if migrated, err := migrateIdentity(stateDir, personaDir); err != nil {
		result.Errors = append(result.Errors, err)
	} else {
		result.IdentityMigrated = migrated
	}
	if migrated, err := migrateSoul(stateDir, personaDir); err != nil {
		result.Errors = append(result.Errors, err)
	} else {
		result.SoulMigrated = migrated
	}
	return result
}

func migrateIdentity(stateDir string, personaDir string) (bool, error) {
	targetPath := filepath.Join(personaDir, statepaths.IdentityFilename)
	if fileExists(targetPath) {
		return false, nil
	}
	for _, sourcePath := range []string{
		filepath.Join(personaDir, statepaths.LegacyIdentityFilename),
		filepath.Join(stateDir, statepaths.LegacyIdentityFilename),
	} {
		raw, exists, err := readExistingFile(sourcePath)
		if err != nil {
			return false, fmt.Errorf("read legacy identity: %w", err)
		}
		if !exists {
			continue
		}
		yamlText, ok, err := extractIdentityYAML(raw)
		if err != nil {
			return false, fmt.Errorf("migrate identity: %w", err)
		}
		if !ok {
			return false, nil
		}
		if err := fsstore.WriteTextAtomic(targetPath, yamlText, fsstore.FileOptions{}); err != nil {
			return false, fmt.Errorf("write identity.yaml: %w", err)
		}
		return true, nil
	}
	return false, nil
}

func migrateSoul(stateDir string, personaDir string) (bool, error) {
	targetPath := filepath.Join(personaDir, statepaths.SoulFilename)
	if fileExists(targetPath) {
		return false, nil
	}
	for _, sourcePath := range []string{
		filepath.Join(personaDir, statepaths.LegacySoulFilename),
		filepath.Join(stateDir, statepaths.LegacySoulFilename),
	} {
		raw, exists, err := readExistingFile(sourcePath)
		if err != nil {
			return false, fmt.Errorf("read legacy soul: %w", err)
		}
		if !exists {
			continue
		}
		content, ok := extractSoulMarkdown(raw)
		if !ok {
			return false, nil
		}
		if err := onboardingcheck.ValidateSoulMarkdown(content); err != nil {
			return false, fmt.Errorf("migrate soul: %w", err)
		}
		if err := fsstore.WriteTextAtomic(targetPath, content, fsstore.FileOptions{}); err != nil {
			return false, fmt.Errorf("write soul.md: %w", err)
		}
		return true, nil
	}
	return false, nil
}

func readExistingFile(path string) ([]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return raw, true, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func extractIdentityYAML(raw []byte) (string, bool, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if strings.EqualFold(markdownutil.FrontmatterStatus(text), "draft") {
		return "", false, nil
	}
	content := strings.TrimSpace(markdownutil.StripFrontmatter(text))
	if content == "" {
		return "", false, nil
	}
	hasYAMLBlock := false
	if block := firstFencedYAMLBlock(content); strings.TrimSpace(block) != "" {
		content = block
		hasYAMLBlock = true
	}
	content = strings.TrimSpace(content)
	if err := onboardingcheck.ValidateIdentityYAML(content); err != nil {
		if !hasYAMLBlock && looksLikeLegacyIdentityMarkdown(content) {
			return "", false, nil
		}
		return "", false, err
	}
	return ensureTrailingNewline(content), true, nil
}

func extractSoulMarkdown(raw []byte) (string, bool) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if strings.EqualFold(markdownutil.FrontmatterStatus(text), "draft") {
		return "", false
	}
	content := strings.TrimSpace(markdownutil.StripFrontmatter(text))
	if content == "" {
		return "", false
	}
	return ensureTrailingNewline(content), true
}

func firstFencedYAMLBlock(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if start < 0 {
			if strings.HasPrefix(lower, "```yaml") || strings.HasPrefix(lower, "```yml") {
				start = i + 1
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			return strings.Join(lines[start:i], "\n")
		}
	}
	if start >= 0 && start < len(lines) {
		return strings.Join(lines[start:], "\n")
	}
	return ""
}

func ensureTrailingNewline(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if raw == "" {
		return ""
	}
	return raw + "\n"
}

func looksLikeLegacyIdentityMarkdown(raw string) bool {
	lower := strings.ToLower(strings.ReplaceAll(raw, "\r\n", "\n"))
	return strings.Contains(lower, "- **name:**") &&
		strings.Contains(lower, "- **creature:**") &&
		strings.Contains(lower, "- **vibe:**") &&
		strings.Contains(lower, "- **emoji:**")
}
