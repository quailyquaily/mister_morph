package onboardingcheck

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configutil"
	markdownutil "github.com/quailyquaily/mistermorph/internal/markdown"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type Status string

const (
	StatusOK         Status = "ok"
	StatusMissing    Status = "missing"
	StatusUnreadable Status = "unreadable"
	StatusMalformed  Status = "malformed"
)

const (
	FileKeyConfig   = "config"
	FileKeyIdentity = "identity"
	FileKeySoul     = "soul"
)

type Item struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Stage  string `json:"stage"`
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (i Item) IsBroken() bool {
	return i.Status == StatusUnreadable || i.Status == StatusMalformed
}

func Check(configPath string, stateDir string) []Item {
	stateDir = strings.TrimSpace(stateDir)
	return []Item{
		InspectConfigPath(configPath),
		InspectIdentityYAMLPath(filepath.Join(stateDir, "persona", "identity.yaml")),
		InspectSoulPath(filepath.Join(stateDir, "persona", "soul.md")),
	}
}

func BrokenItems(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, item := range items {
		if item.IsBroken() {
			out = append(out, item)
		}
	}
	return out
}

func InspectConfigPath(path string) Item {
	item := baseItem(FileKeyConfig, "config.yaml", "llm", path)
	raw, err := os.ReadFile(item.Path)
	if err != nil {
		return itemForReadError(item, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		item.Status = StatusMalformed
		item.Error = "config.yaml is empty"
		return item
	}
	tmp := viper.New()
	if err := configutil.ReadExpandedConfig(tmp, item.Path, nil); err != nil {
		item.Status = StatusMalformed
		item.Error = fmt.Sprintf("invalid config yaml: %v", err)
		return item
	}
	item.Status = StatusOK
	return item
}

func InspectIdentityYAMLPath(path string) Item {
	item := baseItem(FileKeyIdentity, "identity.yaml", "persona", path)
	raw, err := os.ReadFile(item.Path)
	if err != nil {
		return itemForReadError(item, err)
	}
	if err := ValidateIdentityYAML(string(raw)); err != nil {
		item.Status = StatusMalformed
		item.Error = err.Error()
		return item
	}
	item.Status = StatusOK
	return item
}

func InspectSoulPath(path string) Item {
	item := baseItem(FileKeySoul, filepath.Base(strings.TrimSpace(path)), "soul", path)
	if item.Name == "." || item.Name == "" {
		item.Name = "soul.md"
	}
	raw, err := os.ReadFile(item.Path)
	if err != nil {
		return itemForReadError(item, err)
	}
	if err := ValidateSoulMarkdown(string(raw)); err != nil {
		item.Status = StatusMalformed
		item.Error = err.Error()
		return item
	}
	item.Status = StatusOK
	return item
}

func ValidateIdentityYAML(raw string) error {
	content := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if content == "" {
		return fmt.Errorf("identity.yaml is empty")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return fmt.Errorf("identity.yaml is invalid: %w", err)
	}
	root := &doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return fmt.Errorf("identity.yaml is empty")
		}
		root = doc.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("identity.yaml must be a mapping")
	}
	return nil
}

func ValidateSoulMarkdown(raw string) error {
	content := strings.ToLower(strings.TrimSpace(markdownutil.StripFrontmatter(strings.ReplaceAll(raw, "\r\n", "\n"))))
	if content == "" {
		return fmt.Errorf("soul.md is empty")
	}
	if !strings.Contains(content, "## core truths") {
		return fmt.Errorf("soul.md is missing the Core Truths section")
	}
	if !strings.Contains(content, "## boundaries") {
		return fmt.Errorf("soul.md is missing the Boundaries section")
	}
	if !strings.Contains(content, "## vibe") {
		return fmt.Errorf("soul.md is missing the Vibe section")
	}
	return nil
}

func baseItem(key string, name string, stage string, path string) Item {
	return Item{
		Key:    key,
		Name:   name,
		Path:   filepath.Clean(strings.TrimSpace(path)),
		Stage:  stage,
		Status: StatusMissing,
	}
}

func itemForReadError(item Item, err error) Item {
	if os.IsNotExist(err) {
		item.Status = StatusMissing
		return item
	}
	item.Status = StatusUnreadable
	item.Error = err.Error()
	return item
}
