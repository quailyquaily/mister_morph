package caprefs

import (
	"reflect"
	"testing"
)

func TestNames(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "empty", text: "plain text", want: nil},
		{name: "single", text: "use $bash now", want: []string{"bash"}},
		{name: "dedupe case insensitive", text: "$bash and $BASH", want: []string{"bash"}},
		{name: "preserve first spelling and order", text: "$One, $two, then $ONE", want: []string{"One", "two"}},
		{name: "tool chars", text: "$image_generate $my.skill-name", want: []string{"image_generate", "my.skill-name"}},
		{name: "money ignored", text: "budget is $100", want: nil},
		{name: "env var parsed as candidate", text: "env $OPENAI_API_KEY", want: []string{"OPENAI_API_KEY"}},
		{name: "embedded word ignored", text: "foo$bar", want: nil},
		{name: "punctuation colon", text: "$bash: run tests", want: []string{"bash"}},
		{name: "colon has no special syntax", text: "$tool:bash", want: []string{"tool"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Names(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Names() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
