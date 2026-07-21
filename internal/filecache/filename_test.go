package filecache

import (
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "  ", want: "file"},
		{name: "basename", in: "../../reports/final.txt", want: "final.txt"},
		{name: "allowed characters", in: "Report_2026-07+draft.pdf", want: "Report_2026-07+draft.pdf"},
		{name: "unsafe characters", in: " final report (v2)!.pdf ", want: "final_report__v2__.pdf"},
		{name: "trim unsafe edge", in: "..hidden??..", want: "hidden"},
		{name: "no usable characters", in: "报告", want: "file"},
		{name: "limit bytes", in: strings.Repeat("a", 121), want: strings.Repeat("a", 120)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SanitizeFilename(test.in); got != test.want {
				t.Fatalf("SanitizeFilename(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}
