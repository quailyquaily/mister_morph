package personautil

import "testing"

func TestParseIdentityNameFromYAML(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "name", raw: "name: ' **Morph** '", want: "Morph"},
		{name: "placeholder", raw: "name: pick one", want: ""},
		{name: "invalid yaml", raw: "name: [", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseIdentityNameFromYAML(tc.raw); got != tc.want {
				t.Fatalf("parseIdentityNameFromYAML() = %q, want %q", got, tc.want)
			}
		})
	}
}
