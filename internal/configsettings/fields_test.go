package configsettings

import "testing"

func TestPublicFieldSetsHaveUniquePathsAndApplyModes(t *testing.T) {
	seen := map[string]string{}
	sets := []struct {
		name   string
		fields []Field
	}{
		{name: "agent", fields: AgentFields()},
		{name: "console", fields: ConsoleFields()},
		{name: "system", fields: SystemFields()},
	}
	for _, set := range sets {
		for _, field := range set.fields {
			if previous, exists := seen[field.Path]; exists {
				t.Fatalf("field %q is present in %s and %s", field.Path, previous, set.name)
			}
			seen[field.Path] = set.name
			if field.ApplyMode == "" {
				t.Fatalf("field %q has no apply mode", field.Path)
			}
		}
	}

	for _, path := range []string{
		"llm.cache_ttl",
		"llm.routes.main_loop",
		"llm.routes.addressing",
		"max_steps",
		"tools.bash.timeout",
		"acp.agents",
		"telegram.record_untriggered",
		"heartbeat.interval",
		"server.auth_token",
		"logging.level",
		"workspace_dir",
		"file_cache.max_total_bytes",
		"tasks.persistence_targets",
		"bus.max_inflight",
	} {
		if _, exists := seen[path]; !exists {
			t.Errorf("public field %q is not registered", path)
		}
	}
}
