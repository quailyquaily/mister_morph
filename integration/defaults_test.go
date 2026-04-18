package integration

import (
	"reflect"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/spf13/viper"
)

func TestApplyViperDefaultsMatchesSharedDefaults(t *testing.T) {
	got := viper.New()
	want := viper.New()

	ApplyViperDefaults(got)
	configdefaults.Apply(want)

	if gotValue := got.GetString("logging.format"); gotValue != want.GetString("logging.format") {
		t.Fatalf("logging.format = %q, want %q", gotValue, want.GetString("logging.format"))
	}
	if gotValue := got.GetString("console.listen"); gotValue != want.GetString("console.listen") {
		t.Fatalf("console.listen = %q, want %q", gotValue, want.GetString("console.listen"))
	}
	if gotValue := got.GetString("tasks.dir_name"); gotValue != want.GetString("tasks.dir_name") {
		t.Fatalf("tasks.dir_name = %q, want %q", gotValue, want.GetString("tasks.dir_name"))
	}
	if gotValue := got.GetFloat64("telegram.addressing_interject_threshold"); gotValue != want.GetFloat64("telegram.addressing_interject_threshold") {
		t.Fatalf("telegram.addressing_interject_threshold = %v, want %v", gotValue, want.GetFloat64("telegram.addressing_interject_threshold"))
	}
	if gotValue := got.GetStringSlice("multimodal.image.sources"); !reflect.DeepEqual(gotValue, want.GetStringSlice("multimodal.image.sources")) {
		t.Fatalf("multimodal.image.sources = %#v, want %#v", gotValue, want.GetStringSlice("multimodal.image.sources"))
	}
}
