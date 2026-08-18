package consolecmd

import (
	"context"
	"fmt"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/spf13/viper"
)

func (r *consoleLocalRuntime) ReloadAgentConfigFromReader(reader *viper.Viper) error {
	if r == nil {
		return fmt.Errorf("console runtime is not initialized")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.closed {
		return errConsoleExecutionClosed
	}
	generation, err := r.prepareGeneration(reader)
	if err != nil {
		return err
	}
	if err := r.applyPreparedGenerationLocked(generation); err != nil {
		generation.cleanupNow()
		return err
	}
	return nil
}

func (r *consoleLocalRuntime) submitTask(ctx context.Context, req daemonruntime.SubmitTaskRequest) (daemonruntime.SubmitTaskResponse, error) {
	generation, err := r.captureGeneration()
	if err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}
	return r.submitTaskWithGeneration(ctx, generation, req)
}
