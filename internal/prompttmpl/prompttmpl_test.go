package prompttmpl

import (
	"strings"
	"testing"
	"text/template"
)

func TestMustParseAndRender(t *testing.T) {
	tmpl := MustParse("greeting", `{{upper .Name}}`, template.FuncMap{
		"upper": strings.ToUpper,
	})

	got, err := Render(tmpl, map[string]string{"Name": "morph"})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got != "MORPH" {
		t.Fatalf("Render() = %q, want MORPH", got)
	}
}

func TestRenderRejectsMissingKey(t *testing.T) {
	tmpl := MustParse("missing", `{{.Name}}`, nil)
	if _, err := Render(tmpl, map[string]string{}); err == nil {
		t.Fatal("Render() error = nil, want missing-key error")
	}
}
