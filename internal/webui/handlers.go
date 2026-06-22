package webui

import (
	"fmt"
	"html/template"
	"net/http"
)

// fragmentTemplates holds pre-parsed template sets for the shell+fragment architecture.
// Each entry maps a fragment name (e.g., "dashboard") to a template set that contains
// both the "shell" and "content" template definitions.
type fragmentTemplates map[string]*template.Template

// parseFragmentTemplates parses the shell template together with each fragment and its
// partials. The result is a map of fragment name -> *template.Template where executing
// "shell" renders the full page (with the fragment's content block embedded) and executing
// "content" renders just the fragment HTML.
func parseFragmentTemplates() (fragmentTemplates, error) {
	fragments := []string{
		"dashboard",
		"backups",
		"restore",
		"config",
		"paths",
		"retention",
		"watchers",
		"activity",
		"clients",
		"metrics",
	}

	templates := make(fragmentTemplates, len(fragments))
	for _, name := range fragments {
		t, err := template.ParseFS(templatesFS,
			"templates/shell.html",
			"templates/fragments/"+name+".html",
			"templates/partials/*.html",
		)
		if err != nil {
			return nil, fmt.Errorf("parsing fragment template %s: %w", name, err)
		}
		templates[name] = t
	}
	return templates, nil
}

// renderFragment handles the dual-mode response for the shell+fragment architecture.
// If the request includes the HX-Request header (set by htmx), only the "content"
// template is rendered and the HX-Push-Url header is set. For direct browser navigation,
// the full "shell" template is rendered with the fragment embedded.
func (s *Server) renderFragment(w http.ResponseWriter, r *http.Request, fragment string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	tmpl, ok := s.fragmentTmpl[fragment]
	if !ok {
		s.logger.Error("fragment template not found", "fragment", fragment)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		// htmx request: return only the fragment content.
		w.Header().Set("HX-Push-Url", r.URL.Path)
		if err := tmpl.ExecuteTemplate(w, "content", data); err != nil {
			s.logger.Error("fragment template execution failed", "fragment", fragment, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	// Full page request: render the shell with content block filled by the fragment.
	if err := tmpl.ExecuteTemplate(w, "shell", data); err != nil {
		s.logger.Error("shell template execution failed", "fragment", fragment, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
