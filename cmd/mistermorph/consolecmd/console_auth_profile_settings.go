package consolecmd

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/integration"
	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type consoleAuthProfileSettingsPayload struct {
	OriginalName               string         `json:"original_name,omitempty"`
	Name                       string         `json:"name"`
	CredentialKind             string         `json:"credential_kind"`
	CredentialSecret           string         `json:"credential_secret"`
	CredentialSecretConfigured bool           `json:"credential_secret_configured,omitempty"`
	URLPrefixes                []string       `json:"url_prefixes"`
	Methods                    []string       `json:"methods"`
	FollowRedirects            bool           `json:"follow_redirects"`
	AllowProxy                 bool           `json:"allow_proxy"`
	DenyPrivateIPs             *bool          `json:"deny_private_ips"`
	Bindings                   map[string]any `json:"bindings"`
}

func validateConsoleAuthProfileSettings(raw []byte) error {
	reader := viper.New()
	integration.ApplyViperDefaults(reader)
	reader.SetConfigType("yaml")
	if err := reader.ReadConfig(bytes.NewReader(raw)); err != nil {
		return err
	}
	_, err := toolsutil.StaticRegistryConfigFromReader(reader)
	return err
}

func consoleAuthProfileSettingsFromDocument(doc *yaml.Node) []consoleAuthProfileSettingsPayload {
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil
	}
	profilesNode := configbootstrap.FindMappingValue(root, "auth_profiles")
	if profilesNode == nil || profilesNode.Kind != yaml.MappingNode {
		return nil
	}
	items := make([]consoleAuthProfileSettingsPayload, 0, len(profilesNode.Content)/2)
	for index := 0; index+1 < len(profilesNode.Content); index += 2 {
		name := strings.TrimSpace(profilesNode.Content[index].Value)
		node := profilesNode.Content[index+1]
		if name == "" || node == nil || node.Kind != yaml.MappingNode {
			continue
		}
		credentialNode := configbootstrap.FindMappingValue(node, "credential")
		allowNode := configbootstrap.FindMappingValue(node, "allow")
		bindings := map[string]any{}
		if bindingsNode := configbootstrap.FindMappingValue(node, "bindings"); bindingsNode != nil {
			_ = bindingsNode.Decode(&bindings)
		}
		secret := mappingScalar(credentialNode, "secret")
		items = append(items, consoleAuthProfileSettingsPayload{
			OriginalName:               name,
			Name:                       name,
			CredentialKind:             mappingScalar(credentialNode, "kind"),
			CredentialSecretConfigured: secret != "",
			URLPrefixes:                mappingStringList(allowNode, "url_prefixes"),
			Methods:                    mappingStringList(allowNode, "methods"),
			FollowRedirects:            mappingBool(allowNode, "follow_redirects"),
			AllowProxy:                 mappingBool(allowNode, "allow_proxy"),
			DenyPrivateIPs:             mappingOptionalBool(allowNode, "deny_private_ips"),
			Bindings:                   bindings,
		})
	}
	return items
}

func prepareConsoleAuthProfileSecrets(ctx context.Context, profiles []consoleAuthProfileSettingsPayload, store secref.OSStore) ([]string, error) {
	if store == nil {
		return nil, nil
	}
	type replacement struct {
		index int
		value string
	}
	created := make([]string, 0, len(profiles))
	replacements := make([]replacement, 0, len(profiles))
	for index := range profiles {
		value := strings.TrimSpace(profiles[index].CredentialSecret)
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
		name := strings.TrimSpace(profiles[index].Name)
		if err := store.Put(ctx, id, "auth_profiles."+name+".credential.secret", []byte(value)); err != nil {
			secref.DeleteOSSecrets(ctx, store, created)
			return nil, err
		}
		created = append(created, id)
		replacements = append(replacements, replacement{index: index, value: secref.OSSecretRef(id)})
	}
	for _, replacement := range replacements {
		profiles[replacement.index].CredentialSecret = replacement.value
	}
	return created, nil
}

