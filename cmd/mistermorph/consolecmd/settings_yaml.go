package consolecmd

import (
	"bytes"
	"os"

	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"gopkg.in/yaml.v3"
)

func loadYAMLDocument(configPath string) (*yaml.Node, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return configbootstrap.NewEmptyDocument(), nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return configbootstrap.NewEmptyDocument(), nil
	}
	return configbootstrap.LoadDocumentBytes(data)
}

func setMappingOrderedStringList(node *yaml.Node, key string, values []string) {
	values = normalizeConsoleStringList(values)
	if len(values) == 0 {
		configbootstrap.DeleteMappingKey(node, key)
		return
	}
	configbootstrap.SetMappingStringList(node, key, values)
}
