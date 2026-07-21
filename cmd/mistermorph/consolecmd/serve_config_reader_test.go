package consolecmd

import (
	"testing"

	"github.com/spf13/viper"
)

func TestServerCurrentRuntimeConfigReaderDoesNotUseGlobalViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("llm.model", "global-model")

	reader := (&server{}).currentRuntimeConfigReader()
	if got := reader.GetString("llm.model"); got == "global-model" {
		t.Fatalf("currentRuntimeConfigReader() observed process-global model %q", got)
	}
}

func TestConsoleFileCacheDirDoesNotUseGlobalViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("file_cache_dir", t.TempDir())

	if got := consoleFileCacheDir(nil); got == viper.GetString("file_cache_dir") {
		t.Fatalf("consoleFileCacheDir(nil) observed process-global cache directory %q", got)
	}
}
