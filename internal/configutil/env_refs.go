package configutil

import (
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type EnvRefStatus string

const (
	EnvRefSet     EnvRefStatus = "set"
	EnvRefEmpty   EnvRefStatus = "empty"
	EnvRefMissing EnvRefStatus = "missing"
)

type EnvRefCheck struct {
	Name   string
	Status EnvRefStatus
}

// InspectConfigEnvRefs reports whether environment variables referenced by
// YAML scalar values are set, empty, or missing. It never returns their values.
func InspectConfigEnvRefs(path string) ([]EnvRefCheck, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return nil, err
	}

	names := map[string]struct{}{}
	collectEnvRefNames(&node, names)
	sortedNames := make([]string, 0, len(names))
	for name := range names {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)

	checks := make([]EnvRefCheck, 0, len(sortedNames))
	for _, name := range sortedNames {
		value, ok := os.LookupEnv(name)
		status := EnvRefSet
		switch {
		case !ok:
			status = EnvRefMissing
		case strings.TrimSpace(value) == "":
			status = EnvRefEmpty
		}
		checks = append(checks, EnvRefCheck{Name: name, Status: status})
	}
	return checks, nil
}

func collectEnvRefNames(node *yaml.Node, names map[string]struct{}) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			collectEnvRefNames(child, names)
		}
	case yaml.MappingNode:
		for i := 1; i < len(node.Content); i += 2 {
			collectEnvRefNames(node.Content[i], names)
		}
	case yaml.ScalarNode:
		for _, match := range envVarRe.FindAllStringSubmatch(node.Value, -1) {
			if len(match) == 2 {
				names[match[1]] = struct{}{}
			}
		}
	}
}
