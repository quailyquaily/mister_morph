package tools

import (
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

func (r *Registry) ToolNames() string {
	all := r.All()
	names := make([]string, len(all))
	for i, t := range all {
		names[i] = t.Name()
	}
	return strings.Join(names, ", ")
}

func (r *Registry) FormatToolSummaries() string {
	all := r.All()
	var b strings.Builder
	for _, t := range all {
		desc := strings.TrimSpace(t.Description())
		if desc == "" {
			desc = "No description provided."
		}
		fmt.Fprintf(&b, "### %s\n%s\n\n", t.Name(), desc)
	}
	return b.String()
}
