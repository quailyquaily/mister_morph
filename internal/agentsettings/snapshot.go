package agentsettings

import (
	"time"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/spf13/viper"
)

// Reader is the read-only configuration surface used by the agent settings
// HTTP routes. Implementations are treated as immutable after construction.
type Reader interface {
	llmutil.ConfigReader
	GetBool(string) bool
	GetDuration(string) time.Duration
	GetInt(string) int
	GetInt64(string) int64
	GetStringSlice(string) []string
	IsSet(string) bool
	AllSettings() map[string]any
	ConfigFileUsed() string
}

// ReaderSnapshot owns a detached Viper instance and exposes only read methods.
// This prevents route handlers from following a process-global or reloadable
// Viper pointer after their runtime has been constructed.
type ReaderSnapshot struct {
	reader     *viper.Viper
	configFile string
}

func NewReaderSnapshot(source Reader) *ReaderSnapshot {
	reader := viper.New()
	configdefaults.Apply(reader)
	configFile := ""
	if source != nil {
		// AllSettings is produced by Viper itself, so MergeConfigMap receives a
		// map shape it already accepts. Defaults remain in place for omitted keys.
		_ = reader.MergeConfigMap(source.AllSettings())
		configFile = source.ConfigFileUsed()
	}
	return &ReaderSnapshot{reader: reader, configFile: configFile}
}

func (s *ReaderSnapshot) GetString(key string) string {
	return s.reader.GetString(key)
}

func (s *ReaderSnapshot) GetBool(key string) bool {
	return s.reader.GetBool(key)
}

func (s *ReaderSnapshot) GetDuration(key string) time.Duration {
	return s.reader.GetDuration(key)
}

func (s *ReaderSnapshot) GetInt(key string) int {
	return s.reader.GetInt(key)
}

func (s *ReaderSnapshot) GetInt64(key string) int64 {
	return s.reader.GetInt64(key)
}

func (s *ReaderSnapshot) GetStringSlice(key string) []string {
	return append([]string(nil), s.reader.GetStringSlice(key)...)
}

func (s *ReaderSnapshot) IsSet(key string) bool {
	return s.reader.IsSet(key)
}

func (s *ReaderSnapshot) UnmarshalKey(key string, rawVal any, opts ...viper.DecoderConfigOption) error {
	return s.reader.UnmarshalKey(key, rawVal, opts...)
}

func (s *ReaderSnapshot) AllSettings() map[string]any {
	return s.reader.AllSettings()
}

func (s *ReaderSnapshot) ConfigFileUsed() string {
	return s.configFile
}
