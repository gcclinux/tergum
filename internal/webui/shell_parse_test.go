package webui

import (
	"html/template"
	"testing"
)

func TestShellTemplateParse(t *testing.T) {
	tmpl := template.New("shell.html").Funcs(template.FuncMap{
		"version": func() string { return "1.0.0" },
	})
	_, err := tmpl.ParseFS(templatesFS, "templates/shell.html")
	if err != nil {
		t.Fatalf("failed to parse shell.html: %v", err)
	}
}
