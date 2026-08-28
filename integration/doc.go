// Package integration provides a reusable wiring layer for embedding
// mistermorph capabilities into third-party Go programs.
//
// It wires common features such as built-in tools, plan/todo helpers,
// guard, skills prompt loading, and request/prompt inspect dumps.
// Built-in tools can be narrowed with Config.BuiltinToolNames.
//
// In addition to one-shot task execution (RunTask), it can also expose
// long-running channel runners via Runtime.NewTelegramBot(...) and
// Runtime.NewSlackBot(...), and Runtime.NewMixinBot(...).
//
// Configuration is explicit via Config.Set(...) / Config.Overrides.
// The embedding host owns env/config-file loading and passes resolved values in.
//
// Note: this package snapshots config from explicit Config overrides plus
// integration defaults once during Runtime construction.
package integration