func applyConsoleAuthProfileSettings(raw []byte, profiles []consoleAuthProfileSettingsPayload) ([]byte, error) {
	doc, err := configbootstrap.LoadDocumentBytes(raw)
	if err != nil {
		return nil, err
	}
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil, err
	}
	currentNode := configbootstrap.FindMappingValue(root, "auth_profiles")
	current := map[string]*yaml.Node{}
	if currentNode != nil && currentNode.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(currentNode.Content); index += 2 {
			current[strings.ToLower(strings.TrimSpace(currentNode.Content[index].Value))] = currentNode.Content[index+1]
		}
	}

	seen := map[string]bool{}
	nextNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, profile := range profiles {
		name := strings.TrimSpace(profile.Name)
		if name == "" || strings.TrimSpace(profile.CredentialKind) == "" || len(profile.URLPrefixes) == 0 || len(profile.Methods) == 0 {
			return nil, fmt.Errorf("auth profile name, credential kind, URL prefixes, and methods are required")
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("duplicate auth profile %q", name)
		}
		seen[key] = true
		original := strings.ToLower(strings.TrimSpace(profile.OriginalName))
		if original == "" {
			original = key
		}
		node := current[original]
		if node == nil || node.Kind != yaml.MappingNode {
			node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		credentialNode := configbootstrap.EnsureMappingValue(node, "credential")
		configbootstrap.SetOrDeleteMappingScalar(credentialNode, "kind", profile.CredentialKind)
		if secret := strings.TrimSpace(profile.CredentialSecret); secret != "" {
			configbootstrap.SetOrDeleteMappingScalar(credentialNode, "secret", secret)
		} else if mappingScalar(credentialNode, "secret") == "" {
			return nil, fmt.Errorf("auth profile %q credential secret is required", name)
		}

		allowNode := configbootstrap.EnsureMappingValue(node, "allow")
		configbootstrap.SetMappingStringList(allowNode, "url_prefixes", profile.URLPrefixes)
		configbootstrap.SetMappingStringList(allowNode, "methods", profile.Methods)
		configbootstrap.SetMappingBoolValue(allowNode, "follow_redirects", profile.FollowRedirects)
		configbootstrap.SetMappingBoolValue(allowNode, "allow_proxy", profile.AllowProxy)
		if profile.DenyPrivateIPs == nil {
			configbootstrap.DeleteMappingKey(allowNode, "deny_private_ips")
		} else {
			configbootstrap.SetMappingBoolValue(allowNode, "deny_private_ips", *profile.DenyPrivateIPs)
		}

		if len(profile.Bindings) == 0 {
			configbootstrap.DeleteMappingKey(node, "bindings")
		} else {
			bindingsNode := &yaml.Node{}
			if err := bindingsNode.Encode(profile.Bindings); err != nil {
				return nil, fmt.Errorf("auth profile %q bindings: %w", name, err)
			}
			setMappingNode(node, "bindings", bindingsNode)
		}
		nextNode.Content = append(nextNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name},
			node,
		)
	}
	if len(nextNode.Content) == 0 {
		configbootstrap.DeleteMappingKey(root, "auth_profiles")
	} else {
		setMappingNode(root, "auth_profiles", nextNode)
	}
	return configbootstrap.MarshalDocument(doc)
}

func mappingStringList(node *yaml.Node, key string) []string {
	value := configbootstrap.FindMappingValue(node, key)
	if value == nil || value.Kind != yaml.SequenceNode {
		return nil
	}
	items := make([]string, 0, len(value.Content))
	for _, item := range value.Content {
		if text := strings.TrimSpace(item.Value); text != "" {
			items = append(items, text)
		}
	}
	return items
}

func mappingBool(node *yaml.Node, key string) bool {
	value := mappingOptionalBool(node, key)
	return value != nil && *value
}

func mappingOptionalBool(node *yaml.Node, key string) *bool {
	value := configbootstrap.FindMappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return nil
	}
	parsed := strings.EqualFold(strings.TrimSpace(value.Value), "true")
	return &parsed
}
