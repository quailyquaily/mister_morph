package runtimecommands

// Suggestion describes a slash command candidate for chat composers.
type Suggestion struct {
	Value       string `json:"value"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	InsertText  string `json:"insert_text"`
}

var suggestions = []Suggestion{
	{
		Value:       "/help",
		Title:       "/help",
		Description: "Show available runtime commands.",
		InsertText:  "/help ",
	},
	{
		Value:       "/stop",
		Title:       "/stop",
		Description: "Stop the active task in this conversation.",
		InsertText:  "/stop ",
	},
	{
		Value:       "/models",
		Title:       "/models",
		Description: "Show the current model profile.",
		InsertText:  "/models ",
	},
	{
		Value:       "/skills",
		Title:       "/skills",
		Description: "Show the currently loaded skills.",
		InsertText:  "/skills ",
	},
	{
		Value:       "/ctx",
		Title:       "/ctx",
		Description: "Show context window usage for this conversation.",
		InsertText:  "/ctx ",
	},
	{
		Value:       "/workspace",
		Title:       "/workspace",
		Description: "Show the current workspace directory.",
		InsertText:  "/workspace ",
	},
	{
		Value:       "/think",
		Title:       "/think <task>",
		Description: "Run a task through the think LLM route.",
		InsertText:  "/think ",
	},
	{
		Value:       "/ctx compact",
		Title:       "/ctx compact",
		Description: "Compact older context into a checkpoint now.",
		InsertText:  "/ctx compact ",
	},
	{
		Value:       "/models list",
		Title:       "/models list",
		Description: "List configured model profiles.",
		InsertText:  "/models list ",
	},
	{
		Value:       "/models set",
		Title:       "/models set <profile>",
		Description: "Switch the current model profile.",
		InsertText:  "/models set ",
	},
	{
		Value:       "/models reset",
		Title:       "/models reset",
		Description: "Return model selection to automatic mode.",
		InsertText:  "/models reset ",
	},
	{
		Value:       "/workspace attach",
		Title:       "/workspace attach <dir>",
		Description: "Attach or replace the workspace directory.",
		InsertText:  "/workspace attach ",
	},
	{
		Value:       "/workspace detach",
		Title:       "/workspace detach",
		Description: "Detach the current workspace directory.",
		InsertText:  "/workspace detach ",
	},
}

// Suggestions returns the runtime slash commands exposed to chat composer
// autocompletion.
func Suggestions() []Suggestion {
	out := make([]Suggestion, len(suggestions))
	copy(out, suggestions)
	return out
}
