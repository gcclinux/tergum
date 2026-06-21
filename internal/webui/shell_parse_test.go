package webui

import (
	"html/template"
	"testing"
)

func TestShellTemplateParse(t *testing.T) {
	_, err := template.ParseFS(templatesFS, "templates/shell.html")
	if err != nil {
		t.Fatalf("failed to parse shell.html: %v", err)
	}
}
