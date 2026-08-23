package chatcommands

import (
	"context"

	"github.com/quailyquaily/mistermorph/internal/workspace"
)

type RuntimeRegistryOptions struct {
	ModelCommand        ModelCommandFunc
	SkillCommand        SkillCommandFunc
	ContextCommand      ContextCommandFunc
	WorkspaceCommand    WorkspaceCommandFunc
	WorkspaceStore      *workspace.Store
	WorkspaceKey        string
	DefaultWorkspaceDir string
	HelpHeader          string
}

func NewRuntimeRegistry(opts RuntimeRegistryOptions) *Registry {
	reg := NewRegistry()
	header := opts.HelpHeader
	if header == "" {
		header = "Available commands:"
	}
	reg.Register("/help", "show available commands", HelpHandler(reg, header))
	reg.Register("/models", "inspect or change the active model", ModelCommandHandler(opts.ModelCommand))
	reg.Register("/think", "run a task with xhigh reasoning", nil)
	if opts.SkillCommand != nil {
		reg.Register("/skills", "show loaded skills", SkillCommandHandler(opts.SkillCommand))
	}
	if opts.ContextCommand != nil {
		reg.Register("/ctx", "show or compact context usage", ContextCommandHandler(opts.ContextCommand))
	}
	if opts.WorkspaceCommand != nil {
		reg.Register("/workspace", "show or change workspace", WorkspaceCommandHandler(opts.WorkspaceCommand))
	} else {
		reg.Register("/workspace", "show or change workspace", WorkspaceHandler(opts.WorkspaceStore, opts.WorkspaceKey, opts.DefaultWorkspaceDir))
	}
	return reg
}

func WorkspaceHandler(store *workspace.Store, workspaceKey string, defaultWorkspaceDir string) Handler {
	return func(ctx context.Context, args string) (*Result, error) {
		result, err := workspace.ExecuteStoreCommand(store, workspaceKey, args, defaultWorkspaceDir, nil)
		if err != nil {
			return nil, err
		}
		return &Result{Reply: result.Reply}, nil
	}
}
