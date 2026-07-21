package core

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestMemoryRuntimeCleanupClosesDraftClientOnce(t *testing.T) {
	client := &channelBootstrapClient{}
	runtime, err := NewMemoryRuntime(memoryRuntimeLifecycleDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return client, nil
	}), MemoryRuntimeOptions{
		Enabled:    true,
		MemoryDir:  filepath.Join(t.TempDir(), "memory"),
		JournalDir: filepath.Join(t.TempDir(), "journal"),
	})
	if err != nil {
		t.Fatalf("NewMemoryRuntime() error = %v", err)
	}

	runtime.Cleanup()
	runtime.Cleanup()
	if client.closeCalls != 1 {
		t.Fatalf("draft client close calls = %d, want 1", client.closeCalls)
	}
}

func TestMemoryRuntimeClosesDraftClientWhenCreationFails(t *testing.T) {
	client := &channelBootstrapClient{}
	createErr := errors.New("create memory draft client")
	_, err := NewMemoryRuntime(memoryRuntimeLifecycleDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return client, createErr
	}), MemoryRuntimeOptions{
		Enabled:    true,
		MemoryDir:  filepath.Join(t.TempDir(), "memory"),
		JournalDir: filepath.Join(t.TempDir(), "journal"),
	})
	if !errors.Is(err, createErr) {
		t.Fatalf("NewMemoryRuntime() error = %v, want %v", err, createErr)
	}
	if client.closeCalls != 1 {
		t.Fatalf("draft client close calls = %d, want 1", client.closeCalls)
	}
}

func memoryRuntimeLifecycleDeps(createClient func(llmutil.ResolvedRoute) (llm.Client, error)) depsutil.CommonDependencies {
	return depsutil.CommonDependencies{
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{
				Purpose: purpose,
				Profile: "memory",
				ClientConfig: llmconfig.ClientConfig{
					Provider: "test",
					Model:    "memory-model",
				},
			}, nil
		},
		CreateLLMClient: createClient,
	}
}
