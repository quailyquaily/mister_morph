package prompttmpl

import (
	"bytes"
	"text/template"
)

func MustParse(name, source string, funcs template.FuncMap) *template.Template {
	t := template.New(name).Option("missingkey=error")
	if funcs != nil {
		t = t.Funcs(funcs)
	}
	return template.Must(t.Parse(source))
}

func Render(t *template.Template, data any) (string, error) {
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}
