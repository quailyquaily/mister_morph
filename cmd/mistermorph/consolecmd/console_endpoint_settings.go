package consolecmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"gopkg.in/yaml.v3"
)

type consoleEndpointSettingsPayload struct {
	OriginalName        string `json:"original_name,omitempty"`
	Name                string `json:"name"`
	URL                 string `json:"url"`
	AuthToken           string `json:"auth_token"`
	AuthTokenConfigured bool   `json:"auth_token_configured,omitempty"`
}

func consoleEndpointSettingsFromDocument(doc *yaml.Node) []consoleEndpointSettingsPayload {
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil
	}
	consoleNode := configbootstrap.FindMappingValue(root, "console")
	endpointsNode := configbootstrap.FindMappingValue(consoleNode, "endpoints")
	if endpointsNode == nil || endpointsNode.Kind != yaml.SequenceNode {
		return nil
	}
	items := make([]consoleEndpointSettingsPayload, 0, len(endpointsNode.Content))
	for _, node := range endpointsNode.Content {
		if node == nil || node.Kind != yaml.MappingNode {
			continue
		}
		name := mappingScalar(node, "name")
		if name == "" {
			continue
		}
		items = append(items, consoleEndpointSettingsPayload{
			OriginalName:        name,
			Name:                name,
			URL:                 mappingScalar(node, "url"),
			AuthTokenConfigured: mappingScalar(node, "auth_token") != "",
		})
	}
	return items
}

func prepareConsoleEndpointSecrets(ctx context.Context, endpoints []consoleEndpointSettingsPayload, store secref.OSStore) ([]string, error) {
	if store == nil {
		return nil, nil
	}
	type replacement struct {
		index int
		value string
	}
	created := make([]string, 0, len(endpoints))
	replacements := make([]replacement, 0, len(endpoints))
	for index := range endpoints {
		value := strings.TrimSpace(endpoints[index].AuthToken)
		if value == "" {
			continue
		}
		if _, ok := secref.ParseSingleRef(value); ok {
			continue
		}
		id, err := secref.NewOSSecretID()
		if err != nil {
			secref.DeleteOSSecrets(ctx, store, created)
			return nil, err
		}
		name := strings.TrimSpace(endpoints[index].Name)
		if err := store.Put(ctx, id, "console.endpoints."+name+".auth_token", []byte(value)); err != nil {
			secref.DeleteOSSecrets(ctx, store, created)
			return nil, err
		}
		created = append(created, id)
		replacements = append(replacements, replacement{index: index, value: secref.OSSecretRef(id)})
	}
	for _, replacement := range replacements {
		endpoints[replacement.index].AuthToken = replacement.value
	}
	return created, nil
}

func applyConsoleEndpointSettings(raw []byte, endpoints []consoleEndpointSettingsPayload) ([]byte, error) {
	doc, err := configbootstrap.LoadDocumentBytes(raw)
	if err != nil {
		return nil, err
	}
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil, err
	}
	consoleNode := configbootstrap.EnsureMappingValue(root, "console")
	currentNode := configbootstrap.FindMappingValue(consoleNode, "endpoints")
	current := map[string]*yaml.Node{}
	if currentNode != nil && currentNode.Kind == yaml.SequenceNode {
		for _, node := range currentNode.Content {
			if name := strings.ToLower(mappingScalar(node, "name")); name != "" {
				current[name] = node
			}
		}
	}

	seen := map[string]bool{}
	nextNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, endpoint := range endpoints {
		name := strings.TrimSpace(endpoint.Name)
		rawURL := strings.TrimSpace(endpoint.URL)
		if name == "" || rawURL == "" {
			return nil, fmt.Errorf("console endpoint name and URL are required")
		}
		parsed, err := url.Parse(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, fmt.Errorf("console endpoint %q has an invalid URL", name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("duplicate console endpoint %q", name)
		}
		seen[key] = true
		original := strings.ToLower(strings.TrimSpace(endpoint.OriginalName))
		if original == "" {
			original = key
		}
		node := current[original]
		if node == nil {
			node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		configbootstrap.SetOrDeleteMappingScalar(node, "name", name)
		configbootstrap.SetOrDeleteMappingScalar(node, "url", rawURL)
		if token := strings.TrimSpace(endpoint.AuthToken); token != "" {
			configbootstrap.SetOrDeleteMappingScalar(node, "auth_token", token)
		} else if mappingScalar(node, "auth_token") == "" {
			return nil, fmt.Errorf("console endpoint %q auth token is required", name)
		}
		nextNode.Content = append(nextNode.Content, node)
	}
	if len(nextNode.Content) == 0 {
		configbootstrap.DeleteMappingKey(consoleNode, "endpoints")
	} else {
		setMappingNode(consoleNode, "endpoints", nextNode)
	}
	return configbootstrap.MarshalDocument(doc)
}

func mappingScalar(node *yaml.Node, key string) string {
	value := configbootstrap.FindMappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(value.Value)
}

func setMappingNode(node *yaml.Node, key string, value *yaml.Node) {
	for index := 0; index+1 < len(node.Content); index += 2 {
		if strings.EqualFold(strings.TrimSpace(node.Content[index].Value), key) {
			node.Content[index+1] = value
			return
		}
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}
