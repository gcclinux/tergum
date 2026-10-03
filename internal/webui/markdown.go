package webui

import (
	"bytes"
	"html/template"
	"regexp"
	"strconv"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Regex patterns for heading ID generation
var (
	nonAlphanumericRegex  = regexp.MustCompile(`[^a-z0-9-]`)
	consecutiveHyphensRE  = regexp.MustCompile(`-+`)
)

// TOCEntry represents a single entry in the table of contents.
// It captures heading information for navigation purposes.
type TOCEntry struct {
	ID       string     // URL-friendly heading ID
	Text     string     // Heading text content
	Level    int        // Heading level (2 or 3)
	Children []TOCEntry // Nested h3 entries under h2
}

// RenderResult contains the rendered HTML and extracted metadata.
type RenderResult struct {
	HTML template.HTML // Safe HTML content
	TOC  []TOCEntry    // Extracted table of contents (h2 and h3 only)
}

// MarkdownRenderer converts Markdown content to HTML with syntax highlighting
// and extracts table of contents.
type MarkdownRenderer struct {
	md goldmark.Markdown
}

// generateHeadingID converts heading text to a URL-friendly ID.
// Rules:
// 1. Convert to lowercase
// 2. Replace spaces and underscores with hyphens
// 3. Remove non-alphanumeric characters except hyphens
// 4. Collapse consecutive hyphens
// 5. Trim leading/trailing hyphens
// 6. Append numeric suffix for duplicates (-1, -2, etc.)
func generateHeadingID(text string, existing map[string]int) string {
	// Step 1: Convert to lowercase
	id := strings.ToLower(text)

	// Step 2: Replace spaces and underscores with hyphens
	id = strings.ReplaceAll(id, " ", "-")
	id = strings.ReplaceAll(id, "_", "-")

	// Step 3: Remove non-alphanumeric characters except hyphens
	id = nonAlphanumericRegex.ReplaceAllString(id, "")

	// Step 4: Collapse consecutive hyphens
	id = consecutiveHyphensRE.ReplaceAllString(id, "-")

	// Step 5: Trim leading/trailing hyphens
	id = strings.Trim(id, "-")

	// Handle empty result
	if id == "" {
		id = "heading"
	}

	// Step 6: Handle duplicates by appending numeric suffix
	baseID := id
	count, exists := existing[baseID]
	if exists {
		// Increment count and append suffix
		existing[baseID] = count + 1
		id = baseID + "-" + strconv.Itoa(count)
	} else {
		// First occurrence, register with count 1 for next duplicate
		existing[baseID] = 1
	}

	return id
}

// NewMarkdownRenderer creates a new renderer with goldmark and chroma.
// It configures:
// - GFM extension for GitHub Flavored Markdown tables
// - Chroma syntax highlighting with monokai theme and CSS classes
// - Auto heading IDs for anchor link navigation
func NewMarkdownRenderer() *MarkdownRenderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			// GFM extension for tables, strikethrough, autolinks, and task lists
			extension.GFM,
			// Syntax highlighting with chroma
			highlighting.NewHighlighting(
				highlighting.WithStyle("monokai"),
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(true),
					chromahtml.WithLineNumbers(false),
				),
			),
		),
		goldmark.WithParserOptions(
			// Enable auto heading IDs for anchor navigation
			parser.WithAutoHeadingID(),
		),
	)

	return &MarkdownRenderer{
		md: md,
	}
}

// Render converts Markdown source to HTML and extracts TOC.
// It parses the markdown, walks the AST to extract h2/h3 headings for the TOC,
// generates unique IDs for each heading, and renders the final HTML.
func (r *MarkdownRenderer) Render(source []byte) (*RenderResult, error) {
	// Parse the markdown source to get the AST
	reader := text.NewReader(source)
	doc := r.md.Parser().Parse(reader)

	// Extract TOC by walking the AST
	toc, idMap := r.extractTOC(doc, source)

	// Inject heading IDs into the AST before rendering
	r.injectHeadingIDs(doc, source, idMap)

	// Render the markdown to HTML
	var buf bytes.Buffer
	if err := r.md.Renderer().Render(&buf, source, doc); err != nil {
		return nil, err
	}

	return &RenderResult{
		HTML: template.HTML(buf.String()),
		TOC:  toc,
	}, nil
}

// extractTOC walks the AST and extracts h2 and h3 headings for the table of contents.
// Returns the TOC entries and a map of heading IDs for use during rendering.
func (r *MarkdownRenderer) extractTOC(doc ast.Node, source []byte) ([]TOCEntry, map[string]int) {
	var entries []TOCEntry
	var currentH2 *TOCEntry
	idMap := make(map[string]int)

	// Walk the AST to find headings
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		heading, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}

		// Only process h2 and h3
		if heading.Level != 2 && heading.Level != 3 {
			return ast.WalkContinue, nil
		}

		// Extract heading text
		text := extractHeadingText(heading, source)
		id := generateHeadingID(text, idMap)

		entry := TOCEntry{
			ID:    id,
			Text:  text,
			Level: heading.Level,
		}

		if heading.Level == 2 {
			// h2 is a top-level entry
			entries = append(entries, entry)
			// Keep reference to add children
			currentH2 = &entries[len(entries)-1]
		} else if heading.Level == 3 && currentH2 != nil {
			// h3 is nested under the current h2
			currentH2.Children = append(currentH2.Children, entry)
		} else if heading.Level == 3 {
			// h3 without a preceding h2, add as top-level
			entries = append(entries, entry)
		}

		return ast.WalkContinue, nil
	})

	return entries, idMap
}

// injectHeadingIDs adds id attributes to heading nodes so they appear in the rendered HTML.
// This ensures anchor links work correctly.
func (r *MarkdownRenderer) injectHeadingIDs(doc ast.Node, source []byte, existingIDs map[string]int) {
	// Create a fresh map for ID generation during injection
	// This ensures IDs match those generated during TOC extraction
	idMap := make(map[string]int)

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		heading, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}

		// Generate ID for all headings (not just h2/h3) for anchor link support
		text := extractHeadingText(heading, source)
		id := generateHeadingID(text, idMap)

		// Set the heading ID attribute
		heading.SetAttributeString("id", []byte(id))

		return ast.WalkContinue, nil
	})
}

// extractHeadingText extracts the plain text content from a heading node.
func extractHeadingText(heading *ast.Heading, source []byte) string {
	var textParts []string

	// Walk the heading's children to collect all text
	for child := heading.FirstChild(); child != nil; child = child.NextSibling() {
		collectText(child, source, &textParts)
	}

	return strings.Join(textParts, "")
}

// collectText recursively collects text content from AST nodes.
func collectText(n ast.Node, source []byte, parts *[]string) {
	switch node := n.(type) {
	case *ast.Text:
		*parts = append(*parts, string(node.Segment.Value(source)))
	case *ast.String:
		*parts = append(*parts, string(node.Value))
	case *ast.CodeSpan:
		// Include code span text
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			collectText(child, source, parts)
		}
	default:
		// Recursively process children for other node types
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			collectText(child, source, parts)
		}
	}
}
