package webui

import (
	"strings"
	"testing"
)

// =============================================================================
// Unit Tests for MarkdownRenderer
// These tests verify specific markdown inputs produce expected HTML outputs.
// Validates: Requirements 3.2, 3.3, 3.4, 3.5, 3.6
// =============================================================================

func TestMarkdownRenderer_HeadingRendering(t *testing.T) {
	renderer := NewMarkdownRenderer()

	tests := []struct {
		name         string
		markdown     string
		wantTag      string
		wantText     string
		wantID       string
	}{
		{
			name:     "h1 heading",
			markdown: "# Main Title",
			wantTag:  "<h1",
			wantText: "Main Title",
			wantID:   `id="main-title"`,
		},
		{
			name:     "h2 heading",
			markdown: "## Section Heading",
			wantTag:  "<h2",
			wantText: "Section Heading",
			wantID:   `id="section-heading"`,
		},
		{
			name:     "h3 heading",
			markdown: "### Subsection",
			wantTag:  "<h3",
			wantText: "Subsection",
			wantID:   `id="subsection"`,
		},
		{
			name:     "h4 heading",
			markdown: "#### Level Four",
			wantTag:  "<h4",
			wantText: "Level Four",
			wantID:   `id="level-four"`,
		},
		{
			name:     "h5 heading",
			markdown: "##### Level Five",
			wantTag:  "<h5",
			wantText: "Level Five",
			wantID:   `id="level-five"`,
		},
		{
			name:     "h6 heading",
			markdown: "###### Level Six",
			wantTag:  "<h6",
			wantText: "Level Six",
			wantID:   `id="level-six"`,
		},
		{
			name:     "heading with special characters",
			markdown: "## tergum client list",
			wantTag:  "<h2",
			wantText: "tergum client list",
			wantID:   `id="tergum-client-list"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := renderer.Render([]byte(tt.markdown))
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}

			html := string(result.HTML)

			// Verify the heading tag is present
			if !strings.Contains(html, tt.wantTag) {
				t.Errorf("expected HTML to contain %q tag, got: %s", tt.wantTag, html)
			}

			// Verify the heading text is preserved
			if !strings.Contains(html, tt.wantText) {
				t.Errorf("expected HTML to contain text %q, got: %s", tt.wantText, html)
			}

			// Verify the ID attribute is present
			if !strings.Contains(html, tt.wantID) {
				t.Errorf("expected HTML to contain %q, got: %s", tt.wantID, html)
			}
		})
	}
}

func TestMarkdownRenderer_TableRendering(t *testing.T) {
	renderer := NewMarkdownRenderer()

	markdown := `| Flag | Description |
| ---- | ----------- |
| --verbose | Enable verbose output |
| --config | Path to config file |`

	result, err := renderer.Render([]byte(markdown))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	html := string(result.HTML)

	// Verify table structure
	if !strings.Contains(html, "<table>") {
		t.Error("expected HTML to contain <table> tag")
	}
	if !strings.Contains(html, "</table>") {
		t.Error("expected HTML to contain </table> tag")
	}

	// Verify thead and tbody
	if !strings.Contains(html, "<thead>") {
		t.Error("expected HTML to contain <thead> tag")
	}
	if !strings.Contains(html, "<tbody>") {
		t.Error("expected HTML to contain <tbody> tag")
	}

	// Verify header cells
	if !strings.Contains(html, "<th>") {
		t.Error("expected HTML to contain <th> tag")
	}
	if !strings.Contains(html, "Flag") {
		t.Error("expected HTML to contain header text 'Flag'")
	}
	if !strings.Contains(html, "Description") {
		t.Error("expected HTML to contain header text 'Description'")
	}

	// Verify data cells
	if !strings.Contains(html, "<td>") {
		t.Error("expected HTML to contain <td> tag")
	}
	if !strings.Contains(html, "--verbose") {
		t.Error("expected HTML to contain '--verbose'")
	}
	if !strings.Contains(html, "Enable verbose output") {
		t.Error("expected HTML to contain 'Enable verbose output'")
	}
}

func TestMarkdownRenderer_LinkRendering(t *testing.T) {
	renderer := NewMarkdownRenderer()

	tests := []struct {
		name     string
		markdown string
		wantHref string
		wantText string
	}{
		{
			name:     "basic link",
			markdown: "[GitHub](https://github.com)",
			wantHref: `href="https://github.com"`,
			wantText: "GitHub",
		},
		{
			name:     "link with path",
			markdown: "[Documentation](https://example.com/docs/api)",
			wantHref: `href="https://example.com/docs/api"`,
			wantText: "Documentation",
		},
		{
			name:     "link with query params",
			markdown: "[Search](https://search.com?q=test&page=1)",
			wantHref: `href="https://search.com?q=test&amp;page=1"`,
			wantText: "Search",
		},
		{
			name:     "relative link",
			markdown: "[README](./README.md)",
			wantHref: `href="./README.md"`,
			wantText: "README",
		},
		{
			name:     "anchor link",
			markdown: "[Setup](#setup)",
			wantHref: `href="#setup"`,
			wantText: "Setup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := renderer.Render([]byte(tt.markdown))
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}

			html := string(result.HTML)

			// Verify anchor tag is present
			if !strings.Contains(html, "<a ") {
				t.Error("expected HTML to contain <a> tag")
			}

			// Verify href attribute
			if !strings.Contains(html, tt.wantHref) {
				t.Errorf("expected HTML to contain %q, got: %s", tt.wantHref, html)
			}

			// Verify link text
			if !strings.Contains(html, tt.wantText) {
				t.Errorf("expected HTML to contain text %q, got: %s", tt.wantText, html)
			}
		})
	}
}

func TestMarkdownRenderer_CodeBlockSyntaxHighlighting(t *testing.T) {
	renderer := NewMarkdownRenderer()

	tests := []struct {
		name         string
		markdown     string
		wantPre      bool
		wantChroma   bool
		wantLanguage string
	}{
		{
			name: "bash code block",
			markdown: "```bash\ntergum backup --all\n```",
			wantPre:      true,
			wantChroma:   true,
			wantLanguage: "bash",
		},
		{
			name: "go code block",
			markdown: "```go\npackage main\n\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```",
			wantPre:      true,
			wantChroma:   true,
			wantLanguage: "go",
		},
		{
			name: "json code block",
			markdown: "```json\n{\"key\": \"value\"}\n```",
			wantPre:      true,
			wantChroma:   true,
			wantLanguage: "json",
		},
		{
			name: "code block without language",
			markdown: "```\nplain text code\n```",
			wantPre:    true,
			wantChroma: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := renderer.Render([]byte(tt.markdown))
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}

			html := string(result.HTML)

			// Verify pre tag is present
			if tt.wantPre && !strings.Contains(html, "<pre") {
				t.Error("expected HTML to contain <pre> tag")
			}

			// Verify chroma class for syntax highlighting
			if tt.wantChroma {
				if !strings.Contains(html, "chroma") {
					t.Error("expected HTML to contain 'chroma' class for syntax highlighting")
				}
				// Verify language-specific class
				if tt.wantLanguage != "" {
					expectedClass := "language-" + tt.wantLanguage
					if !strings.Contains(html, expectedClass) && !strings.Contains(html, "highlight") {
						// chroma uses different class patterns, check for highlight wrapper
						if !strings.Contains(html, "class=\"chroma\"") {
							t.Errorf("expected HTML to contain syntax highlighting classes, got: %s", html)
						}
					}
				}
			}
		})
	}
}

