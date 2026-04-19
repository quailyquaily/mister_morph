package chatcmd

import (
	"fmt"
	"io"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
)

func handleModelCommand(
	writer io.Writer,
	input string,
	llmValues llmutil.RuntimeValues,
	sessionStore *llmselect.Store,
	buildClient func(route llmutil.ResolvedRoute, cfgOverride *llmconfig.ClientConfig) (llm.Client, error),
) (llm.Client, llmconfig.ClientConfig, bool) {
	prev := sessionStore.Get()
	output, handled, err := llmselect.ExecuteCommandText(llmValues, sessionStore, input)
	if !handled {
		return nil, llmconfig.ClientConfig{}, false
	}
	if err != nil {
		_, _ = fmt.Fprintf(writer, "error: %v\n", err)
		return nil, llmconfig.ClientConfig{}, false
	}
	_, _ = fmt.Fprintln(writer, output)

	sel := sessionStore.Get()
	if sel == prev {
		return nil, llmconfig.ClientConfig{}, false
	}

	newRoute, err := llmselect.ResolveMainRoute(llmValues, sel)
	if err != nil {
		_, _ = fmt.Fprintf(writer, "error resolving route: %v\n", err)
		return nil, llmconfig.ClientConfig{}, false
	}
	newCfg := newRoute.ClientConfig
	newClient, err := buildClient(newRoute, &newCfg)
	if err != nil {
		_, _ = fmt.Fprintf(writer, "error rebuilding client: %v\n", err)
		return nil, llmconfig.ClientConfig{}, false
	}
	_, _ = fmt.Fprintf(writer, "\033[90m[active model: %s]\033[0m\n", newCfg.Model)
	return newClient, newCfg, true
}
