package chatcommands

import (
	"context"

	"github.com/quailyquaily/mistermorph/internal/workspace"
)

type RuntimeRegistryOptions struct {
	ModelCommand     ModelCommandFunc
	SkillCommand     SkillCommandFunc
	ContextCommand   ContextCommandFunc
	WorkspaceCommand WorkspaceCommandFunc
	WorkspaceStore   *workspace.Store
	WorkspaceKey     string
	HelpHeader       string
}

func NewRuntimeRegistry(opts RuntimeRegistryOptions) *Registry {
	reg := NewRegistry()
	header := opts.HelpHeader
	if header == "" {
		header = "Available commands:"
	}
	reg.Register("/help", HelpHandler(reg, header))
	reg.Register("/model", ModelCommandHandler(opts.ModelCommand))
	if opts.SkillCommand != nil {
		reg.Register("/skill", SkillCommandHandler(opts.SkillCommand))
	}
	if opts.ContextCommand != nil {
		reg.Register("/ctx", ContextCommandHandler(opts.ContextCommand))
	}
	if opts.WorkspaceCommand != nil {
		reg.Register("/workspace", WorkspaceCommandHandler(opts.WorkspaceCommand))
	} else {
		reg.Register("/workspace", WorkspaceHandler(opts.WorkspaceStore, opts.WorkspaceKey))
	}
	return reg
}

func WorkspaceHandler(store *workspace.Store, workspaceKey string) Handler {
	return func(ctx context.Context, args string) (*Result, error) {
		result, err := workspace.ExecuteStoreCommand(store, workspaceKey, args, nil)
		if err != nil {
			return nil, err
		}
		return &Result{Reply: result.Reply}, nil
	}
}