func TestMarkdownRenderer_ListRendering(t *testing.T) {
	renderer := NewMarkdownRenderer()

	t.Run("unordered list", func(t *testing.T) {
		markdown := `- First item
- Second item
- Third item`

		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}

		html := string(result.HTML)

		// Verify unordered list structure
		if !strings.Contains(html, "<ul>") {
			t.Error("expected HTML to contain <ul> tag")
		}
		if !strings.Contains(html, "</ul>") {
			t.Error("expected HTML to contain </ul> tag")
		}

		// Verify list items
		if !strings.Contains(html, "<li>") {
			t.Error("expected HTML to contain <li> tags")
		}
		if !strings.Contains(html, "First item") {
			t.Error("expected HTML to contain 'First item'")
		}
		if !strings.Contains(html, "Second item") {
			t.Error("expected HTML to contain 'Second item'")
		}
		if !strings.Contains(html, "Third item") {
			t.Error("expected HTML to contain 'Third item'")
		}
	})

	t.Run("ordered list", func(t *testing.T) {
		markdown := `1. First step
2. Second step
3. Third step`

		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}

		html := string(result.HTML)

		// Verify ordered list structure
		if !strings.Contains(html, "<ol>") {
			t.Error("expected HTML to contain <ol> tag")
		}
		if !strings.Contains(html, "</ol>") {
			t.Error("expected HTML to contain </ol> tag")
		}

		// Verify list items
		if !strings.Contains(html, "<li>") {
			t.Error("expected HTML to contain <li> tags")
		}
		if !strings.Contains(html, "First step") {
			t.Error("expected HTML to contain 'First step'")
		}
		if !strings.Contains(html, "Second step") {
			t.Error("expected HTML to contain 'Second step'")
		}
	})

	t.Run("nested list", func(t *testing.T) {
		markdown := `- Parent item
  - Child item 1
  - Child item 2
- Another parent`

		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}

		html := string(result.HTML)

		// Verify nested list structure (should have multiple <ul> tags)
		ulCount := strings.Count(html, "<ul>")
		if ulCount < 2 {
			t.Errorf("expected at least 2 <ul> tags for nested list, got %d", ulCount)
		}

		if !strings.Contains(html, "Parent item") {
			t.Error("expected HTML to contain 'Parent item'")
		}
		if !strings.Contains(html, "Child item 1") {
			t.Error("expected HTML to contain 'Child item 1'")
		}
	})
}

