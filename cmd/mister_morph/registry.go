package main

import (
	"strings"
	"time"

	"github.com/quailyquaily/mister_morph/tools"
	"github.com/quailyquaily/mister_morph/tools/builtin"
	"github.com/spf13/viper"
)

func registryFromViper() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(builtin.NewEchoTool())

	viper.SetDefault("tools.read_file.max_bytes", 256*1024)
	viper.SetDefault("tools.read_file.deny_paths", []string{"config.yaml"})

	viper.SetDefault("tools.write_file.enabled", true)
	viper.SetDefault("tools.write_file.max_bytes", 512*1024)

	viper.SetDefault("tools.bash.enabled", false)
	viper.SetDefault("tools.bash.confirm", false)
	viper.SetDefault("tools.bash.timeout", 30*time.Second)
	viper.SetDefault("tools.bash.max_output_bytes", 256*1024)
	viper.SetDefault("tools.bash.deny_paths", []string{"config.yaml"})

	viper.SetDefault("tools.url_fetch.enabled", true)
	viper.SetDefault("tools.url_fetch.timeout", 30*time.Second)
	viper.SetDefault("tools.url_fetch.max_bytes", int64(512*1024))
	viper.SetDefault("tools.web_search.enabled", true)
	viper.SetDefault("tools.web_search.timeout", 20*time.Second)
	viper.SetDefault("tools.web_search.max_results", 5)
	viper.SetDefault("tools.web_search.base_url", "https://duckduckgo.com/html/")

	userAgent := strings.TrimSpace(viper.GetString("user_agent"))

	r.Register(builtin.NewReadFileToolWithDenyPaths(
		int64(viper.GetInt("tools.read_file.max_bytes")),
		viper.GetStringSlice("tools.read_file.deny_paths"),
	))

	r.Register(builtin.NewWriteFileTool(
		viper.GetBool("tools.write_file.enabled"),
		viper.GetInt("tools.write_file.max_bytes"),
		strings.TrimSpace(viper.GetString("file_cache_dir")),
	))

	if viper.GetBool("tools.bash.enabled") {
		bt := builtin.NewBashTool(
			true,
			viper.GetBool("tools.bash.confirm"),
			viper.GetDuration("tools.bash.timeout"),
			viper.GetInt("tools.bash.max_output_bytes"),
		)
		bt.DenyPaths = viper.GetStringSlice("tools.bash.deny_paths")
		r.Register(bt)
	}

	if viper.GetBool("tools.url_fetch.enabled") {
		r.Register(builtin.NewURLFetchTool(
			true,
			viper.GetDuration("tools.url_fetch.timeout"),
			viper.GetInt64("tools.url_fetch.max_bytes"),
			userAgent,
		))
	}

	if viper.GetBool("tools.web_search.enabled") {
		r.Register(builtin.NewWebSearchTool(
			true,
			viper.GetString("tools.web_search.base_url"),
			viper.GetDuration("tools.web_search.timeout"),
			viper.GetInt("tools.web_search.max_results"),
			userAgent,
		))
	}

	return r
}

func viperGetBool(key, legacy string) bool {
	if viper.IsSet(key) {
		return viper.GetBool(key)
	}
	return viper.GetBool(legacy)
}

func viperGetDuration(key, legacy string) time.Duration {
	if viper.IsSet(key) {
		return viper.GetDuration(key)
	}
	return viper.GetDuration(legacy)
}

func viperGetInt(key, legacy string) int {
	if viper.IsSet(key) {
		return viper.GetInt(key)
	}
	return viper.GetInt(legacy)
}

func viperGetInt64(key, legacy string) int64 {
	if viper.IsSet(key) {
		return viper.GetInt64(key)
	}
	return viper.GetInt64(legacy)
}

func viperGetString(key, legacy string) string {
	if viper.IsSet(key) {
		return viper.GetString(key)
	}
	return viper.GetString(legacy)
}

func viperGetStringSlice(key, legacy string) []string {
	if viper.IsSet(key) {
		return viper.GetStringSlice(key)
	}
	if viper.IsSet(legacy) {
		return viper.GetStringSlice(legacy)
	}
	return nil
}
