package maepruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/maep"
)

type StartOptions struct {
	Dir         string
	ListenAddrs []string
	Logger      *slog.Logger
	OnDataPush  func(event maep.DataPushEvent)
}

func Start(ctx context.Context, opts StartOptions) (*maep.Node, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		dir = statepaths.MAEPDir()
	} else {
		dir = pathutil.ExpandHomePath(dir)
	}
	svc := maep.NewService(maep.NewFileStore(dir))
	if _, _, err := svc.EnsureIdentity(ctx, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("ensure maep identity: %w", err)
	}
	node, err := maep.NewNode(ctx, svc, maep.NodeOptions{
		ListenAddrs: normalizeListenAddrs(opts.ListenAddrs),
		Logger:      opts.Logger,
		OnDataPush:  opts.OnDataPush,
	})
	if err != nil {
		return nil, err
	}
	return node, nil
}

func normalizeListenAddrs(input []string) []string {
	out := make([]string, 0, len(input))
	seen := map[string]bool{}
	for _, raw := range input {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