func TestMarkdownRenderer_InlineFormatting(t *testing.T) {
	renderer := NewMarkdownRenderer()

	tests := []struct {
		name     string
		markdown string
		wantTag  string
		wantText string
	}{
		{
			name:     "bold text",
			markdown: "This is **bold** text",
			wantTag:  "<strong>",
			wantText: "bold",
		},
		{
			name:     "italic text",
			markdown: "This is *italic* text",
			wantTag:  "<em>",
			wantText: "italic",
		},
		{
			name:     "inline code",
			markdown: "Use `tergum backup` command",
			wantTag:  "<code>",
			wantText: "tergum backup",
		},
		{
			name:     "bold with double asterisks",
			markdown: "**important**",
			wantTag:  "<strong>",
			wantText: "important",
		},
		{
			name:     "italic with underscore",
			markdown: "_emphasized_",
			wantTag:  "<em>",
			wantText: "emphasized",
		},
		{
			name:     "strikethrough (GFM)",
			markdown: "This is ~~deleted~~ text",
			wantTag:  "<del>",
			wantText: "deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := renderer.Render([]byte(tt.markdown))
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}

			html := string(result.HTML)

			// Verify the formatting tag is present
			if !strings.Contains(html, tt.wantTag) {
				t.Errorf("expected HTML to contain %q tag, got: %s", tt.wantTag, html)
			}

			// Verify the formatted text is present
			if !strings.Contains(html, tt.wantText) {
				t.Errorf("expected HTML to contain text %q, got: %s", tt.wantText, html)
			}
		})
	}
}

func TestMarkdownRenderer_TOCExtraction(t *testing.T) {
	renderer := NewMarkdownRenderer()

	t.Run("extracts h2 and h3 headings only", func(t *testing.T) {
		markdown := `# H1 Title
## First Section
### Subsection A
### Subsection B
## Second Section
#### Deep Level
### Another Subsection
##### Very Deep
###### Deepest`

		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}

		// Should have 2 top-level entries (h2)
		if len(result.TOC) != 2 {
			t.Errorf("expected 2 top-level TOC entries, got %d", len(result.TOC))
		}

		// First section
		if result.TOC[0].Text != "First Section" {
			t.Errorf("expected first TOC entry to be 'First Section', got %q", result.TOC[0].Text)
		}
		if result.TOC[0].Level != 2 {
			t.Errorf("expected first TOC entry level to be 2, got %d", result.TOC[0].Level)
		}

		// First section should have 2 children (Subsection A and B)
		if len(result.TOC[0].Children) != 2 {
			t.Errorf("expected first section to have 2 children, got %d", len(result.TOC[0].Children))
		}

		// Second section
		if result.TOC[1].Text != "Second Section" {
			t.Errorf("expected second TOC entry to be 'Second Section', got %q", result.TOC[1].Text)
		}

		// Second section should have 1 child (Another Subsection)
		// Note: h4 and deeper are not included in TOC
		if len(result.TOC[1].Children) != 1 {
			t.Errorf("expected second section to have 1 child, got %d", len(result.TOC[1].Children))
		}
	})

	t.Run("generates unique IDs for TOC entries", func(t *testing.T) {
		markdown := `## Setup
### Setup
## Configuration
### Configuration`

		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}

		// Collect all IDs
		ids := make(map[string]bool)
		for _, entry := range result.TOC {
			if ids[entry.ID] {
				t.Errorf("duplicate ID found: %q", entry.ID)
			}
			ids[entry.ID] = true

			for _, child := range entry.Children {
				if ids[child.ID] {
					t.Errorf("duplicate ID found: %q", child.ID)
				}
				ids[child.ID] = true
			}
		}

		// Should have 4 unique IDs
		if len(ids) != 4 {
			t.Errorf("expected 4 unique IDs, got %d", len(ids))
		}
	})

	t.Run("h3 without preceding h2 becomes top-level", func(t *testing.T) {
		markdown := `### Orphan Subsection
## Section`

		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}

		// Should have 2 top-level entries
		if len(result.TOC) != 2 {
			t.Errorf("expected 2 top-level TOC entries, got %d", len(result.TOC))
		}

		// First entry is the orphan h3
		if result.TOC[0].Text != "Orphan Subsection" {
			t.Errorf("expected first entry to be 'Orphan Subsection', got %q", result.TOC[0].Text)
		}
		if result.TOC[0].Level != 3 {
			t.Errorf("expected first entry level to be 3, got %d", result.TOC[0].Level)
		}
	})

	t.Run("empty document produces empty TOC", func(t *testing.T) {
		markdown := ""

		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}

		if len(result.TOC) != 0 {
			t.Errorf("expected empty TOC, got %d entries", len(result.TOC))
		}
	})
}

