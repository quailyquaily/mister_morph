package tools

import (
	"context"
	"testing"
)

type registryTestTool struct {
	name string
}

func (t *registryTestTool) Name() string { return t.name }

func (t *registryTestTool) Description() string { return "test tool" }

func (t *registryTestTool) ParameterSchema() string { return `{}` }

func (t *registryTestTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

func TestRegistryRegisterRejectsInvalidAndDuplicateTools(t *testing.T) {
	tests := []struct {
		name string
		tool Tool
	}{
		{name: "nil", tool: nil},
		{name: "typed nil", tool: (*registryTestTool)(nil)},
		{name: "empty name", tool: &registryTestTool{}},
		{name: "blank name", tool: &registryTestTool{name: "  \t"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewRegistry()
			if err := reg.Register(tt.tool); err == nil {
				t.Fatal("Register() error = nil, want validation error")
			}
			if got := len(reg.All()); got != 0 {
				t.Fatalf("registry size = %d, want 0", got)
			}
		})
	}

	reg := NewRegistry()
	original := &registryTestTool{name: "same"}
	replacement := &registryTestTool{name: "same"}
	if err := reg.Register(original); err != nil {
		t.Fatalf("Register(original) error = %v", err)
	}
	if err := reg.Register(replacement); err == nil {
		t.Fatal("Register(replacement) error = nil, want duplicate error")
	}
	got, ok := reg.Get("same")
	if !ok {
		t.Fatal("original tool missing after duplicate registration")
	}
	if got != original {
		t.Fatalf("registered tool = %p, want original %p", got, original)
	}
}

func TestRegistryReplaceExplicitlyOverwritesTool(t *testing.T) {
	reg := NewRegistry()
	original := &registryTestTool{name: "same"}
	replacement := &registryTestTool{name: "same"}
	if err := reg.Register(original); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := reg.Replace(replacement); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	got, ok := reg.Get("same")
	if !ok || got != replacement {
		t.Fatalf("registered tool = %v, want replacement", got)
	}
}

func TestRegistryCloneShallowCopiesRegistry(t *testing.T) {
	original := NewRegistry()
	shared := &registryTestTool{name: "shared"}
	if err := original.Register(shared); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	clone := original.Clone()
	got, ok := clone.Get("shared")
	if !ok || got != shared {
		t.Fatalf("clone tool = %v, want same tool instance", got)
	}
	if err := clone.Register(&registryTestTool{name: "clone-only"}); err != nil {
		t.Fatalf("clone Register() error = %v", err)
	}
	if _, ok := original.Get("clone-only"); ok {
		t.Fatal("clone registration mutated original registry")
	}
	if clone.Remove("shared"); !ok {
		t.Fatal("Remove(shared) = false, want true")
	}
	if _, ok := original.Get("shared"); !ok {
		t.Fatal("clone removal mutated original registry")
	}

	nilClone := (*Registry)(nil).Clone()
	if nilClone == nil || len(nilClone.All()) != 0 {
		t.Fatalf("nil Clone() = %#v, want empty registry", nilClone)
	}
}
