package consolecmd

import "github.com/spf13/viper"

func testServerWithRuntimeReader(reader *viper.Viper) *server {
	return &server{
		localRuntime: &consoleLocalRuntime{
			generation: &consoleLocalRuntimeGeneration{reader: reader},
		},
	}
}