func TestMarkdownRenderer_ComplexDocument(t *testing.T) {
	renderer := NewMarkdownRenderer()

	// Test a more realistic CLI documentation snippet
	markdown := `# Tergum CLI Reference

## Global Flags

These flags apply to all commands:

| Flag | Description |
| ---- | ----------- |
| --config | Path to config file |
| --verbose | Enable verbose output |

## Commands

### tergum backup

Backup files to the server.

` + "```bash\ntergum backup --all\n```" + `

**Options:**

- *--all* - Backup all configured paths
- *--path* - Specify a single path to backup

See [configuration](#global-flags) for more details.
`

	result, err := renderer.Render([]byte(markdown))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	html := string(result.HTML)

	// Verify all elements are present
	elements := []struct {
		name     string
		contains string
	}{
		{"h1 heading", "<h1"},
		{"h2 heading", "<h2"},
		{"h3 heading", "<h3"},
		{"table", "<table>"},
		{"code block", "<pre"},
		{"chroma syntax highlighting", "chroma"},
		{"unordered list", "<ul>"},
		{"list item", "<li>"},
		{"bold text", "<strong>"},
		{"italic text", "<em>"},
		{"link", "<a "},
		{"inline code", "<code>"},
	}

	for _, elem := range elements {
		if !strings.Contains(html, elem.contains) {
			t.Errorf("expected HTML to contain %s (%q)", elem.name, elem.contains)
		}
	}

	// Verify TOC structure
	if len(result.TOC) != 2 { // "Global Flags" and "Commands"
		t.Errorf("expected 2 top-level TOC entries, got %d", len(result.TOC))
	}

	if result.TOC[0].Text != "Global Flags" {
		t.Errorf("expected first TOC entry to be 'Global Flags', got %q", result.TOC[0].Text)
	}

	if result.TOC[1].Text != "Commands" {
		t.Errorf("expected second TOC entry to be 'Commands', got %q", result.TOC[1].Text)
	}

	// Commands should have "tergum backup" as a child
	if len(result.TOC[1].Children) != 1 {
		t.Errorf("expected Commands to have 1 child, got %d", len(result.TOC[1].Children))
	}
	if result.TOC[1].Children[0].Text != "tergum backup" {
		t.Errorf("expected child to be 'tergum backup', got %q", result.TOC[1].Children[0].Text)
	}
}

func TestMarkdownRenderer_HeadingIDsInHTML(t *testing.T) {
	renderer := NewMarkdownRenderer()

	markdown := `# Title
## Setup
### Configuration`

	result, err := renderer.Render([]byte(markdown))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	html := string(result.HTML)

	// All headings should have IDs for anchor navigation
	expectedIDs := []string{
		`id="title"`,
		`id="setup"`,
		`id="configuration"`,
	}

	for _, id := range expectedIDs {
		if !strings.Contains(html, id) {
			t.Errorf("expected HTML to contain %q, got: %s", id, html)
		}
	}
}

