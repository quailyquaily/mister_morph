package personautil

import "testing"

func TestParseIdentityName(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "yaml block value",
			raw: "# IDENTITY.md\n\n```yaml\nname: Morph\nname_alts: [M]\ncreature: Fox\n" +
				"vibe: sharp\nemoji: \"\"\n```\n",
			want: "Morph",
		},
		{
			name: "yaml placeholder ignored",
			raw:  "# IDENTITY.md\n\n```yaml\nname: \"(pick one)\"\ncreature: Fox\n```\n",
			want: "",
		},
		{
			name: "inline value",
			raw:  "# IDENTITY.md\n\n- **Name:** Morph\n- **Creature:** Fox\n",
			want: "Morph",
		},
		{
			name: "next line value",
			raw:  "# IDENTITY.md\n\n- **Name:**\n  Morph\n- **Creature:** Fox\n",
			want: "Morph",
		},
		{
			name: "placeholder ignored",
			raw:  "# IDENTITY.md\n\n- **Name:**\n  *(pick one)*\n- **Creature:** Fox\n",
			want: "",
		},
		{
			name: "missing name",
			raw:  "# IDENTITY.md\n\n- **Creature:** Fox\n",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseIdentityName(tc.raw); got != tc.want {
				t.Fatalf("ParseIdentityName() = %q, want %q", got, tc.want)
			}
		})
	}
}
