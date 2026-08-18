package tools

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(tool Tool) error {
	name, err := registryToolName(tool)
	if err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("register tool %q: registry is nil", name)
	}
	if r.tools == nil {
		r.tools = make(map[string]Tool)
	}
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("register tool %q: name already registered", name)
	}
	r.tools[name] = tool
	return nil
}

// Replace installs tool even when another tool already uses the same name.
// Use it only where replacement is an explicit part of the composition contract.
func (r *Registry) Replace(tool Tool) error {
	name, err := registryToolName(tool)
	if err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("replace tool %q: registry is nil", name)
	}
	if r.tools == nil {
		r.tools = make(map[string]Tool)
	}
	r.tools[name] = tool
	return nil
}

// Clone makes an independent registry that shares the registered tool instances.
func (r *Registry) Clone() *Registry {
	out := NewRegistry()
	if r == nil {
		return out
	}
	for name, tool := range r.tools {
		out.tools[name] = tool
	}
	return out
}

func (r *Registry) Remove(name string) bool {
	if r == nil || r.tools == nil {
		return false
	}
	_, exists := r.tools[name]
	if exists {
		delete(r.tools, name)
	}
	return exists
}

func registryToolName(tool Tool) (string, error) {
	if tool == nil {
		return "", fmt.Errorf("tool is nil")
	}
	value := reflect.ValueOf(tool)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return "", fmt.Errorf("tool is nil")
		}
	}
	name := tool.Name()
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("tool name is empty")
	}
	return name, nil
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
		fmt.Fprintf(&b, "- `%s`: %s\n", t.Name(), desc)
	}
	return b.String()
}