func TestMarkdownRenderer_Paragraphs(t *testing.T) {
	renderer := NewMarkdownRenderer()

	markdown := `First paragraph with some text.

Second paragraph with more text.`

	result, err := renderer.Render([]byte(markdown))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	html := string(result.HTML)

	// Verify paragraph tags
	pCount := strings.Count(html, "<p>")
	if pCount != 2 {
		t.Errorf("expected 2 <p> tags, got %d", pCount)
	}

	if !strings.Contains(html, "First paragraph") {
		t.Error("expected HTML to contain 'First paragraph'")
	}
	if !strings.Contains(html, "Second paragraph") {
		t.Error("expected HTML to contain 'Second paragraph'")
	}
}

func TestMarkdownRenderer_Blockquote(t *testing.T) {
	renderer := NewMarkdownRenderer()

	markdown := `> This is a quote
> that spans multiple lines`

	result, err := renderer.Render([]byte(markdown))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	html := string(result.HTML)

	if !strings.Contains(html, "<blockquote>") {
		t.Error("expected HTML to contain <blockquote> tag")
	}
	if !strings.Contains(html, "This is a quote") {
		t.Error("expected HTML to contain quote text")
	}
}

func TestMarkdownRenderer_HorizontalRule(t *testing.T) {
	renderer := NewMarkdownRenderer()

	markdown := `Some text

---

More text`

	result, err := renderer.Render([]byte(markdown))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	html := string(result.HTML)

	if !strings.Contains(html, "<hr") {
		t.Error("expected HTML to contain <hr> tag")
	}
}

func TestMarkdownRenderer_AutoLinks(t *testing.T) {
	renderer := NewMarkdownRenderer()

	// GFM autolinks
	markdown := `Visit https://example.com for more info.`

	result, err := renderer.Render([]byte(markdown))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	html := string(result.HTML)

	if !strings.Contains(html, `href="https://example.com"`) {
		t.Error("expected HTML to contain autolinked URL")
	}
}

// =============================================================================
// Unit Tests for generateHeadingID (existing tests moved below)
// =============================================================================

func TestGenerateHeadingID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		existing map[string]int
		want     string
	}{
		{
			name:     "simple two words",
			input:    "tergum setup",
			existing: make(map[string]int),
			want:     "tergum-setup",
		},
		{
			name:     "uppercase words",
			input:    "Global Flags",
			existing: make(map[string]int),
			want:     "global-flags",
		},
		{
			name:     "three words",
			input:    "tergum client list",
			existing: make(map[string]int),
			want:     "tergum-client-list",
		},
		{
			name:     "with underscores",
			input:    "some_heading_text",
			existing: make(map[string]int),
			want:     "some-heading-text",
		},
		{
			name:     "special characters removed",
			input:    "Hello, World!",
			existing: make(map[string]int),
			want:     "hello-world",
		},
		{
			name:     "consecutive spaces",
			input:    "multiple   spaces",
			existing: make(map[string]int),
			want:     "multiple-spaces",
		},
		{
			name:     "leading and trailing spaces",
			input:    "  trimmed heading  ",
			existing: make(map[string]int),
			want:     "trimmed-heading",
		},
		{
			name:     "numbers preserved",
			input:    "Chapter 1",
			existing: make(map[string]int),
			want:     "chapter-1",
		},
		{
			name:     "empty string becomes heading",
			input:    "",
			existing: make(map[string]int),
			want:     "heading",
		},
		{
			name:     "only special characters becomes heading",
			input:    "!@#$%",
			existing: make(map[string]int),
			want:     "heading",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateHeadingID(tt.input, tt.existing)
			if got != tt.want {
				t.Errorf("generateHeadingID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateHeadingID_Duplicates(t *testing.T) {
	existing := make(map[string]int)

	// First occurrence
	id1 := generateHeadingID("tergum client list", existing)
	if id1 != "tergum-client-list" {
		t.Errorf("First occurrence: got %q, want %q", id1, "tergum-client-list")
	}

	// Second occurrence (duplicate)
	id2 := generateHeadingID("tergum client list", existing)
	if id2 != "tergum-client-list-1" {
		t.Errorf("Second occurrence: got %q, want %q", id2, "tergum-client-list-1")
	}

	// Third occurrence
	id3 := generateHeadingID("tergum client list", existing)
	if id3 != "tergum-client-list-2" {
		t.Errorf("Third occurrence: got %q, want %q", id3, "tergum-client-list-2")
	}

	// Different heading should not be affected
	id4 := generateHeadingID("Global Flags", existing)
	if id4 != "global-flags" {
		t.Errorf("Different heading: got %q, want %q", id4, "global-flags")
	}
}
