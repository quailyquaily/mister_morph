package main

import (
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/clifmt"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type toolPreview struct {
	Name        string
	Description string
}

func newToolsCmd(registryFactory func() *tools.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "List available tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runToolsCmd(cmd, args, registryFactory)
		},
	}
}

func runToolsCmd(cmd *cobra.Command, _ []string, registryFactory func() *tools.Registry) error {
	r := tools.NewRegistry()
	if registryFactory != nil {
		if reg := registryFactory(); reg != nil {
			r = reg
		}
	}
	corePreviews := make(map[string]toolPreview, len(r.All()))
	for _, tool := range r.All() {
		name := strings.TrimSpace(tool.Name())
		if name == "" {
			continue
		}
		corePreviews[name] = toolPreview{
			Name:        name,
			Description: strings.TrimSpace(tool.Description()),
		}
	}

	extraPreviews := map[string]toolPreview{}
	if viper.GetBool("tools.spawn.enabled") {
		addToolPreview(extraPreviews, toolPreview{
			Name:        "spawn",
			Description: "Starts a subtask with its own context and a restricted tool whitelist, then returns a structured result envelope.",
		})
	}
	// Runtime tools are injected in run/serve/telegram/slack.
	toolsutil.RegisterRuntimeTools(r, toolsutil.LoadRuntimeToolsRegisterConfigFromViper(), toolsutil.RuntimeToolLLMOptions{})
	for _, name := range []string{toolsutil.BuiltinPlanCreate, toolsutil.BuiltinTodoUpdate} {
		if t, ok := r.Get(name); ok {
			addToolPreview(extraPreviews, toolPreview{
				Name:        t.Name(),
				Description: t.Description(),
			})
		}
	}

	telegramPreviews := map[string]toolPreview{}
	// Telegram-only tools are injected at runtime and are not part of the base registry.
	addToolPreview(telegramPreviews, toolPreview{
		Name:        "telegram_send_file",
		Description: "[telegram only] Sends a local file (under file_cache_dir) to the active chat.",
	})
	addToolPreview(telegramPreviews, toolPreview{
		Name:        "telegram_send_photo",
		Description: "[telegram only] Sends a local image (under file_cache_dir) to the active chat as an inline photo.",
	})
	addToolPreview(telegramPreviews, toolPreview{
		Name:        "telegram_send_voice",
		Description: "[telegram only] Sends a voice message from a local audio file under file_cache_dir.",
	})
	addToolPreview(telegramPreviews, toolPreview{
		Name:        "message_react",
		Description: "[telegram only] Adds an emoji reaction to a Telegram message.",
	})

	out := cmd.OutOrStdout()
	coreRows := rowsFromToolPreviews(corePreviews)
	extraRows := rowsFromToolPreviews(extraPreviews)
	telegramRows := rowsFromToolPreviews(telegramPreviews)

	clifmt.PrintNameDetailTable(out, clifmt.NameDetailTableOptions{
		Title:          "Core tools",
		Rows:           coreRows,
		EmptyText:      "No core tools are registered.",
		NameHeader:     "NAME",
		DetailHeader:   "DESCRIPTION",
		MinDetailWidth: 36,
		EmptyDetail:    "No description provided.",
	})

	_, _ = out.Write([]byte("\n"))
	clifmt.PrintNameDetailTable(out, clifmt.NameDetailTableOptions{
		Title:          "Extra tools",
		Rows:           extraRows,
		EmptyText:      "No extra tools are available.",
		NameHeader:     "NAME",
		DetailHeader:   "DESCRIPTION",
		MinDetailWidth: 36,
		EmptyDetail:    "No description provided.",
	})

	_, _ = out.Write([]byte("\n"))
	clifmt.PrintNameDetailTable(out, clifmt.NameDetailTableOptions{
		Title:          "Telegram tools",
		Rows:           telegramRows,
		EmptyText:      "No Telegram tools are available.",
		NameHeader:     "NAME",
		DetailHeader:   "DESCRIPTION",
		MinDetailWidth: 36,
		EmptyDetail:    "No description provided.",
	})
	return nil
}

func addToolPreview(previews map[string]toolPreview, p toolPreview) {
	if previews == nil {
		return
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return
	}
	if _, exists := previews[name]; exists {
		return
	}
	p.Name = name
	p.Description = strings.TrimSpace(p.Description)
	previews[name] = p
}

func rowsFromToolPreviews(previews map[string]toolPreview) []clifmt.NameDetailRow {
	if len(previews) == 0 {
		return nil
	}
	names := make([]string, 0, len(previews))
	for name := range previews {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]clifmt.NameDetailRow, 0, len(names))
	for _, name := range names {
		p := previews[name]
		rows = append(rows, clifmt.NameDetailRow{
			Name:   p.Name,
			Detail: p.Description,
		})
	}
	return rows
}
