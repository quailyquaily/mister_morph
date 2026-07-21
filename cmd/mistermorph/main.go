package main

import (
	"context"
	"os"

	"github.com/quailyquaily/mistermorph/internal/processsignal"
)

func main() {
	ctx, stop := processsignal.NotifyContext(context.Background())
	defer stop()
	if err := ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
